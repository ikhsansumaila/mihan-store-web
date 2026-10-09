package orders

// Modul pesanan: checkout & pesanan pelanggan (http_customer.go), pengelolaan pesanan & pengaturan toko
// oleh admin (http_admin.go), bukti transfer privat + retensi (proofs.go, proofstore.go, proofs_purge.go),
// saran & default ongkir per kelurahan (shipping.go), akses data (repo.go), validasi (validate.go), dan
// aturan murni (domain.go).
//
// Antarmuka ke modul lain didefinisikan DI SINI (sisi pemakai) dan diisi oleh perakit (internal/app):
// Events (Discord + Web Push), CartReader (keranjang), Pricing (harga efektif katalog), RegionResolver
// (data wilayah), serta fungsi pelaku (Customer/Admin) dan normalisasi telepon (identity).
// Paket ini hanya memakai TIPE data wilayah dari internal/regions (DTO/CodesInput/ComposeAddress).

import (
	"context"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
)

// Person: pengguna yang login (pelanggan atau admin) sebatas yang dibutuhkan modul pesanan.
type Person struct {
	ID       uint64
	Username string
	Label    string // label pelaku activity log (email, atau username bila email kosong)
}

// uid: id pelaku untuk activity log (nil bila tidak ada pelaku).
func (p *Person) uid() *uint64 {
	if p == nil {
		return nil
	}
	id := p.ID
	return &id
}

func (p *Person) label() string {
	if p == nil {
		return ""
	}
	return p.Label
}

// Events: notifikasi SETELAH commit (Discord pengelola + Web Push admin/pelanggan). Implementasi membaca
// ringkasan pesanan sendiri dan mengirim asinkron; kegagalan tidak memengaruhi respons.
type Events interface {
	OrderCreated(db *gorm.DB, orderID uint64)             // pesanan baru (Discord + push admin)
	OrderCancelledByCustomer(db *gorm.DB, orderID uint64) // dibatalkan pemilik (Discord + push admin)
	PaymentProofUploaded(db *gorm.DB, orderID uint64)     // bukti transfer diunggah/diganti (Discord + push admin)
	PricingSet(db *gorm.DB, orderID uint64)               // ongkir/total dikonfirmasi atau diubah (push pelanggan)
	OrderPaid(db *gorm.DB, orderID uint64)                // dibayar (Discord + push pelanggan)
	OrderCompleted(db *gorm.DB, orderID uint64)           // selesai (push pelanggan)
	OrderCancelledByAdmin(db *gorm.DB, orderID uint64)    // dibatalkan admin (Discord + push pelanggan)
}

// CartReader: keranjang pelanggan (modul cart).
type CartReader interface {
	// Lock mengunci keranjang pengguna di dalam transaksi checkout (0 = belum punya keranjang).
	Lock(tx *gorm.DB, userID uint64) (cartID uint64, err error)
	// Snapshot: keranjang terkini lengkap (harga terlihat, foto) untuk respons 409 price_changed.
	Snapshot(db *gorm.DB, userID uint64) (any, error)
}

// PriceLine / PriceResult: masukan & hasil harga satuan efektif (harga dasar + jenjang grosir).
type PriceLine struct {
	ProductID uint64
	BasePrice int64
	Qty       int64
}

type PriceResult struct {
	UnitPrice  int64
	TierMinQty int64 // 0 = harga eceran
}

// Pricing: aturan harga modul katalog.
type Pricing interface {
	// UnitPrices: harga efektif per baris (urutan sama dengan masukan), jenjang dibaca di dalam tx.
	UnitPrices(tx *gorm.DB, lines []PriceLine) ([]PriceResult, error)
	// CheckExpectedTotal: galat "harga berubah" bila total yang dilihat pelanggan berbeda (nil = tidak diperiksa).
	CheckExpectedTotal(expected *int64, total int64) error
	IsPriceChanged(err error) bool
	PriceChangedMessage() string
}

// RegionResolver: kode wilayah dari klien -> nama dari data wilayah di database (modul regions).
type RegionResolver interface {
	ResolveChain(ctx context.Context, db *gorm.DB, in regions.CodesInput) (*regions.DTO, error)
}

// Slots: batas pemrosesan gambar paralel (dipakai bersama foto produk).
type Slots interface {
	Acquire(ctx context.Context) (func(), bool)
}

// Deps: dependensi modul pesanan.
type Deps struct {
	DB             func() *gorm.DB // koneksi saat ini (nil bila belum terhubung)
	Now            func() time.Time
	Audit          *audit.Logger
	Customer       func(r *http.Request) *Person // pelanggan yang login (rute dibungkus middleware pelanggan)
	Admin          func(r *http.Request) *Person // admin pelaku (rute dibungkus middleware admin)
	NormalizePhone func(string) (string, error)
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
	Events         Events
	Cart           CartReader
	MaxCartLines   int   // jumlah produk berbeda maksimal per keranjang
	MaxItemQty     int64 // jumlah maksimal per produk
	Pricing        Pricing
	Regions        RegionResolver
	ImageSlots     Slots
	Proofs         *ProofStore // nil = PAYMENT_PROOF_DIR belum diisi (unggah/lihat bukti 503)
}

// Service: modul pesanan. Limiter dan Proofs boleh diganti saat uji.
type Service struct {
	CheckoutLimiter  *platform.RateLimiter // per pengguna: buat pesanan
	CancelLimiter    *platform.RateLimiter // per pengguna: batalkan pesanan
	ProofLimiter     *platform.RateLimiter // per pelanggan: unggah/hapus bukti transfer
	ProofViewLimiter *platform.RateLimiter // per pelanggan: lihat bukti transfer
	Proofs           *ProofStore           // penyimpanan PRIVAT bukti transfer (PAYMENT_PROOF_DIR)

	db             func() *gorm.DB
	now            func() time.Time
	audit          *audit.Logger
	customer       func(r *http.Request) *Person
	admin          func(r *http.Request) *Person
	normalizePhone func(string) (string, error)
	respondTxError func(w http.ResponseWriter, err error, ctx string)
	events         Events
	cart           CartReader
	maxCartLines   int
	maxItemQty     int64
	pricing        Pricing
	regions        RegionResolver
	imageSlots     Slots
}

func New(d Deps) *Service {
	return &Service{
		CheckoutLimiter:  platform.NewRateLimiter(10, 10*time.Minute),
		CancelLimiter:    platform.NewRateLimiter(10, 10*time.Minute),
		ProofLimiter:     platform.NewRateLimiter(10, 10*time.Minute),
		ProofViewLimiter: platform.NewRateLimiter(120, time.Minute),
		Proofs:           d.Proofs,

		db: d.DB, now: d.Now, audit: d.Audit, customer: d.Customer, admin: d.Admin,
		normalizePhone: d.NormalizePhone, respondTxError: d.RespondTxError, events: d.Events,
		cart: d.Cart, maxCartLines: d.MaxCartLines, maxItemQty: d.MaxItemQty, pricing: d.Pricing,
		regions: d.Regions, imageSlots: d.ImageSlots,
	}
}
