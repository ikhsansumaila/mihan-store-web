package cart

// Keranjang belanja pelanggan (wajib login, sesi Bearer). Disimpan di database per pengguna.
// Harga TIDAK disimpan sebagai sumber kebenaran: respons selalu memakai harga efektif terkini
// (harga dasar + jenjang grosir, lihat catalog/pricing.go), dan produk yang nonaktif/terhapus ditandai
// available=false (tidak dihitung ke subtotal).
//
// cart_items.seen_unit_price = harga satuan efektif yang terakhir DILIHAT pelanggan untuk baris itu.
// Diisi saat item ditambahkan / jumlahnya diubah pelanggan / "Mengerti" (ack-prices), dan saat
// keranjang pertama kali dibuka bila masih NULL. catalog.PriceChanged = harga efektif sekarang (qty sama)
// != seen_unit_price.
//
// Catatan arsitektur: paket ini memakai aturan harga MURNI dari internal/catalog (pricing.go);
// data jenjang dan URL foto diberikan lewat dependensi eksplisit (Deps).

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/internal/catalog"
	"mihanstore/internal/platform"
)

const (
	// MaxLines: jumlah produk berbeda maksimal per keranjang; MaxItemQty: jumlah maksimal per produk.
	MaxLines   = 50
	MaxItemQty = 999

	msgTooManyCart     = "Terlalu banyak permintaan keranjang. Coba lagi sebentar lagi."
	msgProductNotAvail = "Produk tidak ditemukan atau sedang tidak tersedia"
)

// TierLoader memuat jenjang harga grosir untuk sekumpulan produk (produk tanpa jenjang tidak ada di peta).
type TierLoader func(db *gorm.DB, productIDs []uint64) (map[uint64][]catalog.PriceTier, error)

// Deps: dependensi modul keranjang, diberikan eksplisit oleh perakit.
type Deps struct {
	DB      func() *gorm.DB       // koneksi saat ini (nil bila belum terhubung)
	Limiter *platform.RateLimiter // per pengguna: tambah/ubah keranjang
	Tiers   TierLoader
	// ImageURLs mengubah image_path produk menjadi URL publik {image, thumb} ("" bila tidak ada foto).
	ImageURLs func(imagePath string) (image, thumb string)
	// UserID: id pelanggan yang login (rute sudah dibungkus middleware pelanggan).
	UserID         func(r *http.Request) uint64
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
}

// Service: keranjang. Limiter boleh diganti saat uji.
type Service struct {
	Limiter *platform.RateLimiter

	db             func() *gorm.DB
	tiers          TierLoader
	imageURLs      func(string) (string, string)
	userID         func(*http.Request) uint64
	respondTxError func(w http.ResponseWriter, err error, ctx string)
}

func New(d Deps) *Service {
	return &Service{Limiter: d.Limiter, db: d.DB, tiers: d.Tiers, imageURLs: d.ImageURLs, userID: d.UserID, respondTxError: d.RespondTxError}
}

type ItemDTO struct {
	ProductID uint64 `json:"productId" gorm:"column:product_id"`
	Name      string `json:"name" gorm:"column:name"`
	Category  string `json:"category" gorm:"column:category"`
	Image     string `json:"image" gorm:"column:image"`
	Price     int64  `json:"price" gorm:"column:price"` // harga dasar produk (field lama)
	Qty       int64  `json:"qty" gorm:"column:qty"`
	LineTotal int64  `json:"lineTotal" gorm:"-"` // unitPrice * qty
	Available bool   `json:"available" gorm:"column:available"`
	// Harga berjenjang (grosir) & satuan.
	Unit              string               `json:"unit" gorm:"column:unit"`
	BaseUnitPrice     int64                `json:"baseUnitPrice" gorm:"-"`
	UnitPrice         int64                `json:"unitPrice" gorm:"-"` // harga satuan efektif
	TierMinQty        *int64               `json:"tierMinQty" gorm:"-"`
	NextTier          *catalog.NextTierDTO `json:"nextTier" gorm:"-"`
	Savings           int64                `json:"savings" gorm:"-"` // hemat dibanding harga dasar (baris ini)
	PriceChanged      bool                 `json:"priceChanged" gorm:"-"`
	PreviousUnitPrice *int64               `json:"previousUnitPrice" gorm:"-"`
	// Foto produk (URL thumbnail, "" bila tidak ada). `image` = URL foto utama.
	Thumb string `json:"thumb" gorm:"-"`

	itemID uint64
	seen   *int64
}

