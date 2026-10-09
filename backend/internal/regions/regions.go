package regions

// Data wilayah Indonesia (provinsi, kabupaten/kota, kecamatan, kelurahan/desa).
//
// Sumber: https://wilayah.id/api (JSON statis). Disimpan di tabel region_datasets dalam format JSON,
// SATU baris per "dataset induk":
//   provinces                 -> semua provinsi            (level 1, parent_code NULL)
//   regencies:{kodeProvinsi}  -> kab/kota satu provinsi    (level 2)
//   districts:{kodeKabKota}   -> kecamatan satu kab/kota   (level 3)
//   villages:{kodeKecamatan}  -> kelurahan/desa satu kec.  (level 4)
// Kolom data berisi array [{code,name}] seperti dari API.
//
// File ini: format & validasi kode, cache memori, API publik (hanya baca), verifikasi rantai
// wilayah saat checkout, dan pembentuk alamat tersusun. Pengambilan & impor: regions_fetch.go,
// regions_import.go; admin: admin_regions.go.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

const (
	LevelProvince = 1
	LevelRegency  = 2
	LevelDistrict = 3
	LevelVillage  = 4

	MsgUnavailable    = "Data wilayah belum tersedia"
	msgRegionNotFound = "Wilayah tidak ditemukan"
	msgRegionBadCode  = "Kode wilayah tidak valid"
	msgTooManyRegion  = "Terlalu banyak permintaan data wilayah. Coba lagi sebentar lagi."
	MsgRefresh        = "Data wilayah tidak lengkap. Perbarui halaman lalu coba lagi."

	regionCacheTTL     = 10 * time.Minute
	regionCacheMaxKeys = 12000
	regionNameMaxRunes = 100
)

