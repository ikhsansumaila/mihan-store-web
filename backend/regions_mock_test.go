package main

// Server tiruan wilayah.id (kecil: 2 provinsi) dipakai tes unit, integrasi, dan e2e.
// Tes otomatis TIDAK PERNAH memanggil wilayah.id asli.

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// mockRegionTree: dataset_key -> item. Bisa diubah per tes (salinan baru tiap panggilan).
func mockRegionTree() map[string][]RegionItem {
	return map[string][]RegionItem{
		"provinces": {{"36", "Banten"}, {"31", "DKI Jakarta"}},
		"regencies:31": {
			{"31.71", "Kota Administrasi Jakarta Pusat"},
			{"31.74", "Kota Administrasi Jakarta Selatan"},
		},
		"regencies:36":      {{"36.03", "Kabupaten Tangerang"}, {"36.71", "Kota Tangerang"}},
		"districts:31.71":   {{"31.71.01", "Gambir"}},
		"districts:31.74":   {{"31.74.06", "Cilandak"}, {"31.74.09", "Jagakarsa"}},
		"districts:36.03":   {{"36.03.01", "Balaraja"}},
		"districts:36.71":   {{"36.71.01", "Tangerang"}, {"36.71.02", "Jatiuwung"}},
		"villages:31.71.01": {{"31.71.01.1001", "Gambir"}, {"31.71.01.1002", "Cideng"}},
		"villages:31.74.06": {
			{"31.74.06.1001", "Cipete Selatan"}, {"31.74.06.1002", "Gandaria Selatan"},
			{"31.74.06.1003", "Cilandak Barat"}, {"31.74.06.1004", "Lebak Bulus"}, {"31.74.06.1005", "Pondok Labu"},
		},
		"villages:31.74.09": {{"31.74.09.1002", "Srengseng Sawah"}, {"31.74.09.1005", "Tanjung Barat"}},
		"villages:36.03.01": {{"36.03.01.2001", "Saga"}, {"36.03.01.1002", "Balaraja"}},
		"villages:36.71.01": {{"36.71.01.1001", "Sukarasa"}, {"36.71.01.1002", "Tangerang"}},
		"villages:36.71.02": {{"36.71.02.1001", "Alam Jaya"}},
	}
}

// mockRegionServer menyajikan pohon wilayah di /provinces.json, /regencies/<kode>.json, dst.
// Mode khusus per path bisa dipasang lewat fail (path -> fungsi penulis respons).
type mockRegionServer struct {
	mu       sync.Mutex
	tree     map[string][]RegionItem
	updated  string
	fail     map[string]func(w http.ResponseWriter, n int) bool // true = sudah ditangani
	hits     map[string]int
	ua       string
	inflight int
	maxIn    int
	delay    func()
}

func newMockRegionServer() *mockRegionServer {
	return &mockRegionServer{tree: mockRegionTree(), updated: "2025-07-04", fail: map[string]func(http.ResponseWriter, int) bool{}, hits: map[string]int{}}
}

func pathToRegionKey(p string) string {
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

func (m *mockRegionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.inflight++
	if m.inflight > m.maxIn {
		m.maxIn = m.inflight
	}
	m.ua = r.UserAgent()
	key := pathToRegionKey(strings.TrimPrefix(r.URL.Path, "/wilayah/api"))
	m.hits[key]++
	n := m.hits[key]
	f := m.fail[key]
	items, ok := m.tree[key]
	upd := m.updated
	delay := m.delay
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.inflight--
		m.mu.Unlock()
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

func (m *mockRegionServer) hitCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[key]
}
