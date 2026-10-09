package regions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mihanstore/internal/regions/regionstest"
)

func TestRegionCodes(t *testing.T) {
	valid := map[string]int{"11": 1, "31.74": 2, "31.74.06": 3, "31.74.06.1001": 4, "11.01.01.2001": 4}
	for c, l := range valid {
		if !validRegionCode(l, c) || regionLevelOf(c) != l {
			t.Errorf("%s harus valid tingkat %d", c, l)
		}
	}
	for _, c := range []string{"", "1", "111", "31.7", "31.74.6", "31.74.06.100", "31.74.06.10011", "31-74", "31.74.06.1001'", "ab", "31.74.06.1001\n", " 31"} {
		if regionLevelOf(c) != 0 {
			t.Errorf("%q harus tidak valid", c)
		}
	}
	if validRegionCode(2, "31") || validRegionCode(5, "31") || validRegionCode(0, "") {
		t.Error("tingkat salah harus ditolak")
	}
	norm := map[string]string{" 31 ": "31", "3174": "31.74", "317406": "31.74.06", "3174061001": "31.74.06.1001",
		"31.74": "31.74", "31a4": "31a4", "12345": "12345"}
	for in, want := range norm {
		if got := normalizeRegionCode(in); got != want {
			t.Errorf("normalize(%q) = %q, mau %q", in, got, want)
		}
	}
	if regionParentOf("31.74.06.1001") != "31.74.06" || regionParentOf("31") != "" {
		t.Error("regionParentOf")
	}
	for _, c := range []struct {
		l      int
		parent string
		key    string
	}{{1, "", "provinces"}, {2, "31", "regencies:31"}, {3, "31.74", "districts:31.74"}, {4, "31.74.06", "villages:31.74.06"}} {
		if got := regionDatasetKey(c.l, c.parent); got != c.key {
			t.Errorf("key %d %s = %s", c.l, c.parent, got)
		}
		l, p, ok := ParseDatasetKey(c.key)
		if !ok || l != c.l || p != c.parent {
			t.Errorf("parse %s = %d %s %v", c.key, l, p, ok)
		}
	}
	for _, k := range []string{"", "regencies:", "regencies:31.74", "villages:31", "kota:31", "districts:31.74.06"} {
		if _, _, ok := ParseDatasetKey(k); ok {
			t.Errorf("kunci %q harus tidak valid", k)
		}
	}
}

func TestComposeAddress(t *testing.T) {
	pc := "12430"
	reg := &DTO{Province: Ref{"31", "DKI Jakarta"}, Regency: Ref{"31.74", "Kota Administrasi Jakarta Selatan"},
		District: Ref{"31.74.06", "Cilandak"}, Village: Ref{"31.74.06.1004", "Lebak Bulus"}}
	got := ComposeAddress("Jl. Mawar No. 5\nRT 01/RW 02 ", reg, "Kota Administrasi Jakarta Selatan", &pc)
	want := "Jl. Mawar No. 5, RT 01/RW 02, Lebak Bulus, Kec. Cilandak, Kota Administrasi Jakarta Selatan, DKI Jakarta 12430"
	if got != want {
		t.Fatalf("alamat baru:\n%s\n%s", got, want)
	}
	// Pesanan lama: alamat + kota (+ kode pos bila ada).
	if got := ComposeAddress("Jl. Lama No. 1", nil, "Tangerang", nil); got != "Jl. Lama No. 1, Tangerang" {
		t.Errorf("lama tanpa kode pos: %s", got)
	}
	if got := ComposeAddress("Jl. Lama No. 1\r\n\r\nBlok A,", nil, "Tangerang", &pc); got != "Jl. Lama No. 1, Blok A, Tangerang 12430" {
		t.Errorf("lama dengan kode pos: %s", got)
	}
}

func body(items string, upd string) []byte {
	return []byte(`{"data":` + items + `,"meta":{"administrative_area_level":2,"updated_at":"` + upd + `"}}`)
}

