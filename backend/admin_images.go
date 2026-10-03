package main

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
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

const (
	msgImageTooMany     = "Terlalu banyak unggahan foto. Coba lagi sebentar lagi."
	msgImageNoFile      = "Pilih berkas foto terlebih dahulu (field \"file\")."
	msgImageStoreDown   = "Penyimpanan foto sedang tidak tersedia. Coba lagi nanti; bagian lain toko tetap berjalan."
	msgImageStoreFull   = "Penyimpanan foto penuh. Hapus foto yang tidak dipakai atau hubungi pengelola server."
	msgImageBusy        = "Server sedang memproses foto lain. Coba lagi sebentar lagi."
	maxUploadBodyBytes  = maxUploadBytes + 256<<10 // isi berkas + overhead multipart
	multipartFileField  = "file"
	multipartMaxPartHdr = 8 << 10
)

// setImageStore memasang ImageStore beserta pagar disknya (dipakai NewApp dan tes).
func (a *App) setImageStore(s ImageStore, maxMB int64) {
	a.images = s
	var h storeHealth
	if sh, ok := s.(storeHealth); ok {
		h = sh
	}
	a.imageGuard = newDiskGuard(h, maxMB<<20, 5*time.Minute)
}

type imageURLsDTO struct {
	Image string `json:"image"`
	Thumb string `json:"thumb"`
}

// imageErrorStatus memetakan galat pemrosesan/penyimpanan ke status HTTP dan pesan Indonesia.
func imageErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errImageTooLarge):
		return http.StatusRequestEntityTooLarge, errImageTooLarge.Error()
	case errors.Is(err, errImageType):
		return http.StatusUnsupportedMediaType, errImageType.Error()
	case errors.Is(err, errImageCorrupt):
		return http.StatusUnprocessableEntity, errImageCorrupt.Error()
	case errors.Is(err, errImageResolution):
		return http.StatusUnprocessableEntity, errImageResolution.Error()
	case errors.Is(err, errStoreFull):
		return http.StatusInsufficientStorage, msgImageStoreFull
	case errors.Is(err, errStoreUnavailable):
		return http.StatusServiceUnavailable, msgImageStoreDown
	}
	return http.StatusInternalServerError, msgServerError
}

// readUploadFile membaca isi field "file" dari body multipart (maks. maxUploadBytes).
func readUploadFile(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, http.StatusBadRequest, "Unggahan harus berupa multipart/form-data dengan field \"file\"."
	}
	if r.ContentLength > maxUploadBodyBytes {
		return nil, http.StatusRequestEntityTooLarge, errImageTooLarge.Error()
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, http.StatusBadRequest, "Format unggahan tidak valid."
	}
	for parts := 0; parts < 8; parts++ {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, errImageTooLarge.Error()
			}
			return nil, http.StatusBadRequest, "Format unggahan tidak valid."
		}
		if p.FormName() != multipartFileField {
			_, _ = io.Copy(io.Discard, io.LimitReader(p, multipartMaxPartHdr))
			p.Close()
			continue
		}
		// Nama berkas dan Content-Type dari klien sengaja diabaikan.
		data, err := io.ReadAll(io.LimitReader(p, maxUploadBytes+1))
		p.Close()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, errImageTooLarge.Error()
			}
			return nil, http.StatusBadRequest, "Unggahan terputus. Coba lagi."
		}
		if len(data) > maxUploadBytes {
			return nil, http.StatusRequestEntityTooLarge, errImageTooLarge.Error()
		}
		if len(data) == 0 {
			return nil, http.StatusBadRequest, msgImageNoFile
		}
		return data, 0, ""
	}
	return nil, http.StatusBadRequest, msgImageNoFile
}

// acquireImageSlot membatasi pemrosesan gambar paralel (decode foto besar memakan memori).
func (a *App) acquireImageSlot(ctx context.Context) (func(), bool) {
	select {
	case a.imageSem <- struct{}{}:
		return func() { <-a.imageSem }, true
	case <-ctx.Done():
		return nil, false
	case <-time.After(20 * time.Second):
		return nil, false
	}
}

