package media

// Lapisan penyimpanan foto produk. Tabel (products.image_path) hanya menyimpan KUNCI
// (mis. "products/12/<uuid>.jpg"); lokasi fisik dan URL publik ditentukan ImageStore.
// Implementasi saat ini: LocalStore (disk VPS, IMAGE_STORE=local). Untuk pindah ke Cloudflare R2
// atau Google Cloud Storage cukup menambah implementasi ImageStore baru (Save/Delete/URL) dan
// memilihnya lewat IMAGE_STORE; tabel dan tampilan tidak berubah.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
)

// ImageStore menyimpan berkas foto berdasarkan kunci relatif.
type ImageStore interface {
	// Save menulis data ke kunci secara atomik (pembaca tidak pernah melihat berkas setengah jadi).
	Save(ctx context.Context, key string, data []byte) error
	// Delete menghapus kunci; kunci yang tidak ada bukan galat.
	Delete(ctx context.Context, key string) error
	// URL mengembalikan URL publik untuk kunci (mis. "/uploads/products/12/<uuid>.jpg").
	URL(key string) string
}

// StoreHealth (opsional) dipakai pagar disk dan pemeriksaan "dapat ditulis".
type StoreHealth interface {
	Writable() error
	Usage(ctx context.Context) (int64, error)
}

// ---------- Kunci ----------

var (
	// Kunci foto utama yang sah. Nilai image_path lain (mis. "kerupuk1.jpg" peninggalan lama)
	// dianggap TIDAK punya foto.
	productImageKeyRe = regexp.MustCompile(`^products/([0-9]{1,20})/(` + imaging.UUIDPattern + `)\.jpg$`)
	safeKeyRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*(/[a-z0-9][a-z0-9_.-]*)*$`)
)

// IsProductImageKey: true bila s kunci foto utama yang dibuat server.
func IsProductImageKey(s string) bool { return productImageKeyRe.MatchString(s) }

// ProductImageKeyFor: kunci foto utama milik produk id tertentu?
func ProductImageKeyFor(s string, id uint64) bool {
	m := productImageKeyRe.FindStringSubmatch(s)
	return m != nil && m[1] == strconv.FormatUint(id, 10)
}

// ThumbKey menurunkan kunci thumbnail: products/12/<uuid>.jpg -> products/12/<uuid>_t.jpg.
func ThumbKey(key string) string { return strings.TrimSuffix(key, ".jpg") + "_t.jpg" }

// NewProductImageKey membuat kunci baru dengan UUID v4 acak; nama berkas dari klien tidak pernah dipakai.
func NewProductImageKey(productID uint64) (string, error) {
	id, err := platform.NewPublicID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("products/%d/%s.jpg", productID, id), nil
}

// PublicImageURLs mengubah nilai image_path menjadi URL publik {image, thumb}; kosong untuk nilai lama/kosong.
func PublicImageURLs(store ImageStore, imagePath string) (string, string) {
	if store == nil || !IsProductImageKey(imagePath) {
		return "", ""
	}
	return store.URL(imagePath), store.URL(ThumbKey(imagePath))
}

// ---------- LocalStore ----------

// LocalStore menyimpan berkas di bawah direktori dasar (UPLOADS_DIR, default /data/uploads).
type LocalStore struct {
	base      string
	urlPrefix string // default "/uploads/"
}

func NewLocalStore(base, urlPrefix string) *LocalStore {
	if urlPrefix == "" {
		urlPrefix = "/uploads/"
	}
	return &LocalStore{base: filepath.Clean(base), urlPrefix: urlPrefix}
}

// path memetakan kunci ke path di dalam direktori dasar; menolak kunci yang bisa keluar dari base.
func (s *LocalStore) path(key string) (string, error) {
	if key == "" || len(key) > 255 || !safeKeyRe.MatchString(key) || strings.Contains(key, "..") {
		return "", imaging.ErrBadKey
	}
	p := filepath.Join(s.base, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.base, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", imaging.ErrBadKey
	}
	return p, nil
}

func (s *LocalStore) URL(key string) string { return s.urlPrefix + key }

func (s *LocalStore) Save(ctx context.Context, key string, data []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return imaging.WrapStoreErr(err)
	}
	tmp, err := os.CreateTemp(dir, ".unggah-*.tmp")
	if err != nil {
		return imaging.WrapStoreErr(err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := tmp.Sync(); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := tmp.Close(); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return imaging.WrapStoreErr(err)
	}
	ok = true
	return nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	// Direktori produk yang kosong sengaja dibiarkan (menghapusnya bisa berlomba dengan unggahan baru).
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Writable memeriksa direktori dasar bisa ditulis (membuat lalu menghapus berkas uji).
func (s *LocalStore) Writable() error {
	if err := os.MkdirAll(s.base, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.base, ".cek-tulis-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Usage menjumlahkan ukuran semua berkas di bawah direktori dasar.
func (s *LocalStore) Usage(ctx context.Context) (int64, error) {
	var total int64
	err := filepath.WalkDir(s.base, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// ---------- Pagar disk ----------

// DiskGuard menolak unggahan baru bila total ukuran uploads melebihi batas. Ukuran dihitung ulang
// berkala (cache `ttl`), tidak setiap permintaan; unggahan sukses menambah angka cache.
type DiskGuard struct {
	mu       sync.Mutex
	health   StoreHealth
	limit    int64
	ttl      time.Duration
	now      func() time.Time
	usage    int64
	measured time.Time
	writable bool
	checked  time.Time
}

func NewDiskGuard(h StoreHealth, limitBytes int64, ttl time.Duration) *DiskGuard {
	return &DiskGuard{health: h, limit: limitBytes, ttl: ttl, now: time.Now}
}

// StartupCheck mencatat peringatan bila direktori tidak dapat ditulis (bagian lain toko tetap jalan).
func (g *DiskGuard) StartupCheck(dir string) {
	if g == nil || g.health == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checked = g.now()
	if err := g.health.Writable(); err != nil {
		g.writable = false
		log.Printf("PERINGATAN: direktori unggahan %s tidak dapat ditulis (%v); unggah foto produk akan menjawab 503", dir, err)
		return
	}
	g.writable = true
	log.Printf("direktori unggahan %s dapat ditulis", dir)
}

// Check: nil bila boleh mengunggah; imaging.ErrStoreUnavailable / imaging.ErrStoreFull bila tidak.
func (g *DiskGuard) Check(ctx context.Context) error {
	if g == nil || g.health == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if !g.writable && (g.checked.IsZero() || now.Sub(g.checked) >= time.Minute) {
		g.checked = now
		g.writable = g.health.Writable() == nil
	}
	if !g.writable {
		return imaging.ErrStoreUnavailable
	}
	if g.measured.IsZero() || now.Sub(g.measured) >= g.ttl {
		u, err := g.health.Usage(ctx)
		if err != nil {
			log.Printf("pagar disk: gagal menghitung ukuran uploads: %v", err)
			return imaging.ErrStoreUnavailable
		}
		g.usage, g.measured = u, now
	}
	if g.limit > 0 && g.usage >= g.limit {
		return imaging.ErrStoreFull
	}
	return nil
}

// Add memperbarui angka cache setelah menulis/menghapus berkas.
func (g *DiskGuard) Add(n int64) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.usage += n
	if g.usage < 0 {
		g.usage = 0
	}
	g.mu.Unlock()
}

// MarkUnwritable dipanggil saat penulisan gagal karena izin/read-only.
func (g *DiskGuard) MarkUnwritable() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.writable = false
	g.checked = g.now()
	g.mu.Unlock()
}
