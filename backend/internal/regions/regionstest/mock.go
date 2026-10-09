package regionstest

// Paket regionstest: SATU server tiruan wilayah.id (kecil: 2 provinsi) yang dipakai bersama tes unit
// internal/regions serta tes integrasi, kontrak, dan e2e internal/app. Tes otomatis TIDAK PERNAH memanggil
// wilayah.id asli. Hanya diimpor tes (tidak ikut biner server).
//
// Paket ini sengaja TIDAK mengimpor internal/regions (agar tes di dalam paket regions bisa memakainya tanpa
// siklus impor): Item berbentuk sama dengan Item (JSON {code, name}).

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Item: satu wilayah (bentuk JSON sama dengan regions.Item).
type Item = struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Tree: dataset_key -> item. Bisa diubah per tes (salinan baru tiap panggilan).
// It membuat satu item wilayah (literal berkunci dihindari agar tabel pohon tetap ringkas).
func It(code, name string) Item { return Item{Code: code, Name: name} }

func Tree() map[string][]Item {
	return map[string][]Item{
		"provinces": {It("36", "Banten"), It("31", "DKI Jakarta")},
		"regencies:31": {
			It("31.71", "Kota Administrasi Jakarta Pusat"),
			It("31.74", "Kota Administrasi Jakarta Selatan"),
		},
		"regencies:36":      {It("36.03", "Kabupaten Tangerang"), It("36.71", "Kota Tangerang")},
		"districts:31.71":   {It("31.71.01", "Gambir")},
		"districts:31.74":   {It("31.74.06", "Cilandak"), It("31.74.09", "Jagakarsa")},
		"districts:36.03":   {It("36.03.01", "Balaraja")},
		"districts:36.71":   {It("36.71.01", "Tangerang"), It("36.71.02", "Jatiuwung")},
		"villages:31.71.01": {It("31.71.01.1001", "Gambir"), It("31.71.01.1002", "Cideng")},
		"villages:31.74.06": {
			It("31.74.06.1001", "Cipete Selatan"), It("31.74.06.1002", "Gandaria Selatan"),
			It("31.74.06.1003", "Cilandak Barat"), It("31.74.06.1004", "Lebak Bulus"), It("31.74.06.1005", "Pondok Labu"),
		},
		"villages:31.74.09": {It("31.74.09.1002", "Srengseng Sawah"), It("31.74.09.1005", "Tanjung Barat")},
		"villages:36.03.01": {It("36.03.01.2001", "Saga"), It("36.03.01.1002", "Balaraja")},
		"villages:36.71.01": {It("36.71.01.1001", "Sukarasa"), It("36.71.01.1002", "Tangerang")},
		"villages:36.71.02": {It("36.71.02.1001", "Alam Jaya")},
	}
}

// Server menyajikan pohon wilayah di /provinces.json, /regencies/<kode>.json, dst.
// Mode khusus per path bisa dipasang lewat fail (path -> fungsi penulis respons).
type Server struct {
	Mu       sync.Mutex
	Tree     map[string][]Item
	Updated  string
	Fail     map[string]func(w http.ResponseWriter, n int) bool // true = sudah ditangani
	Hits     map[string]int
	UA       string
	Inflight int
	MaxIn    int
	Delay    func()
}

func NewServer() *Server {
	return &Server{Tree: Tree(), Updated: "2025-07-04", Fail: map[string]func(http.ResponseWriter, int) bool{}, Hits: map[string]int{}}
}

func PathToKey(p string) string {
	p = strings.TrimSuffix(p, ".json")
	if p == "/provinces" {
		return "provinces"
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[0] + ":" + parts[1]
}

func (m *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.Mu.Lock()
	m.Inflight++
	if m.Inflight > m.MaxIn {
		m.MaxIn = m.Inflight
	}
	m.UA = r.UserAgent()
	key := PathToKey(strings.TrimPrefix(r.URL.Path, "/wilayah/api"))
	m.Hits[key]++
	n := m.Hits[key]
	f := m.Fail[key]
	items, ok := m.Tree[key]
	upd := m.Updated
	delay := m.Delay
	m.Mu.Unlock()
	defer func() {
		m.Mu.Lock()
		m.Inflight--
		m.Mu.Unlock()
	}()
	if delay != nil {
		delay()
	}
	if f != nil && f(w, n) {
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	level := map[string]int{"provinces": 1, "regencies": 2, "districts": 3, "villages": 4}[strings.SplitN(key, ":", 2)[0]]
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items, "meta": map[string]any{"administrative_area_level": level, "updated_at": upd}})
}

func (m *Server) HitCount(key string) int {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.Hits[key]
}