func (a *App) AdminUploadProductImage(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if !limitUser(w, a.imageLimiter, admin, msgImageTooMany) {
		return
	}
	if a.images == nil {
		writeError(w, http.StatusServiceUnavailable, msgImageStoreDown)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	// Produk harus ada (dan belum dihapus) sebelum memproses berkas.
	var exists int64
	if err := db.Model(&Product{}).Where("id = ?", id).Count(&exists).Error; err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if exists == 0 {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if err := a.imageGuard.check(r.Context()); err != nil {
		st, msg := imageErrorStatus(err)
		writeError(w, st, msg)
		return
	}
	data, st, msg := readUploadFile(w, r)
	if st != 0 {
		writeError(w, st, msg)
		return
	}
	release, ok := a.acquireImageSlot(r.Context())
	if !ok {
		writeError(w, http.StatusServiceUnavailable, msgImageBusy)
		return
	}
	img, err := processProductImage(data)
	release()
	if err != nil {
		st, msg := imageErrorStatus(err)
		writeError(w, st, msg)
		return
	}

	key, err := newProductImageKey(id)
	if err != nil {
		a.respondTxError(w, err, "kunci foto")
		return
	}
	tkey := thumbKey(key)
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
	a.imageGuard.add(newBytes)

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
			Updates(map[string]any{"image_path": key, "updated_by": uid(admin)}).Error; err != nil {
			return err
		}
		details := map[string]any{
			"kunciBaru": key, "kunciLama": nilIfEmpty(oldKey), "ukuranByte": len(img.Main), "ukuranThumbByte": len(img.Thumb),
			"ukuranUnggahByte": len(data), "lebar": img.Width, "tinggi": img.Height, "formatAsal": string(img.SourceKind),
		}
		if err := a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "product.image_update",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Foto produk diperbarui: " + cur.Name,
			Details: details,
		})); err != nil {
			return err
		}
		if a.imageTxHook != nil {
			return a.imageTxHook(tx)
		}
		return nil
	})
	if err != nil {
		// Transaksi gagal: berkas baru tidak dirujuk siapa pun -> hapus.
		a.removeImageFiles(ctx, key)
		a.imageGuard.add(-newBytes)
		a.respondTxError(w, err, "unggah foto produk")
		return
	}
	// Sesudah commit: berkas lama (bila kunci sah milik produk ini) dihapus best-effort.
	if oldKey != "" && oldKey != key && productImageKeyFor(oldKey, id) {
		a.removeImageFiles(ctx, oldKey)
	}
	image, thumb := publicImageURLs(a.images, key)
	writeJSON(w, http.StatusOK, imageURLsDTO{Image: image, Thumb: thumb})
}

func (a *App) AdminDeleteProductImage(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if !limitUser(w, a.imageLimiter, admin, msgImageTooMany) {
		return
	}
	db := a.db.Load().WithContext(r.Context())
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
			Updates(map[string]any{"image_path": nil, "updated_by": uid(admin)}).Error; err != nil {
			return err
		}
		if err := a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "product.image_delete",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Foto produk dihapus: " + cur.Name,
			Details: map[string]any{"kunciLama": oldKey},
		})); err != nil {
			return err
		}
		if a.imageTxHook != nil {
			return a.imageTxHook(tx)
		}
		return nil
	})
	if err != nil {
		a.respondTxError(w, err, "hapus foto produk")
		return
	}
	if oldKey != "" && productImageKeyFor(oldKey, id) && a.images != nil {
		a.removeImageFiles(context.WithoutCancel(r.Context()), oldKey)
	}
	writeJSON(w, http.StatusOK, imageURLsDTO{})
}

// removeImageFiles menghapus berkas utama + thumbnail (best-effort, galat hanya dicatat).
func (a *App) removeImageFiles(ctx context.Context, key string) {
	for _, k := range []string{key, thumbKey(key)} {
		if err := a.images.Delete(ctx, k); err != nil {
			log.Printf("hapus berkas foto %s: %v", k, err)
		}
	}
	// Ukuran persis tidak diketahui tanpa stat; angka cache dikoreksi pada penghitungan ulang berkala.
}

func (a *App) imageStoreFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, errStoreUnavailable) {
		a.imageGuard.markUnwritable()
	}
	st, msg := imageErrorStatus(err)
	if st == http.StatusInternalServerError {
		log.Printf("simpan foto produk: %v", err)
	} else {
		log.Printf("simpan foto produk (%d): %v", st, err)
	}
	writeError(w, st, msg)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