func TestParseRegionResponse(t *testing.T) {
	items, upd, err := parseRegionResponse(body(`[{"code":"31.74","name":" Kota Administrasi Jakarta Selatan "},{"code":"31.71","name":"Jakpus"}]`, "2025-07-04"), 2, "31")
	if err != nil || len(items) != 2 || items[0].Name != "Kota Administrasi Jakarta Selatan" || upd != "2025-07-04" {
		t.Fatalf("respons valid: %v %v %q", items, err, upd)
	}
	if _, upd, _ := parseRegionResponse(body(`[]`, "kemarin"), 2, "31"); upd != "" {
		t.Error("tanggal tidak valid harus diabaikan")
	}
	bad := map[string]string{
		"json rusak":            `{"data":[{"code":"31.74"`,
		"data bukan array":      `{"data":{"code":"31.74"}}`,
		"tanpa data":            `{"meta":{}}`,
		"code angka":            string(body(`[{"code":3174,"name":"X"}]`, "")),
		"code kosong":           string(body(`[{"code":" ","name":"X"}]`, "")),
		"name kosong":           string(body(`[{"code":"31.74","name":""}]`, "")),
		"tanpa name":            string(body(`[{"code":"31.74"}]`, "")),
		"format salah":          string(body(`[{"code":"31.7","name":"X"}]`, "")),
		"tidak berawalan induk": string(body(`[{"code":"32.74","name":"X"}]`, "")),
		"duplikat":              string(body(`[{"code":"31.74","name":"X"},{"code":"31.74","name":"Y"}]`, "")),
		"nama kontrol":          string(body(`[{"code":"31.74","name":"X\u0007"}]`, "")),
		"nama panjang":          string(body(`[{"code":"31.74","name":"`+strings.Repeat("a", 101)+`"}]`, "")),
	}
	for name, b := range bad {
		if _, _, err := parseRegionResponse([]byte(b), 2, "31"); err == nil {
			t.Errorf("%s harus ditolak", name)
		}
	}
}

// testFetcher: fetcher cepat untuk tes; sleep dicatat (tanpa menunggu sungguhan).
func testFetcher(base string) (*regionFetcher, *[]time.Duration) {
	cfg := DefaultFetchConfig(base)
	cfg.Interval = 0
	cfg.MinProvinces = 1
	cfg.Timeout = 2 * time.Second
	f := newRegionFetcher(cfg)
	var mu sync.Mutex
	slept := []time.Duration{}
	f.sleep = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			mu.Lock()
			slept = append(slept, d)
			mu.Unlock()
		}
		return ctx.Err()
	}
	f.jitter = func() float64 { return 1 }
	return f, &slept
}

