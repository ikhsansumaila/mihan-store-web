//go:build e2e

// E2E data wilayah terhadap container backend UJI. Sumber data = server tiruan wilayah.id di proses tes
// ini (http://e2e-runner:9000/wilayah/api, 2 provinsi), BUKAN wilayah.id asli.
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var e2eWilayah = newMockRegionServer()

// Hasil pemeriksaan SEBELUM data wilayah pertama kali disimpan (dicatat oleh ensureE2ERegions).
var (
	e2eRegionsOnce    sync.Once
	e2ePrePublic      e2eResp
	e2ePreCheckout    e2eResp
	e2eRegionsSeedErr string
)

// e2eRegionCodes: wilayah Kota Tangerang di pohon tiruan.
func e2eRegionCodes() map[string]any {
	return map[string]any{"provinceCode": "36", "regencyCode": "36.71", "districtCode": "36.71.01", "villageCode": "36.71.01.1001"}
}

// e2eGoogleCustomer membuat pelanggan lewat login Google tiruan (tidak memakai kuota registrasi password
// 5/jam per IP yang juga dipakai tes lain).
func e2eGoogleCustomer(t *testing.T, sub, username string) string {
	t.Helper()
	r := e2e(t, "POST", e2eBackend+"/api/auth/google", nil, map[string]any{"credential": googleJWT(t, sub, username+"@uji.test", "Pembeli Wilayah")})
	if r.Code != 200 || r.Body["needsProfile"] != true {
		t.Fatalf("google %s: %d %s", username, r.Code, r.Raw)
	}
	r = e2e(t, "POST", e2eBackend+"/api/auth/google/complete", nil, map[string]any{"profileToken": r.Body["profileToken"], "username": username})
	if r.Code != 201 || r.Body["token"] == nil {
		t.Fatalf("google complete %s: %d %s", username, r.Code, r.Raw)
	}
	return r.Body["token"].(string)
}