// Item adalah satu wilayah: {code, name} (bentuk sama seperti wilayah.id).
type Item struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var regionCodeRe = [5]*regexp.Regexp{
	nil,
	regexp.MustCompile(`^[0-9]{2}$`),
	regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}$`),
	regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$`),
	regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{2}\.[0-9]{4}$`),
}

// regionLevelNames: nama tingkat untuk pesan & ringkasan (indeks = level).
var regionLevelNames = [5]string{"", "provinsi", "kabupaten/kota", "kecamatan", "kelurahan/desa"}

// regionLevelKeys: kunci JSON per tingkat (counts/diff/status).
var regionLevelKeys = [5]string{"", "provinces", "regencies", "districts", "villages"}

// validRegionCode: kode berformat bertitik sesuai tingkatnya (11 / 11.01 / 11.01.01 / 11.01.01.2001).
func validRegionCode(level int, code string) bool {
	if level < 1 || level > 4 || len(code) > 13 {
		return false
	}
	return regionCodeRe[level].MatchString(code)
}

// normalizeRegionCode merapikan masukan kode dari klien: spasi dibuang, dan bentuk tanpa titik
// (3174 / 317406 / 3174061001) diubah ke bentuk bertitik. Hasil tetap harus lolos validRegionCode.
func normalizeRegionCode(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 20 || strings.ContainsAny(s, ".") || s == "" {
		return s
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return s
		}
	}
	switch len(s) {
	case 4:
		return s[:2] + "." + s[2:]
	case 6:
		return s[:2] + "." + s[2:4] + "." + s[4:]
	case 10:
		return s[:2] + "." + s[2:4] + "." + s[4:6] + "." + s[6:]
	}
	return s
}

// regionLevelOf menebak tingkat dari bentuk kode (0 bila tidak valid).
func regionLevelOf(code string) int {
	for l := 1; l <= 4; l++ {
		if validRegionCode(l, code) {
			return l
		}
	}
	return 0
}

// regionParentOf: kode induk ("11.01.01.2001" -> "11.01.01"; provinsi -> "").
func regionParentOf(code string) string {
	if i := strings.LastIndexByte(code, '.'); i > 0 {
		return code[:i]
	}
	return ""
}

// regionDatasetKey: kunci baris dataset untuk daftar anak tingkat `level` di bawah `parent`.
func regionDatasetKey(level int, parent string) string {
	switch level {
	case LevelProvince:
		return "provinces"
	case LevelRegency:
		return "regencies:" + parent
	case LevelDistrict:
		return "districts:" + parent
	case LevelVillage:
		return "villages:" + parent
	}
	return ""
}

// ParseDatasetKey kebalikan regionDatasetKey (ok=false bila format salah).
func ParseDatasetKey(key string) (level int, parent string, ok bool) {
	if key == "provinces" {
		return 1, "", true
	}
	prefix, p, found := strings.Cut(key, ":")
	if !found {
		return 0, "", false
	}
	for l := 2; l <= 4; l++ {
		if prefix == regionLevelKeys[l] && validRegionCode(l-1, p) {
			return l, p, true
		}
	}
	return 0, "", false
}

// ItemsHash: SHA-256 (hex) dari JSON kanonik array item (urutan sumber dipertahankan).
func ItemsHash(items []Item) (string, []byte) {
	if items == nil {
		items = []Item{}
	}
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b
}

// ---------- Alamat tersusun ----------

// Ref / DTO: wilayah pesanan (kode + nama), nil untuk pesanan lama.
type Ref struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type DTO struct {
	Province Ref `json:"province"`
	Regency  Ref `json:"regency"`
	District Ref `json:"district"`
	Village  Ref `json:"village"`
}

// flattenAddress menjadikan alamat multi-baris satu baris ("a\nb" -> "a, b").
func flattenAddress(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.Join(strings.Fields(p), " "), " ,")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// ComposeAddress: "<alamat lengkap>, <kelurahan/desa>, Kec. <kecamatan>, <kab/kota>, <provinsi> <kode pos>".
// Pesanan lama (tanpa wilayah): "<alamat>, <kota> <kode pos>".
func ComposeAddress(address string, region *DTO, city string, postal *string) string {
	parts := []string{}
	if a := flattenAddress(address); a != "" {
		parts = append(parts, a)
	}
	pc := ""
	if postal != nil {
		pc = strings.TrimSpace(*postal)
	}
	var last string
	if region != nil && region.Village.Name != "" {
		parts = append(parts, region.Village.Name, "Kec. "+region.District.Name, region.Regency.Name)
		last = region.Province.Name
	} else {
		last = strings.TrimSpace(city)
	}
	if pc != "" {
		last = strings.TrimSpace(last + " " + pc)
	}
	if last != "" {
		parts = append(parts, last)
	}
	return strings.Join(parts, ", ")
}

// ---------- Cache memori ----------

type regionDataset struct {
	Key       string
	Items     []Item
	UpdatedAt string // YYYY-MM-DD (bisa kosong)
	Hash      string
	Body      []byte // respons API publik siap kirim
	ETag      string
	loadedAt  time.Time
}

func (d *regionDataset) name(code string) (string, bool) {
	for _, it := range d.Items {
		if it.Code == code {
			return it.Name, true
		}
	}
	return "", false
}

type regionCache struct {
	mu  sync.RWMutex
	m   map[string]*regionDataset
	gen uint64
	ttl time.Duration
	now func() time.Time
}

func newRegionCache() *regionCache {
	return &regionCache{m: map[string]*regionDataset{}, ttl: regionCacheTTL, now: time.Now}
}

func (c *regionCache) get(key string) (*regionDataset, uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d := c.m[key]
	if d != nil && c.now().Sub(d.loadedAt) > c.ttl {
		d = nil
	}
	return d, c.gen
}

// put menyimpan hasil baca bila cache tidak dibersihkan sejak pembacaan dimulai (gen sama).
func (c *regionCache) put(d *regionDataset, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if len(c.m) >= regionCacheMaxKeys {
		now := c.now()
		for k, v := range c.m {
			if now.Sub(v.loadedAt) > c.ttl {
				delete(c.m, k)
			}
		}
		if len(c.m) >= regionCacheMaxKeys {
			c.m = map[string]*regionDataset{}
		}
	}
	c.m[d.Key] = d
}

// invalidate dipanggil setelah Simpan: semua dataset dibaca ulang dari DB.
func (c *regionCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.m = map[string]*regionDataset{}
}

var ErrUnavailable = errors.New(MsgUnavailable)

// loadDataset membaca satu dataset hidup (cache dulu). nil, nil = tidak ada.
func (s *Service) loadDataset(ctx context.Context, db *gorm.DB, key string) (*regionDataset, error) {
	if d, _ := s.cache.get(key); d != nil {
		return d, nil
	}
	_, gen := s.cache.get("")
	var rows []struct {
		Data      string  `gorm:"column:data"`
		Hash      string  `gorm:"column:content_hash"`
		UpdatedAt *string `gorm:"column:updated"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT data, content_hash, DATE_FORMAT(source_updated_at, '%Y-%m-%d') AS updated
		FROM region_datasets WHERE dataset_key = ? AND deleted_at IS NULL LIMIT 1`, key).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var items []Item
	if err := json.Unmarshal([]byte(rows[0].Data), &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []Item{}
	}
	d := &regionDataset{Key: key, Items: items, Hash: rows[0].Hash, loadedAt: s.cache.now()}
	if rows[0].UpdatedAt != nil {
		d.UpdatedAt = *rows[0].UpdatedAt
	}
	var meta map[string]any
	if d.UpdatedAt != "" {
		meta = map[string]any{"updatedAt": d.UpdatedAt}
	} else {
		meta = map[string]any{"updatedAt": nil}
	}
	body, _ := json.Marshal(map[string]any{"data": items, "meta": meta})
	d.Body = append(body, '\n')
	sum := sha256.Sum256([]byte(d.Hash + "|" + d.UpdatedAt))
	d.ETag = `"` + hex.EncodeToString(sum[:12]) + `"`
	s.cache.put(d, gen)
	return d, nil
}

// dataReady: true bila dataset provinsi sudah pernah diimpor.
func (s *Service) dataReady(ctx context.Context, db *gorm.DB) (bool, error) {
	d, err := s.loadDataset(ctx, db, "provinces")
	return d != nil && len(d.Items) > 0, err
}

// ---------- API publik ----------

func etagMatch(header, etag string) bool {
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if p == "*" || strings.TrimPrefix(p, "W/") == etag {
			return true
		}
	}
	return false
}

func noStoreError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	platform.WriteError(w, status, msg)
}

// PublicRegions: GET /api/regions/provinces, /regencies/{kode}, /districts/{kode}, /villages/{kode}.
func (s *Service) PublicRegions(level int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := s.Limiter.Allow(platform.RateKey(s.clientIP(r))); !ok {
			platform.RetryAfter(w, wait)
			noStoreError(w, http.StatusTooManyRequests, msgTooManyRegion)
			return
		}
		parent := ""
		if level > 1 {
			parent = normalizeRegionCode(mux.Vars(r)["code"])
			if !validRegionCode(level-1, parent) {
				noStoreError(w, http.StatusBadRequest, msgRegionBadCode)
				return
			}
		}
		db := s.db()
		if db == nil {
			noStoreError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		ready, err := s.dataReady(r.Context(), db)
		if err != nil {
			log.Printf("wilayah publik: %v", err)
			noStoreError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		if !ready {
			noStoreError(w, http.StatusServiceUnavailable, MsgUnavailable)
			return
		}
		d, err := s.loadDataset(r.Context(), db, regionDatasetKey(level, parent))
		if err != nil {
			log.Printf("wilayah publik: %v", err)
			noStoreError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		if d == nil {
			noStoreError(w, http.StatusNotFound, msgRegionNotFound)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "public, max-age=3600")
		h.Set("ETag", d.ETag)
		if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatch(inm, d.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(d.Body)
	}
}

// ---------- Verifikasi rantai wilayah (checkout) ----------

type CodesInput struct {
	Province, Regency, District, Village string
}

// ResolveChain memeriksa kode (format, lalu keberadaan di data tersimpan, berjenjang) dan
// mengembalikan NAMA dari database. Kesalahan: *platform.FieldError (422), ErrUnavailable (503), lainnya (DB).
func (s *Service) ResolveChain(ctx context.Context, db *gorm.DB, in CodesInput) (*DTO, error) {
	codes := [5]string{"", normalizeRegionCode(in.Province), normalizeRegionCode(in.Regency),
		normalizeRegionCode(in.District), normalizeRegionCode(in.Village)}
	fields := [5]string{"", "provinceCode", "regencyCode", "districtCode", "villageCode"}
	for l := 1; l <= 4; l++ {
		if codes[l] == "" {
			return nil, &platform.FieldError{Field: fields[l], Msg: "Pilih " + regionLevelNames[l]}
		}
		if !validRegionCode(l, codes[l]) {
			return nil, &platform.FieldError{Field: fields[l], Msg: "Kode " + regionLevelNames[l] + " tidak valid"}
		}
		if l > 1 && regionParentOf(codes[l]) != codes[l-1] {
			return nil, &platform.FieldError{Field: fields[l], Msg: strings.ToUpper(regionLevelNames[l][:1]) + regionLevelNames[l][1:] +
				" tidak sesuai dengan " + regionLevelNames[l-1] + " yang dipilih"}
		}
	}
	ready, err := s.dataReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrUnavailable
	}
	var refs [5]Ref
	for l := 1; l <= 4; l++ {
		d, err := s.loadDataset(ctx, db, regionDatasetKey(l, codes[l-1]))
		if err != nil {
			return nil, err
		}
		name, ok := "", false
		if d != nil {
			name, ok = d.name(codes[l])
		}
		if !ok {
			return nil, &platform.FieldError{Field: fields[l], Msg: strings.ToUpper(regionLevelNames[l][:1]) + regionLevelNames[l][1:] +
				" tidak ditemukan. Pilih ulang dari daftar."}
		}
		refs[l] = Ref{Code: codes[l], Name: name}
	}
	return &DTO{Province: refs[1], Regency: refs[2], District: refs[3], Village: refs[4]}, nil
}