type DTO struct {
	Items           []ItemDTO `json:"items"`
	Subtotal        int64     `json:"subtotal"`  // dengan harga efektif
	ItemCount       int64     `json:"itemCount"` // jumlah qty produk yang tersedia
	LineCount       int       `json:"lineCount"`
	HasUnavailable  bool      `json:"hasUnavailable"`
	MaxQty          int       `json:"maxQty"`
	MaxLines        int       `json:"maxLines"`
	Savings         int64     `json:"savings"`
	HasPriceChanges bool      `json:"hasPriceChanges"`
}

type cartItemRow struct {
	ItemID    uint64 `gorm:"column:item_id"`
	ProductID uint64 `gorm:"column:product_id"`
	Name      string `gorm:"column:name"`
	Category  string `gorm:"column:category"`
	Image     string `gorm:"column:image"`
	Price     int64  `gorm:"column:price"`
	Unit      string `gorm:"column:unit"`
	Qty       int64  `gorm:"column:qty"`
	Seen      *int64 `gorm:"column:seen_unit_price"`
	Available bool   `gorm:"column:available"`
}

const cartItemsSQL = `SELECT ci.id AS item_id, ci.product_id, p.name, c.slug AS category, COALESCE(p.image_path, '') AS image,
  p.price, p.unit, ci.qty, ci.seen_unit_price, (p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL) AS available
FROM carts ca
JOIN cart_items ci ON ci.cart_id = ca.id AND ci.deleted_at IS NULL
JOIN products p ON p.id = ci.product_id
JOIN categories c ON c.id = p.category_id
WHERE ca.user_id = ? AND ca.deleted_at IS NULL
ORDER BY ci.id`

// buildCartItem menghitung harga efektif, petunjuk jenjang berikutnya, hemat, dan penanda
// perubahan harga untuk satu baris keranjang (logika murni).
func buildCartItem(r cartItemRow, tiers []catalog.PriceTier) ItemDTO {
	it := ItemDTO{ProductID: r.ProductID, Name: r.Name, Category: r.Category, Image: r.Image, Price: r.Price,
		Qty: r.Qty, Available: r.Available, Unit: r.Unit, BaseUnitPrice: r.Price, itemID: r.ItemID, seen: r.Seen}
	unitPrice, tierMin := catalog.EffectiveUnitPrice(r.Price, tiers, r.Qty)
	it.UnitPrice = unitPrice
	it.LineTotal = unitPrice * r.Qty
	if tierMin > 0 {
		m := tierMin
		it.TierMinQty = &m
		it.Savings = (r.Price - unitPrice) * r.Qty
	}
	if r.Available {
		it.NextTier = catalog.NextTier(r.Price, tiers, r.Qty, MaxItemQty)
		if catalog.PriceChanged(unitPrice, r.Seen) {
			it.PriceChanged = true
			prev := *r.Seen
			it.PreviousUnitPrice = &prev
		}
	}
	return it
}

// AttachImages mengubah image_path tiap baris menjadi URL publik {image, thumb} ("" bila tidak ada foto).
func (s *Service) AttachImages(c *DTO) {
	for i := range c.Items {
		c.Items[i].Image, c.Items[i].Thumb = s.imageURLs(c.Items[i].Image)
	}
}

// Load membaca keranjang pengguna dengan harga efektif terkini.
func (s *Service) Load(db *gorm.DB, userID uint64) (*DTO, error) {
	var rows []cartItemRow
	if err := db.Raw(cartItemsSQL, userID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ProductID)
	}
	tiers, err := s.tiers(db, ids)
	if err != nil {
		return nil, err
	}
	c := &DTO{Items: make([]ItemDTO, 0, len(rows)), LineCount: len(rows), MaxQty: MaxItemQty, MaxLines: MaxLines}
	for _, r := range rows {
		it := buildCartItem(r, tiers[r.ProductID])
		if it.Available {
			c.Subtotal += it.LineTotal
			c.ItemCount += it.Qty
			c.Savings += it.Savings
			if it.PriceChanged {
				c.HasPriceChanges = true
			}
		} else {
			c.HasUnavailable = true
		}
		c.Items = append(c.Items, it)
	}
	return c, nil
}

// FillUnseenPrices mengisi seen_unit_price yang masih NULL dengan harga yang sedang ditampilkan
// (baris lama sebelum fitur ini / keranjang pertama kali dibuka). Best-effort.
func FillUnseenPrices(db *gorm.DB, c *DTO) {
	for _, it := range c.Items {
		if it.seen == nil && it.Available {
			if err := db.Exec(`UPDATE cart_items SET seen_unit_price = ? WHERE id = ? AND seen_unit_price IS NULL`, it.UnitPrice, it.itemID).Error; err != nil {
				log.Printf("keranjang: isi harga terlihat: %v", err)
				return
			}
		}
	}
}