// e2eAdminFetch memulai fetch lewat API admin dan menunggu sampai tidak 'running' lagi.
func e2eAdminFetch(t *testing.T) (uint64, map[string]any) {
	t.Helper()
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	r := e2e(t, "POST", e2eBackend+"/api/admin/regions/fetch", adminHdr(t, admin, true), map[string]any{})
	if r.Code != 202 {
		t.Fatalf("admin fetch: %d %s", r.Code, r.Raw)
	}
	id := uint64(r.Body["run"].(map[string]any)["id"].(float64))
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		rr := e2e(t, "GET", fmt.Sprintf("%s/api/admin/regions/runs/%d", e2eBackend, id), adminHdr(t, admin, false), nil)
		run := rr.Body["run"].(map[string]any)
		if run["status"] != "running" {
			return id, run
		}
		if p, ok := run["progress"].(map[string]any); !ok || p["total"] == nil {
			t.Fatalf("kemajuan tidak ada: %s", rr.Raw)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("run %d tidak selesai", id)
	return 0, nil
}

func ensureE2ERegions(t *testing.T) {
	t.Helper()
	e2eRegionsOnce.Do(func() {
		// 1) Sebelum ada data: API publik & checkout -> 503.
		e2ePrePublic = e2e(t, "GET", e2eBackend+"/api/regions/provinces", nil, nil)
		tok := e2eGoogleCustomer(t, "e2e-wilayah-pre", "e2e_wilayah_pre")
		e2e(t, "POST", e2eBackend+"/api/cart/items", bearer(tok), map[string]any{"productId": 1, "qty": 1})
		key, _ := newPublicID()
		body := map[string]any{"recipientName": "Pra", "recipientPhone": "081377778888", "address": "Jl. Pra No. 1",
			"postalCode": "15111", "idempotencyKey": key}
		for k, v := range e2eRegionCodes() {
			body[k] = v
		}
		e2ePreCheckout = e2e(t, "POST", e2eBackend+"/api/orders", bearer(tok), body)
		// 2) Admin fetch -> staged -> simpan.
		id, run := e2eAdminFetch(t)
		if run["status"] != "staged" {
			e2eRegionsSeedErr = fmt.Sprintf("run %d: %v", id, run)
			return
		}
		r := e2e(t, "POST", fmt.Sprintf("%s/api/admin/regions/runs/%d/save", e2eBackend, id), adminHdr(t, os.Getenv("E2E_ADMIN_EMAIL"), true), map[string]any{})
		if r.Code != 200 {
			e2eRegionsSeedErr = fmt.Sprintf("simpan: %d %s", r.Code, r.Raw)
		}
	})
	if e2eRegionsSeedErr != "" {
		t.Fatalf("seed wilayah e2e: %s", e2eRegionsSeedErr)
	}
}

func TestE2ERegions(t *testing.T) {
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	B := e2eBackend
	discordMode.Store("ok")
	ensureE2ERegions(t)

	// Sebelum data ada: 503 dengan pesan jelas.
	if e2ePrePublic.Code != 503 || !strings.Contains(string(e2ePrePublic.Raw), "Data wilayah belum tersedia") {
		t.Errorf("API publik sebelum impor: %d %s", e2ePrePublic.Code, e2ePrePublic.Raw)
	}
	if e2ePreCheckout.Code != 503 || !strings.Contains(string(e2ePreCheckout.Raw), "Data wilayah belum tersedia") {
		t.Errorf("checkout sebelum impor: %d %s", e2ePreCheckout.Code, e2ePreCheckout.Raw)
	}
	if ua := e2eWilayah.ua; ua != regionUserAgent {
		t.Errorf("User-Agent fetcher: %q", ua)
	}

	// Status admin setelah simpan.
	st := e2e(t, "GET", B+"/api/admin/regions/status", adminHdr(t, admin, false), nil)
	stored := st.Body["stored"].(map[string]any)
	items := stored["items"].(map[string]any)
	if st.Code != 200 || stored["hasData"] != true || items["provinces"].(float64) != 2 || items["villages"].(float64) != 14 ||
		stored["sourceUpdatedAt"] != "2025-07-04" || stored["lastSaved"].(map[string]any)["by"] != admin {
		t.Fatalf("status: %d %s", st.Code, st.Raw)
	}

	// API publik: cache & ETag, rantai 31 -> 31.74 -> 31.74.06 -> desa.
	p := e2e(t, "GET", B+"/api/regions/provinces", nil, nil)
	if p.Code != 200 || p.Hdr.Get("Cache-Control") != "public, max-age=3600" || p.Hdr.Get("ETag") == "" ||
		!strings.Contains(string(p.Raw), `"meta":{"updatedAt":"2025-07-04"}`) {
		t.Fatalf("provinsi: %d %v %s", p.Code, p.Hdr, p.Raw)
	}
	if r := e2e(t, "GET", B+"/api/regions/provinces", map[string]string{"If-None-Match": p.Hdr.Get("ETag")}, nil); r.Code != 304 {
		t.Fatalf("ETag 304: %d", r.Code)
	}
	for path, want := range map[string]string{
		"/api/regions/regencies/31":      "Kota Administrasi Jakarta Selatan",
		"/api/regions/districts/31.74":   "Cilandak",
		"/api/regions/villages/31.74.06": "Lebak Bulus",
		"/api/regions/regencies/36":      "Kota Tangerang",
		"/api/regions/villages/36.71.01": "Sukarasa",
	} {
		if r := e2e(t, "GET", B+path, nil, nil); r.Code != 200 || !strings.Contains(string(r.Raw), want) {
			t.Errorf("%s: %d %s", path, r.Code, r.Raw)
		}
	}
	if r := e2e(t, "GET", B+"/api/regions/regencies/99", nil, nil); r.Code != 404 {
		t.Errorf("induk tidak ada: %d", r.Code)
	}
	if r := e2e(t, "GET", B+"/api/regions/villages/abc", nil, nil); r.Code != 400 {
		t.Errorf("kode salah: %d", r.Code)
	}

	// Pelanggan: checkout wilayah bertingkat sukses (nama dari DB); kode tidak cocok induk -> 422.
	tok := e2eGoogleCustomer(t, "e2e-wilayah-beli", "e2e_wilayah_beli")
	e2e(t, "POST", B+"/api/cart/items", bearer(tok), map[string]any{"productId": 2, "qty": 1})
	key, _ := newPublicID()
	body := map[string]any{"recipientName": "Siti E2E", "recipientPhone": "081377778888", "address": "Jl. Mawar No. 5, RT 01/RW 02",
		"postalCode": "12440", "idempotencyKey": key, "provinceCode": "31", "regencyCode": "36.71", "districtCode": "31.74.06",
		"villageCode": "31.74.06.1004", "villageName": "Desa Palsu", "city": "Kota Palsu"}
	if r := e2e(t, "POST", B+"/api/orders", bearer(tok), body); r.Code != 422 || r.Body["field"] != "regencyCode" {
		t.Fatalf("kode tidak cocok induk: %d %s", r.Code, r.Raw)
	}
	body["regencyCode"] = "31.74"
	r := e2e(t, "POST", B+"/api/orders", bearer(tok), body)
	if r.Code != 201 {
		t.Fatalf("checkout wilayah: %d %s", r.Code, r.Raw)
	}
	rcp := r.Body["recipient"].(map[string]any)
	if rcp["fullAddress"] != "Jl. Mawar No. 5, RT 01/RW 02, Lebak Bulus, Kec. Cilandak, Kota Administrasi Jakarta Selatan, DKI Jakarta 12440" ||
		rcp["city"] != "Kota Administrasi Jakarta Selatan" {
		t.Fatalf("alamat tersusun: %v", rcp)
	}
	orderNo := r.Body["orderNo"].(string)
	hits := waitHits(orderNo, 1, 10*time.Second)
	if len(hits) != 1 {
		t.Fatalf("notifikasi: %v", hits)
	}
	for _, bad := range []string{"Mawar", "Lebak Bulus", "Cilandak", "12440", "DKI Jakarta"} {
		if strings.Contains(hits[0], bad) {
			t.Errorf("notifikasi Discord tidak boleh memuat alamat (%s): %s", bad, hits[0])
		}
	}

	// Pelanggan biasa / tanpa token Access ke admin wilayah -> ditolak.
	if r := e2e(t, "GET", B+"/api/admin/regions/status", bearer(tok), nil); r.Code != 401 && r.Code != 403 {
		t.Errorf("pelanggan -> admin wilayah: %d", r.Code)
	}
	if r := e2e(t, "POST", B+"/api/admin/regions/fetch", bearer(tok), map[string]any{}); r.Code == 202 || r.Code == 200 {
		t.Errorf("pelanggan memulai fetch: %d", r.Code)
	}
	if r := e2e(t, "POST", B+"/api/admin/regions/fetch", adminHdr(t, admin, false), map[string]any{}); r.Code != 403 {
		t.Errorf("fetch tanpa header CSRF harus 403: %d", r.Code)
	}

	// Admin: fetch dengan data sumber berubah -> Batal -> data lama tidak berubah.
	e2eWilayah.mu.Lock()
	e2eWilayah.tree["villages:31.74.06"][3].Name = "Lebak Bulus Berubah"
	e2eWilayah.mu.Unlock()
	id, run := e2eAdminFetch(t)
	diff, _ := run["diff"].(map[string]any)
	if run["status"] != "staged" || diff == nil || diff["changed"].(float64) != 1 {
		t.Fatalf("ringkasan run: %v", run)
	}
	if r := e2e(t, "POST", fmt.Sprintf("%s/api/admin/regions/runs/%d/discard", B, id), adminHdr(t, admin, true), map[string]any{}); r.Code != 200 || r.Body["status"] != "discarded" {
		t.Fatalf("buang: %d %s", r.Code, r.Raw)
	}
	if r := e2e(t, "GET", B+"/api/regions/villages/31.74.06", nil, nil); strings.Contains(string(r.Raw), "Berubah") || !strings.Contains(string(r.Raw), "Lebak Bulus") {
		t.Fatalf("data lama harus tetap setelah batal: %s", r.Raw)
	}
	// Fetch lagi -> Simpan -> data baru tampil (cache dibersihkan).
	id, run = e2eAdminFetch(t)
	if run["status"] != "staged" {
		t.Fatalf("run: %v", run)
	}
	if r := e2e(t, "POST", fmt.Sprintf("%s/api/admin/regions/runs/%d/save", B, id), adminHdr(t, admin, true), map[string]any{}); r.Code != 200 {
		t.Fatalf("simpan: %d %s", r.Code, r.Raw)
	}
	if r := e2e(t, "GET", B+"/api/regions/villages/31.74.06", nil, nil); !strings.Contains(string(r.Raw), "Lebak Bulus Berubah") {
		t.Fatalf("data baru harus tampil setelah simpan: %s", r.Raw)
	}
	// Pesanan lama tetap memakai nama saat checkout (snapshot), tidak ikut berubah.
	if r := e2e(t, "GET", B+"/api/orders/"+orderNo, bearer(tok), nil); !strings.Contains(string(r.Raw), `"name":"Lebak Bulus"`) {
		t.Fatalf("snapshot nama wilayah pesanan: %s", r.Raw)
	}
	// Kembalikan pohon semula (agar CLI e2e dan tes lain konsisten).
	e2eWilayah.mu.Lock()
	e2eWilayah.tree = mockRegionTree()
	e2eWilayah.mu.Unlock()
	id, _ = e2eAdminFetch(t)
	e2e(t, "POST", fmt.Sprintf("%s/api/admin/regions/runs/%d/save", B, id), adminHdr(t, admin, true), map[string]any{})
	if st := e2e(t, "GET", B+"/api/admin/regions/status", adminHdr(t, admin, false), nil); len(st.Body["runs"].([]any)) < 4 || st.Body["activeRun"] != nil {
		t.Fatalf("riwayat run: %s", st.Raw)
	}
}

// TestE2EServeMockOnly hanya menahan proses agar server tiruan wilayah tetap hidup saat skrip e2e
// menjalankan `main import-regions` (CLI) di container backend uji. Dilewati pada putaran e2e biasa.
func TestE2EServeMockOnly(t *testing.T) {
	if os.Getenv("E2E_SERVE_MOCK_ONLY") != "1" {
		t.Skip("hanya untuk uji CLI import-regions")
	}
	time.Sleep(90 * time.Second)
}
