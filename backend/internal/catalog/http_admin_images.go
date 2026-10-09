package catalog

// Unggah dan hapus foto utama produk (admin).
//
// POST   /api/admin/products/{id}/image  multipart/form-data, field "file" (JPG/PNG/WebP, maks. 2 MB)
// DELETE /api/admin/products/{id}/image
//
// Alur unggah: batas laju per admin -> pagar disk -> baca isi (maks. 2 MB) -> periksa tipe dari isi,
// DecodeConfig (tolak dekompresi-bom), decode, orientasi EXIF, perkecil (1200 px + thumbnail 400 px),
// encode ulang JPEG tanpa metadata -> tulis berkas baru (atomik) -> SATU transaksi: kunci produk,
// image_path + updated_by + log product.image_update -> bila transaksi gagal, berkas baru dihapus;
// bila sukses, berkas lama (dan thumbnail-nya) dihapus best-effort.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
)

const (
	msgImageTooMany    = "Terlalu banyak unggahan foto. Coba lagi sebentar lagi."
	maxUploadBodyBytes = imaging.MaxUploadBytes + 256<<10 // isi berkas + overhead multipart
)

type imageURLsDTO struct {
	Image string `json:"image"`
	Thumb string `json:"thumb"`
}

func (a *Service) AdminUploadProductImage(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if !platform.LimitUser(w, a.ImageLimiter, *adminID, msgImageTooMany) {
		return
	}
	if a.images == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, imaging.MsgImageStoreDown)
		return
	}
	db := a.db().WithContext(r.Context())
	// Produk harus ada (dan belum dihapus) sebelum memproses berkas.
	var exists int64
	if err := db.Model(&Product{}).Where("id = ?", id).Count(&exists).Error; err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if exists == 0 {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if err := a.imageGuard.Check(r.Context()); err != nil {
		st, msg := imaging.ImageErrorStatus(err)
		platform.WriteError(w, st, msg)
		return
	}
	data, st, msg := imaging.ReadUploadFile(w, r)
	if st != 0 {
		platform.WriteError(w, st, msg)
		return
	}
	release, ok := a.imageSlots.Acquire(r.Context())
	if !ok {
		platform.WriteError(w, http.StatusServiceUnavailable, imaging.MsgImageBusy)
		return
	}
	img, err := imaging.ProcessProductImage(data)
	release()
	if err != nil {
		st, msg := imaging.ImageErrorStatus(err)
		platform.WriteError(w, st, msg)
		return
	}

	key, err := media.NewProductImageKey(id)
	if err != nil {
		a.respondTxError(w, err, "kunci foto")
		return
	}
	tkey := media.ThumbKey(key)
	ctx := context.WithoutCancel(r.Context()) // penulisan/pembersihan berkas tidak boleh terpotong di tengah
	if err := a.images.Save(ctx, key, img.Main); err != nil {
		a.imageStoreFailed(w, err)
		return
	}
	if err := a.images.Save(ctx, tkey, img.Thumb); err != nil {
		a.removeImageFiles(ctx, key)
		a.imageStoreFailed(w, err)
		return
	}
	newBytes := int64(len(img.Main) + len(img.Thumb))
	a.imageGuard.Add(newBytes)

	var oldKey string
	err = db.Transaction(func(tx *gorm.DB) error {
		cur, _, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		if cur.ImagePath != nil {
			oldKey = *cur.ImagePath
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).
			Updates(map[string]any{"image_path": key, "updated_by": adminID}).Error; err != nil {
			return err
		}
		details := map[string]any{
			"kunciBaru": key, "kunciLama": nilIfEmpty(oldKey), "ukuranByte": len(img.Main), "ukuranThumbByte": len(img.Thumb),
			"ukuranUnggahByte": len(data), "lebar": img.Width, "tinggi": img.Height, "formatAsal": string(img.SourceKind),
		}
		if err := a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "product.image_update",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Foto produk diperbarui: " + cur.Name,
			Details: details,
		})); err != nil {
			return err
		}
		if a.ImageTxHook != nil {
			return a.ImageTxHook(tx)
		}
		return nil
	})
	if err != nil {
		// Transaksi gagal: berkas baru tidak dirujuk siapa pun -> hapus.
		a.removeImageFiles(ctx, key)
		a.imageGuard.Add(-newBytes)
		a.respondTxError(w, err, "unggah foto produk")
		return
	}
	// Sesudah commit: berkas lama (bila kunci sah milik produk ini) dihapus best-effort.
	if oldKey != "" && oldKey != key && media.ProductImageKeyFor(oldKey, id) {
		a.removeImageFiles(ctx, oldKey)
	}
	image, thumb := media.PublicImageURLs(a.images, key)
	platform.WriteJSON(w, http.StatusOK, imageURLsDTO{Image: image, Thumb: thumb})
}

func (a *Service) AdminDeleteProductImage(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if !platform.LimitUser(w, a.ImageLimiter, *adminID, msgImageTooMany) {
		return
	}
	db := a.db().WithContext(r.Context())
	var oldKey string
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, _, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		if cur.ImagePath == nil || *cur.ImagePath == "" {
			return nil // sudah tanpa foto: tidak ada yang diubah atau dicatat
		}
		oldKey = *cur.ImagePath
		if err := tx.Model(&Product{}).Where("id = ?", id).
			Updates(map[string]any{"image_path": nil, "updated_by": adminID}).Error; err != nil {
			return err
		}
		if err := a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "product.image_delete",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Foto produk dihapus: " + cur.Name,
			Details: map[string]any{"kunciLama": oldKey},
		})); err != nil {
			return err
		}
		if a.ImageTxHook != nil {
			return a.ImageTxHook(tx)
		}
		return nil
	})
	if err != nil {
		a.respondTxError(w, err, "hapus foto produk")
		return
	}
	if oldKey != "" && media.ProductImageKeyFor(oldKey, id) && a.images != nil {
		a.removeImageFiles(context.WithoutCancel(r.Context()), oldKey)
	}
	platform.WriteJSON(w, http.StatusOK, imageURLsDTO{})
}

// removeImageFiles menghapus berkas utama + thumbnail (best-effort, galat hanya dicatat).
func (a *Service) removeImageFiles(ctx context.Context, key string) {
	for _, k := range []string{key, media.ThumbKey(key)} {
		if err := a.images.Delete(ctx, k); err != nil {
			log.Printf("hapus berkas foto %s: %v", k, err)
		}
	}
	// Ukuran persis tidak diketahui tanpa stat; angka cache dikoreksi pada penghitungan ulang berkala.
}

func (a *Service) imageStoreFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, imaging.ErrStoreUnavailable) {
		a.imageGuard.MarkUnwritable()
	}
	st, msg := imaging.ImageErrorStatus(err)
	if st == http.StatusInternalServerError {
		log.Printf("simpan foto produk: %v", err)
	} else {
		log.Printf("simpan foto produk (%d): %v", st, err)
	}
	platform.WriteError(w, st, msg)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
