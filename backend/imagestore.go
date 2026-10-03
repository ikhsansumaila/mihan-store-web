package main

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
	"syscall"
	"time"
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

// storeHealth (opsional) dipakai pagar disk dan pemeriksaan "dapat ditulis".
type storeHealth interface {
	Writable() error
	Usage(ctx context.Context) (int64, error)
}

var (
	errStoreFull        = errors.New("penyimpanan penuh")
	errStoreUnavailable = errors.New("penyimpanan tidak tersedia")
	errBadKey           = errors.New("kunci berkas tidak valid")
)

// ---------- Kunci ----------

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	// Kunci foto utama yang sah. Nilai image_path lain (mis. "kerupuk1.jpg" peninggalan lama)
	// dianggap TIDAK punya foto.
	productImageKeyRe = regexp.MustCompile(`^products/([0-9]{1,20})/(` + uuidPattern + `)\.jpg$`)
	safeKeyRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*(/[a-z0-9][a-z0-9_.-]*)*$`)
)

// isProductImageKey: true bila s kunci foto utama yang dibuat server.
func isProductImageKey(s string) bool { return productImageKeyRe.MatchString(s) }

// productImageKeyFor: kunci foto utama milik produk id tertentu?
func productImageKeyFor(s string, id uint64) bool {
	m := productImageKeyRe.FindStringSubmatch(s)
	return m != nil && m[1] == strconv.FormatUint(id, 10)
}

// thumbKey menurunkan kunci thumbnail: products/12/<uuid>.jpg -> products/12/<uuid>_t.jpg.
func thumbKey(key string) string { return strings.TrimSuffix(key, ".jpg") + "_t.jpg" }

// newProductImageKey membuat kunci baru dengan UUID v4 acak; nama berkas dari klien tidak pernah dipakai.
func newProductImageKey(productID uint64) (string, error) {
	id, err := newPublicID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("products/%d/%s.jpg", productID, id), nil
}

// publicImageURLs mengubah nilai image_path menjadi URL publik {image, thumb}; kosong untuk nilai lama/kosong.
func publicImageURLs(store ImageStore, imagePath string) (string, string) {
	if store == nil || !isProductImageKey(imagePath) {
		return "", ""
	}
	return store.URL(imagePath), store.URL(thumbKey(imagePath))
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
		return "", errBadKey
	}
	p := filepath.Join(s.base, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.base, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errBadKey
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
		return wrapStoreErr(err)
	}
	tmp, err := os.CreateTemp(dir, ".unggah-*.tmp")
	if err != nil {
		return wrapStoreErr(err)
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
		return wrapStoreErr(err)
	}
	if err := tmp.Sync(); err != nil {
		return wrapStoreErr(err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		return wrapStoreErr(err)
	}
	if err := tmp.Close(); err != nil {
		return wrapStoreErr(err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return wrapStoreErr(err)
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

func wrapStoreErr(err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return fmt.Errorf("%w: %v", errStoreFull, err)
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("%w: %v", errStoreUnavailable, err)
	}
	return err
}

// ---------- Pagar disk ----------

// diskGuard menolak unggahan baru bila total ukuran uploads melebihi batas. Ukuran dihitung ulang
// berkala (cache `ttl`), tidak setiap permintaan; unggahan sukses menambah angka cache.
type diskGuard struct {
	mu       sync.Mutex
	health   storeHealth
	limit    int64
	ttl      time.Duration
	now      func() time.Time
	usage    int64
	measured time.Time
	writable bool
	checked  time.Time
}

func newDiskGuard(h storeHealth, limitBytes int64, ttl time.Duration) *diskGuard {
	return &diskGuard{health: h, limit: limitBytes, ttl: ttl, now: time.Now}
}

// startupCheck mencatat peringatan bila direktori tidak dapat ditulis (bagian lain toko tetap jalan).
func (g *diskGuard) startupCheck(dir string) {
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

// check: nil bila boleh mengunggah; errStoreUnavailable / errStoreFull bila tidak.
func (g *diskGuard) check(ctx context.Context) error {
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
		return errStoreUnavailable
	}
	if g.measured.IsZero() || now.Sub(g.measured) >= g.ttl {
		u, err := g.health.Usage(ctx)
		if err != nil {
			log.Printf("pagar disk: gagal menghitung ukuran uploads: %v", err)
			return errStoreUnavailable
		}
		g.usage, g.measured = u, now
	}
	if g.limit > 0 && g.usage >= g.limit {
		return errStoreFull
	}
	return nil
}

// add memperbarui angka cache setelah menulis/menghapus berkas.
func (g *diskGuard) add(n int64) {
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

// markUnwritable dipanggil saat penulisan gagal karena izin/read-only.
func (g *diskGuard) markUnwritable() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.writable = false
	g.checked = g.now()
	g.mu.Unlock()
}