func TestRegionFetcherRetry(t *testing.T) {
	m := regionstest.NewServer()
	ts := httptest.NewServer(m)
	defer ts.Close()

	// 429 dengan Retry-After: 7 -> menunggu 7 detik, lalu sukses.
	m.Fail["regencies:31"] = func(w http.ResponseWriter, n int) bool {
		if n == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			return true
		}
		return false
	}
	f, slept := testFetcher(ts.URL)
	items, _, err := f.fetchDataset(context.Background(), 2, "31")
	if err != nil || len(items) != 2 || m.HitCount("regencies:31") != 2 {
		t.Fatalf("429 lalu sukses: %v %d", err, m.HitCount("regencies:31"))
	}
	if len(*slept) != 1 || (*slept)[0] != 7*time.Second || f.retries.Load() != 1 {
		t.Fatalf("Retry-After tidak dihormati: %v", *slept)
	}
	if m.UA != UserAgent {
		t.Errorf("User-Agent: %q", m.UA)
	}

	// 500 dua kali lalu sukses -> backoff eksponensial (jitter=1: 1s, 2s).
	m.Fail["regencies:36"] = func(w http.ResponseWriter, n int) bool {
		if n <= 2 {
			w.WriteHeader(503)
			return true
		}
		return false
	}
	f, slept = testFetcher(ts.URL)
	if _, _, err := f.fetchDataset(context.Background(), 2, "36"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(*slept) != "[1s 2s]" {
		t.Fatalf("backoff: %v", *slept)
	}

	// 500 terus -> berhenti setelah 3 retry (4 percobaan).
	m.Fail["districts:31.74"] = func(w http.ResponseWriter, n int) bool { w.WriteHeader(500); return true }
	f, _ = testFetcher(ts.URL)
	_, _, err = f.fetchDataset(context.Background(), 3, "31.74")
	if err == nil || m.HitCount("districts:31.74") != 4 || f.retries.Load() != 3 {
		t.Fatalf("retry maks 3: %v hits=%d", err, m.HitCount("districts:31.74"))
	}

	// JSON rusak / 404 -> tidak di-retry.
	m.Fail["districts:31.71"] = func(w http.ResponseWriter, n int) bool { w.Write([]byte(`{"data":[`)); return true }
	f, _ = testFetcher(ts.URL)
	if _, _, err := f.fetchDataset(context.Background(), 3, "31.71"); err == nil || m.HitCount("districts:31.71") != 1 {
		t.Fatalf("json rusak: %v %d", err, m.HitCount("districts:31.71"))
	}
	if _, _, err := f.fetchDataset(context.Background(), 3, "99.99"); err == nil || m.HitCount("districts:99.99") != 1 {
		t.Fatalf("404 tidak boleh di-retry: %v", err)
	}

	// Respons terlalu besar.
	m.Fail["districts:36.03"] = func(w http.ResponseWriter, n int) bool {
		w.Write([]byte(`{"data":[{"code":"36.03.01","name":"` + strings.Repeat("x", 2000) + `"}]}`))
		return true
	}
	f, _ = testFetcher(ts.URL)
	f.cfg.MaxBytes = 1000
	if _, _, err := f.fetchDataset(context.Background(), 3, "36.03"); err == nil || !strings.Contains(err.Error(), "terlalu besar") {
		t.Fatalf("ukuran berlebih: %v", err)
	}

	// Kode anak tidak berawalan induk dan kode ganda -> ditolak.
	m.Tree["districts:36.71"] = []regionstest.Item{{"36.72.01", "Salah Induk"}}
	f, _ = testFetcher(ts.URL)
	if _, _, err := f.fetchDataset(context.Background(), 3, "36.71"); err == nil || !strings.Contains(err.Error(), "berawalan") {
		t.Fatalf("kode anak salah induk: %v", err)
	}
	m.Tree["districts:36.71"] = []regionstest.Item{{"36.71.01", "A"}, {"36.71.01", "B"}}
	if _, _, err := f.fetchDataset(context.Background(), 3, "36.71"); err == nil || !strings.Contains(err.Error(), "ganda") {
		t.Fatalf("kode ganda: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	if parseRetryAfter("12", now) != 12*time.Second || parseRetryAfter("", now) != 0 || parseRetryAfter("-1", now) != 0 || parseRetryAfter("x", now) != 0 {
		t.Error("detik")
	}
	if d := parseRetryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now); d != 30*time.Second {
		t.Errorf("tanggal: %v", d)
	}
}

func runMock(t *testing.T, m *regionstest.Server, mut func(*regionFetcher)) (map[string][]Item, regionFetchSummary, error) {
	t.Helper()
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)
	f, _ := testFetcher(ts.URL)
	if mut != nil {
		mut(f)
	}
	got := map[string][]Item{}
	var last regionFetchProgress
	sum, err := f.Run(context.Background(), func(r regionFetchResult) error {
		got[r.Key] = r.Items
		return nil
	}, func(p regionFetchProgress) error { last = p; return nil })
	if err == nil && (!last.Final || last.Done != last.Total) {
		t.Errorf("kemajuan akhir: %+v", last)
	}
	return got, sum, err
}

