package catalog

// Modul katalog: produk, kategori, jenjang harga grosir (pricing.go = aturan murni), API publik
// (http_public.go), CRUD admin (http_admin.go) dan unggah foto produk (http_admin_images.go,
// pemrosesan/penyimpanan gambar di subpaket media). Dependensi lintas modul diberikan eksplisit
// lewat Deps oleh perakit (internal/app).

import (
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
)

// Deps: dependensi modul katalog.
type Deps struct {
	DB    func() *gorm.DB // koneksi saat ini (nil bila belum terhubung)
	Now   func() time.Time
	Audit *audit.Logger
	// Actor: pelaku admin rute /api/admin/* (id untuk activity log & batas laju, label email/username).
	Actor          func(r *http.Request) (id *uint64, label string)
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
	ImageLimiter   *platform.RateLimiter // per admin: unggah/hapus foto
	ImageSlots     imaging.Slots         // batas pemrosesan gambar paralel (dipakai bersama bukti transfer)
}

// Service: modul katalog. ImageLimiter dan ImageTxHook boleh diganti saat uji.
type Service struct {
	ImageLimiter *platform.RateLimiter
	// ImageTxHook KHUSUS TES: dipanggil di dalam transaksi foto sebelum commit (nil di produksi).
	ImageTxHook func(tx *gorm.DB) error

	db             func() *gorm.DB
	now            func() time.Time
	audit          *audit.Logger
	actor          func(r *http.Request) (*uint64, string)
	respondTxError func(w http.ResponseWriter, err error, ctx string)
	images         media.ImageStore
	imageGuard     *media.DiskGuard
	imageSlots     imaging.Slots
}

func New(d Deps) *Service {
	return &Service{ImageLimiter: d.ImageLimiter, db: d.DB, now: d.Now, audit: d.Audit, actor: d.Actor,
		respondTxError: d.RespondTxError, imageSlots: d.ImageSlots}
}

// SetImageStore memasang ImageStore beserta pagar disknya (dipakai perakit dan tes).
func (a *Service) SetImageStore(s media.ImageStore, maxMB int64) {
	a.images = s
	var h media.StoreHealth
	if sh, ok := s.(media.StoreHealth); ok {
		h = sh
	}
	a.imageGuard = media.NewDiskGuard(h, maxMB<<20, 5*time.Minute)
}

// ImageGuard: pagar disk foto produk (pemeriksaan saat start, tes).
func (a *Service) ImageGuard() *media.DiskGuard { return a.imageGuard }

// ImageURLs mengubah image_path produk menjadi URL publik {image, thumb} ("" bila tidak ada foto).
func (a *Service) ImageURLs(imagePath string) (string, string) {
	return media.PublicImageURLs(a.images, imagePath)
}
