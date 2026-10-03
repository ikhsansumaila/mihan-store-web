package main

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
)

const (
	regionLevelProvince = 1
	regionLevelRegency  = 2
	regionLevelDistrict = 3
	regionLevelVillage  = 4

	msgRegionUnavailable = "Data wilayah belum tersedia"
	msgRegionNotFound    = "Wilayah tidak ditemukan"
	msgRegionBadCode     = "Kode wilayah tidak valid"
	msgTooManyRegion     = "Terlalu banyak permintaan data wilayah. Coba lagi sebentar lagi."
	msgRegionRefresh     = "Data wilayah tidak lengkap. Perbarui halaman lalu coba lagi."

	regionCacheTTL     = 10 * time.Minute
	regionCacheMaxKeys = 12000
	regionNameMaxRunes = 100
)

// RegionItem adalah satu wilayah: {code, name} (bentuk sama seperti wilayah.id).
type RegionItem struct {
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
	case regionLevelProvince:
		return "provinces"
	case regionLevelRegency:
		return "regencies:" + parent
	case regionLevelDistrict:
		return "districts:" + parent
	case regionLevelVillage:
		return "villages:" + parent
	}
	return ""
}

// parseRegionDatasetKey kebalikan regionDatasetKey (ok=false bila format salah).
func parseRegionDatasetKey(key string) (level int, parent string, ok bool) {
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

// regionItemsHash: SHA-256 (hex) dari JSON kanonik array item (urutan sumber dipertahankan).
func regionItemsHash(items []RegionItem) (string, []byte) {
	if items == nil {
		items = []RegionItem{}
	}
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b
}

// ---------- Alamat tersusun ----------

// RegionRef / RegionDTO: wilayah pesanan (kode + nama), nil untuk pesanan lama.
type RegionRef struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type RegionDTO struct {
	Province RegionRef `json:"province"`
	Regency  RegionRef `json:"regency"`
	District RegionRef `json:"district"`
	Village  RegionRef `json:"village"`
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

// composeAddress: "<alamat lengkap>, <kelurahan/desa>, Kec. <kecamatan>, <kab/kota>, <provinsi> <kode pos>".
// Pesanan lama (tanpa wilayah): "<alamat>, <kota> <kode pos>".
func composeAddress(address string, region *RegionDTO, city string, postal *string) string {
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
	Items     []RegionItem
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

var errRegionUnavailable = errors.New(msgRegionUnavailable)

// loadRegionDataset membaca satu dataset hidup (cache dulu). nil, nil = tidak ada.
func (a *App) loadRegionDataset(ctx context.Context, db *gorm.DB, key string) (*regionDataset, error) {
	if d, _ := a.regions.get(key); d != nil {
		return d, nil
	}
	_, gen := a.regions.get("")
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
	var items []RegionItem
	if err := json.Unmarshal([]byte(rows[0].Data), &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []RegionItem{}
	}
	d := &regionDataset{Key: key, Items: items, Hash: rows[0].Hash, loadedAt: a.regions.now()}
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
	a.regions.put(d, gen)
	return d, nil
}

// regionDataReady: true bila dataset provinsi sudah pernah diimpor.
func (a *App) regionDataReady(ctx context.Context, db *gorm.DB) (bool, error) {
	d, err := a.loadRegionDataset(ctx, db, "provinces")
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
	writeError(w, status, msg)
}

// PublicRegions: GET /api/regions/provinces, /regencies/{kode}, /districts/{kode}, /villages/{kode}.
func (a *App) PublicRegions(level int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := a.regionLimiter.Allow(rateKey(a.ips.ClientIP(r))); !ok {
			retryAfter(w, wait)
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
		db := a.db.Load()
		if db == nil {
			noStoreError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		ready, err := a.regionDataReady(r.Context(), db)
		if err != nil {
			log.Printf("wilayah publik: %v", err)
			noStoreError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		if !ready {
			noStoreError(w, http.StatusServiceUnavailable, msgRegionUnavailable)
			return
		}
		d, err := a.loadRegionDataset(r.Context(), db, regionDatasetKey(level, parent))
		if err != nil {
			log.Printf("wilayah publik: %v", err)
			noStoreError(w, http.StatusServiceUnavailable, msgServiceDown)
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

// fieldError: kesalahan validasi satu field (422 {error, field}).
type fieldError struct {
	Field string
	Msg   string
}

func (e *fieldError) Error() string { return e.Msg }

type regionCodesInput struct {
	Province, Regency, District, Village string
}

// resolveRegionChain memeriksa kode (format, lalu keberadaan di data tersimpan, berjenjang) dan
// mengembalikan NAMA dari database. Kesalahan: *fieldError (422), errRegionUnavailable (503), lainnya (DB).
func (a *App) resolveRegionChain(ctx context.Context, db *gorm.DB, in regionCodesInput) (*RegionDTO, error) {
	codes := [5]string{"", normalizeRegionCode(in.Province), normalizeRegionCode(in.Regency),
		normalizeRegionCode(in.District), normalizeRegionCode(in.Village)}
	fields := [5]string{"", "provinceCode", "regencyCode", "districtCode", "villageCode"}
	for l := 1; l <= 4; l++ {
		if codes[l] == "" {
			return nil, &fieldError{fields[l], "Pilih " + regionLevelNames[l]}
		}
		if !validRegionCode(l, codes[l]) {
			return nil, &fieldError{fields[l], "Kode " + regionLevelNames[l] + " tidak valid"}
		}
		if l > 1 && regionParentOf(codes[l]) != codes[l-1] {
			return nil, &fieldError{fields[l], strings.ToUpper(regionLevelNames[l][:1]) + regionLevelNames[l][1:] +
				" tidak sesuai dengan " + regionLevelNames[l-1] + " yang dipilih"}
		}
	}
	ready, err := a.regionDataReady(ctx, db)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, errRegionUnavailable
	}
	var refs [5]RegionRef
	for l := 1; l <= 4; l++ {
		d, err := a.loadRegionDataset(ctx, db, regionDatasetKey(l, codes[l-1]))
		if err != nil {
			return nil, err
		}
		name, ok := "", false
		if d != nil {
			name, ok = d.name(codes[l])
		}
		if !ok {
			return nil, &fieldError{fields[l], strings.ToUpper(regionLevelNames[l][:1]) + regionLevelNames[l][1:] +
				" tidak ditemukan. Pilih ulang dari daftar."}
		}
		refs[l] = RegionRef{Code: codes[l], Name: name}
	}
	return &RegionDTO{Province: refs[1], Regency: refs[2], District: refs[3], Village: refs[4]}, nil
}