func TestRegionFetcherRun(t *testing.T) {
	m := regionstest.NewServer()
	m.Delay = func() { time.Sleep(5 * time.Millisecond) }
	got, sum, err := runMock(t, m, func(f *regionFetcher) { f.cfg.Concurrency = 3 })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(regionstest.Tree()) || sum.Items != [5]int64{0, 2, 4, 6, 14} || sum.SourceUpdatedAt != "2025-07-04" ||
		sum.Requests != 13 || len(sum.FailedKeys) != 0 {
		t.Fatalf("hasil run: %d dataset, %+v", len(got), sum)
	}
	if m.MaxIn > 3 {
		t.Errorf("konkurensi melebihi batas: %d", m.MaxIn)
	}

	// Konkurensi dibatasi maks. 6 walau dikonfigurasi lebih.
	if f := newRegionFetcher(FetchConfig{Concurrency: 50}); f.cfg.Concurrency != 6 {
		t.Error("konkurensi maks 6")
	}

	// Provinsi terlalu sedikit -> gagal.
	if _, _, err := runMock(t, regionstest.NewServer(), func(f *regionFetcher) { f.cfg.MinProvinces = 30 }); err == nil ||
		!strings.Contains(err.Error(), "tidak masuk akal") {
		t.Fatalf("provinsi < 30: %v", err)
	}

	// Satu dataset gagal (<= batas) -> run tetap selesai, kunci gagal dicatat, turunannya tidak diambil.
	m = regionstest.NewServer()
	m.Fail["districts:31.74"] = func(w http.ResponseWriter, n int) bool { w.WriteHeader(500); return true }
	got, sum, err = runMock(t, m, nil)
	if err != nil || fmt.Sprint(sum.FailedKeys) != "[districts:31.74]" || got["villages:31.74.06"] != nil || sum.Failed != 1 {
		t.Fatalf("gagal sebagian: %v %+v", err, sum)
	}

	// Kegagalan melebihi MaxFailures -> run gagal.
	m = regionstest.NewServer()
	for k := range m.Tree {
		if strings.HasPrefix(k, "villages:") {
			m.Fail[k] = func(w http.ResponseWriter, n int) bool { w.WriteHeader(404); return true }
		}
	}
	_, _, err = runMock(t, m, func(f *regionFetcher) { f.cfg.MaxFailures = 2 })
	if !errors.Is(err, errRegionTooManyFailures) {
		t.Fatalf("terlalu banyak gagal: %v", err)
	}

	// Galat dari sink menghentikan run.
	ts := httptest.NewServer(regionstest.NewServer())
	defer ts.Close()
	f, _ := testFetcher(ts.URL)
	stop := errors.New("berhenti")
	if _, err := f.Run(context.Background(), func(r regionFetchResult) error {
		if r.Level == 3 {
			return stop
		}
		return nil
	}, func(regionFetchProgress) error { return nil }); !errors.Is(err, stop) {
		t.Fatalf("sink error: %v", err)
	}
	// Pembatalan konteks.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Run(ctx, func(regionFetchResult) error { return nil }, func(regionFetchProgress) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("batal: %v", err)
	}
}