// markSeen menyetel seen_unit_price = harga efektif sekarang untuk baris keranjang. productID 0 =
// semua baris (ack-prices). Dipanggil di dalam transaksi yang sudah mengunci keranjang.
func (s *Service) markSeen(tx *gorm.DB, cartID, productID uint64) error {
	q := `SELECT ci.id AS item_id, ci.product_id, p.price, ci.qty FROM cart_items ci JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = ? AND ci.deleted_at IS NULL`
	args := []any{cartID}
	if productID != 0 {
		q += ` AND ci.product_id = ?`
		args = append(args, productID)
	}
	var rows []cartItemRow
	if err := tx.Raw(q, args...).Scan(&rows).Error; err != nil {
		return err
	}
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ProductID)
	}
	tiers, err := s.tiers(tx, ids)
	if err != nil {
		return err
	}
	for _, r := range rows {
		p, _ := catalog.EffectiveUnitPrice(r.Price, tiers[r.ProductID], r.Qty)
		if err := tx.Exec(`UPDATE cart_items SET seen_unit_price = ? WHERE id = ?`, p, r.ItemID).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureCart membuat keranjang bila belum ada lalu mengunci barisnya (FOR UPDATE) agar
// perubahan keranjang dan checkout pengguna yang sama berjalan berurutan.
func ensureCart(tx *gorm.DB, userID uint64) (uint64, error) {
	if err := tx.Exec(`INSERT INTO carts (user_id) VALUES (?) AS n ON DUPLICATE KEY UPDATE user_id = n.user_id`, userID).Error; err != nil {
		return 0, err
	}
	return Lock(tx, userID)
}

// Lock mengunci keranjang aktif pengguna (FOR UPDATE); 0 bila belum ada.
func Lock(tx *gorm.DB, userID uint64) (uint64, error) {
	var ids []uint64
	if err := tx.Raw(`SELECT id FROM carts WHERE user_id = ? AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// productAvailable: produk ada, aktif, belum dihapus, dan kategorinya belum dihapus.
func productAvailable(tx *gorm.DB, productID uint64) (bool, error) {
	var n int64
	err := tx.Raw(`SELECT COUNT(*) FROM products p JOIN categories c ON c.id = p.category_id
		WHERE p.id = ? AND p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL`, productID).Scan(&n).Error
	return n > 0, err
}

func (s *Service) respondCart(w http.ResponseWriter, r *http.Request, status int) {
	userID := s.userID(r)
	db := s.db().WithContext(r.Context())
	c, err := s.Load(db, userID)
	if err != nil {
		log.Printf("keranjang: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	FillUnseenPrices(db, c)
	s.AttachImages(c)
	platform.WriteJSON(w, status, c)
}

// AckCartPrices: POST /api/cart/ack-prices — pelanggan menekan "Mengerti": harga yang terlihat
// untuk semua baris disetel ke harga efektif sekarang (penanda perubahan harga hilang).
func (s *Service) AckCartPrices(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	if !platform.LimitUser(w, s.Limiter, userID, msgTooManyCart) {
		return
	}
	err := s.db().WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		cartID, err := Lock(tx, userID)
		if err != nil || cartID == 0 {
			return err
		}
		return s.markSeen(tx, cartID, 0)
	})
	if err != nil {
		s.respondTxError(w, err, "ack harga keranjang")
		return
	}
	s.respondCart(w, r, http.StatusOK)
}

func (s *Service) GetCart(w http.ResponseWriter, r *http.Request) {
	s.respondCart(w, r, http.StatusOK)
}

type cartItemInput struct {
	ProductID uint64 `json:"productId"`
	Qty       *int64 `json:"qty"`
}

// AddCartItem: POST /api/cart/items {productId, qty?=1}. Qty ditambahkan ke yang sudah ada
// (maksimal 999 per produk).
func (s *Service) AddCartItem(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	if !platform.LimitUser(w, s.Limiter, userID, msgTooManyCart) {
		return
	}
	var in cartItemInput
	if err := platform.DecodeJSONLenient(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	qty := int64(1)
	if in.Qty != nil {
		qty = *in.Qty
	}
	if in.ProductID == 0 {
		platform.WriteError(w, http.StatusBadRequest, "productId wajib diisi")
		return
	}
	if qty < 1 || qty > MaxItemQty {
		platform.WriteError(w, http.StatusBadRequest, "Jumlah harus 1 sampai 999")
		return
	}
	err := s.db().WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		cartID, err := ensureCart(tx, userID)
		if err != nil {
			return err
		}
		ok, err := productAvailable(tx, in.ProductID)
		if err != nil {
			return err
		}
		if !ok {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgProductNotAvail}
		}
		var existing, lines int64
		if err := tx.Raw(`SELECT COUNT(*) FROM cart_items WHERE cart_id = ? AND product_id = ?`, cartID, in.ProductID).Scan(&existing).Error; err != nil {
			return err
		}
		if existing == 0 {
			if err := tx.Raw(`SELECT COUNT(*) FROM cart_items WHERE cart_id = ?`, cartID).Scan(&lines).Error; err != nil {
				return err
			}
			if lines >= MaxLines {
				return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Keranjang maksimal berisi 50 produk berbeda"}
			}
		}
		if err := tx.Exec(`INSERT INTO cart_items (cart_id, product_id, qty) VALUES (?, ?, ?) AS n
			ON DUPLICATE KEY UPDATE qty = LEAST(cart_items.qty + n.qty, 999)`, cartID, in.ProductID, qty).Error; err != nil {
			return err
		}
		// Pelanggan sendiri menambah: harga yang kini tampil = harga yang dilihat (bukan "perubahan harga").
		return s.markSeen(tx, cartID, in.ProductID)
	})
	if err != nil {
		s.respondTxError(w, err, "tambah keranjang")
		return
	}
	s.respondCart(w, r, http.StatusOK)
}

// SetCartItem: PUT /api/cart/items {productId, qty}. qty 0 = hapus.
func (s *Service) SetCartItem(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	if !platform.LimitUser(w, s.Limiter, userID, msgTooManyCart) {
		return
	}
	var in cartItemInput
	if err := platform.DecodeJSONLenient(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if in.ProductID == 0 || in.Qty == nil {
		platform.WriteError(w, http.StatusBadRequest, "productId dan qty wajib diisi")
		return
	}
	qty := *in.Qty
	if qty < 0 || qty > MaxItemQty {
		platform.WriteError(w, http.StatusBadRequest, "Jumlah harus 0 sampai 999 (0 = hapus)")
		return
	}
	err := s.db().WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if qty == 0 {
			cartID, err := Lock(tx, userID)
			if err != nil || cartID == 0 {
				return err
			}
			return tx.Exec(`DELETE FROM cart_items WHERE cart_id = ? AND product_id = ?`, cartID, in.ProductID).Error
		}
		cartID, err := ensureCart(tx, userID)
		if err != nil {
			return err
		}
		ok, err := productAvailable(tx, in.ProductID)
		if err != nil {
			return err
		}
		if !ok {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgProductNotAvail}
		}
		var existing, lines int64
		if err := tx.Raw(`SELECT COUNT(*) FROM cart_items WHERE cart_id = ? AND product_id = ?`, cartID, in.ProductID).Scan(&existing).Error; err != nil {
			return err
		}
		if existing == 0 {
			if err := tx.Raw(`SELECT COUNT(*) FROM cart_items WHERE cart_id = ?`, cartID).Scan(&lines).Error; err != nil {
				return err
			}
			if lines >= MaxLines {
				return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Keranjang maksimal berisi 50 produk berbeda"}
			}
		}
		if err := tx.Exec(`INSERT INTO cart_items (cart_id, product_id, qty) VALUES (?, ?, ?) AS n
			ON DUPLICATE KEY UPDATE qty = n.qty`, cartID, in.ProductID, qty).Error; err != nil {
			return err
		}
		// Perubahan harga karena pelanggan mengubah jumlah tidak dianggap "perubahan harga".
		return s.markSeen(tx, cartID, in.ProductID)
	})
	if err != nil {
		s.respondTxError(w, err, "ubah keranjang")
		return
	}
	s.respondCart(w, r, http.StatusOK)
}

// DeleteCartItem: DELETE /api/cart/items/{productId}.
func (s *Service) DeleteCartItem(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	pid, err := strconv.ParseUint(mux.Vars(r)["productId"], 10, 64)
	if err != nil || pid == 0 {
		platform.WriteError(w, http.StatusNotFound, msgProductNotAvail)
		return
	}
	err = s.db().WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		cartID, err := Lock(tx, userID)
		if err != nil || cartID == 0 {
			return err
		}
		return tx.Exec(`DELETE FROM cart_items WHERE cart_id = ? AND product_id = ?`, cartID, pid).Error
	})
	if err != nil {
		s.respondTxError(w, err, "hapus item keranjang")
		return
	}
	s.respondCart(w, r, http.StatusOK)
}

// ClearCart: DELETE /api/cart.
func (s *Service) ClearCart(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	err := s.db().WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		cartID, err := Lock(tx, userID)
		if err != nil || cartID == 0 {
			return err
		}
		return tx.Exec(`DELETE FROM cart_items WHERE cart_id = ?`, cartID).Error
	})
	if err != nil {
		s.respondTxError(w, err, "kosongkan keranjang")
		return
	}
	s.respondCart(w, r, http.StatusOK)
}