func TestRegionFetcherPacing(t *testing.T) {
	ts := httptest.NewServer(regionstest.NewServer())
	defer ts.Close()
	f, slept := testFetcher(ts.URL)
	f.cfg.Interval = 50 * time.Millisecond
	for i := 0; i < 3; i++ {
		if _, _, err := f.fetchDataset(context.Background(), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Permintaan ke-2 dan ke-3 harus menunggu giliran (jarak minimal antar permintaan).
	if len(*slept) < 2 {
		t.Fatalf("batas laju tidak diterapkan: %v", *slept)
	}
}

func TestComputeRegionDiff(t *testing.T) {
	k := func(key string, level int, hash string, n int64) regionKeyInfo {
		return regionKeyInfo{Key: key, Level: level, Hash: hash, Items: n}
	}
	stored := []regionKeyInfo{
		k("provinces", 1, "p", 2), k("regencies:31", 2, "r31", 2), k("regencies:36", 2, "r36", 2),
		k("districts:31.74", 3, "d", 10), k("villages:31.74.06", 4, "v1", 100), k("villages:31.74.09", 4, "v2", 100),
		k("villages:36.71.01", 4, "v3", 50),
	}
	staged := []regionKeyInfo{
		k("provinces", 1, "p", 2), k("regencies:31", 2, "r31-baru", 3), k("regencies:36", 2, "r36", 2),
		k("districts:31.74", 3, "d", 10), k("villages:31.74.06", 4, "v1", 100), k("villages:31.74.10", 4, "vb", 0),
	}
	d := computeRegionDiff(stored, staged, nil)
	if d.New != 1 || d.Changed != 1 || d.Unchanged != 4 || d.Missing != 2 || d.Protected != 0 || d.EmptyDatasets != 1 {
		t.Fatalf("diff: %+v", d)
	}
	if d.ItemsStored["villages"] != 250 || d.ItemsNew["villages"] != 100 || d.ItemsNew["regencies"] != 5 {
		t.Fatalf("jumlah item: %+v %+v", d.ItemsStored, d.ItemsNew)
	}
	w := strings.Join(d.Warnings, " | ")
	if !strings.Contains(w, "kelurahan/desa turun 60.0%") || strings.Contains(w, "kabupaten/kota turun") || !strings.Contains(w, "1 dataset kosong") {
		t.Fatalf("peringatan: %s", w)
	}
	// Turun tepat 10% -> tidak ada peringatan; tingkat kosong -> peringatan.
	d = computeRegionDiff([]regionKeyInfo{k("provinces", 1, "a", 10)}, []regionKeyInfo{k("provinces", 1, "b", 9)}, nil)
	if strings.Contains(strings.Join(d.Warnings, "|"), "provinsi turun") || !strings.Contains(strings.Join(d.Warnings, "|"), "Tingkat kecamatan kosong") {
		t.Fatalf("batas 10%%: %v", d.Warnings)
	}
	// Permintaan gagal: dataset lama di bawahnya dipertahankan (bukan hilang) dan item lama ikut dihitung.
	d = computeRegionDiff(stored, staged[:4], []string{"districts:31.74"})
	if d.Missing != 1 || d.Protected != 2 || d.ItemsNew["villages"] != 200 {
		t.Fatalf("dilindungi: %+v", d)
	}
	if !strings.Contains(strings.Join(d.Warnings, "|"), "1 permintaan gagal") {
		t.Fatalf("peringatan gagal: %v", d.Warnings)
	}
}

func TestRegionKeyProtected(t *testing.T) {
	f := []string{"regencies:36", "villages:31.74.06"}
	for key, want := range map[string]bool{
		"regencies:36": true, "districts:36.71": true, "villages:36.71.01": true, "villages:31.74.06": true,
		"villages:31.74.09": false, "regencies:31": false, "provinces": false, "districts:31.74": false, "rusak": false,
	} {
		if regionKeyProtected(key, f) != want {
			t.Errorf("%s: mau %v", key, want)
		}
	}
}

func TestRegionCacheInvalidate(t *testing.T) {
	c := newRegionCache()
	_, gen := c.get("x")
	c.invalidate()
	c.put(&regionDataset{Key: "x", loadedAt: time.Now()}, gen) // dibaca sebelum invalidasi -> dibuang
	if d, _ := c.get("x"); d != nil {
		t.Fatal("data basi tersimpan setelah invalidasi")
	}
	_, gen = c.get("x")
	c.put(&regionDataset{Key: "x", loadedAt: time.Now()}, gen)
	if d, _ := c.get("x"); d == nil {
		t.Fatal("cache tidak menyimpan")
	}
	c.now = func() time.Time { return time.Now().Add(regionCacheTTL + time.Second) }
	if d, _ := c.get("x"); d != nil {
		t.Fatal("TTL tidak berlaku")
	}
}

func TestEtagAndRegionBase(t *testing.T) {
	if !etagMatch(`W/"abc", "def"`, `"abc"`) || !etagMatch(`*`, `"x"`) || etagMatch(`"abd"`, `"abc"`) {
		t.Error("etagMatch")
	}
}

func TestRegionRunRegistry(t *testing.T) {
	g := newRegionRunRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	g.add(7, cancel)
	go func() {
		<-ctx.Done()
		g.finish(7)
	}()
	start := time.Now()
	g.stop(7, 2*time.Second)
	if ctx.Err() == nil || time.Since(start) > time.Second {
		t.Fatal("stop harus membatalkan dan menunggu goroutine")
	}
	g.stop(99, time.Second) // tidak ada -> langsung kembali
}
