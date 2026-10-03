//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// e2eUpload mengirim foto lewat API admin backend UJI (multipart, header Access + CSRF seperti browser).
func e2eUpload(t *testing.T, url string, hdr map[string]string, data []byte) e2eResp {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "IMG_kamera.jpg")
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", "mihanstore-e2e")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	r := e2eResp{Code: res.StatusCode, Raw: raw, Hdr: res.Header}
	r.Body = map[string]any{}
	_ = json.Unmarshal(raw, &r.Body)
	return r
}

// rawGet mengirim path APA ADANYA (tanpa normalisasi klien) untuk uji traversal.
func rawGet(t *testing.T, base, path string) int {
	t.Helper()
	host := strings.TrimPrefix(base, "http://")
	c, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", path, host)
	b, _ := io.ReadAll(c)
	var code int
	fmt.Sscanf(string(b), "HTTP/1.1 %d", &code)
	return code
}

func e2eSynthJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 160, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func TestE2EProductImages(t *testing.T) {
	if e2eFrontend == "" {
		t.Skip("E2E_FRONTEND_URL kosong")
	}
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	r := e2e(t, "POST", e2eBackend+"/api/admin/products", adminHdr(t, admin, true), map[string]any{
		"name": "E2E Produk Berfoto", "price": 15000, "categoryId": 1, "unit": "pcs"})
	if r.Code != 201 {
		t.Fatalf("buat produk: %d %s", r.Code, r.Raw)
	}
	pid := uint64(r.Body["id"].(float64))
	upURL := fmt.Sprintf("%s/api/admin/products/%d/image", e2eBackend, pid)

	// Tanpa Access / tanpa CSRF -> ditolak.
	if r := e2eUpload(t, upURL, nil, e2eSynthJPEG(50, 50)); r.Code != 401 {
		t.Fatalf("unggah tanpa Access: %d", r.Code)
	}
	if r := e2eUpload(t, upURL, adminHdr(t, admin, false), e2eSynthJPEG(50, 50)); r.Code != 403 {
		t.Fatalf("unggah tanpa CSRF: %d", r.Code)
	}

	// Unggah foto 2400x1800 -> 1200x900 + thumbnail 400x300.
	r = e2eUpload(t, upURL, adminHdr(t, admin, true), e2eSynthJPEG(2400, 1800))
	if r.Code != 200 {
		t.Fatalf("unggah: %d %s", r.Code, r.Raw)
	}
	img, thumb := r.Body["image"].(string), r.Body["thumb"].(string)
	if !strings.HasPrefix(img, fmt.Sprintf("/uploads/products/%d/", pid)) || thumb != strings.TrimSuffix(img, ".jpg")+"_t.jpg" {
		t.Fatalf("URL: %s %s", img, thumb)
	}

	// Diambil lewat nginx frontend UJI.
	for _, c := range []struct {
		path string
		w, h int
	}{{img, 1200, 900}, {thumb, 400, 300}} {
		res, err := http.Get(e2eFrontend + c.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" ||
			res.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %v", c.path, res.StatusCode, res.Header)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(body))
		if err != nil || cfg.Width != c.w || cfg.Height != c.h || res.ContentLength != int64(len(body)) {
			t.Fatalf("%s: ukuran %dx%d (%v), panjang %d", c.path, cfg.Width, cfg.Height, err, res.ContentLength)
		}
		t.Logf("%s -> 200 image/jpeg %d byte %dx%d, Cache-Control immutable, nosniff", c.path, len(body), cfg.Width, cfg.Height)
	}
	// API publik lewat proxy frontend memuat image/thumb.
	if r := e2e(t, "GET", e2eFrontend+"/api/products", nil, nil); !strings.Contains(string(r.Raw), `"thumb":"`+thumb+`"`) {
		t.Fatalf("API publik tanpa thumb: %s", r.Raw)
	}

	// Tidak ada / bukan pola sah / daftar / traversal -> bukan 200.
	missing := fmt.Sprintf("/uploads/products/%d/00000000-0000-4000-8000-000000000000.jpg", pid)
	bad := map[string]int{
		missing:     404,
		"/uploads/": 404,
		"/uploads":  404,
		fmt.Sprintf("/uploads/products/%d/", pid):      404,
		"/uploads/products/":                           404,
		strings.Replace(img, ".jpg", ".png", 1):        404,
		strings.Replace(img, ".jpg", ".JPG", 1):        404,
		img + ".php":                                   404,
		"/uploads/.env":                                404,
		"/uploads/products/1/evil.php":                 404,
		"/uploads/../etc/passwd":                       0,
		"/uploads/../../../../etc/passwd":              0,
		"/uploads/%2e%2e/%2e%2e/etc/passwd":            0,
		"/uploads/..%2f..%2fetc%2fpasswd":              0,
		"/uploads/%2E%2E%2Fetc%2Fpasswd":               0,
		"/uploads/products/..%5c..%5cetc/passwd":       0,
		"/uploads/products/../../etc/nginx/nginx.conf": 0,
		"/uploads/products/1/..;/..;/etc/passwd":       0,
		"/static/../uploads/../../etc/passwd":          0,
	}
	for p, want := range bad {
		code := rawGet(t, e2eFrontend, p)
		if code == 200 || (want != 0 && code != want) {
			t.Errorf("%s -> %d (mau bukan 200%s)", p, code, map[bool]string{true: fmt.Sprintf(", tepatnya %d", want), false: ""}[want != 0])
		} else {
			t.Logf("%s -> %d", p, code)
		}
	}
	// Metode selain GET/HEAD ditolak.
	if res, err := http.Post(e2eFrontend+img, "text/plain", strings.NewReader("x")); err != nil || res.StatusCode == 200 {
		t.Fatalf("POST ke berkas: %v %v", err, res)
	}

	// Ganti foto -> berkas lama 404 lewat nginx; hapus foto -> berkas baru 404.
	r = e2eUpload(t, upURL, adminHdr(t, admin, true), e2eSynthJPEG(300, 200))
	if r.Code != 200 {
		t.Fatalf("ganti: %d %s", r.Code, r.Raw)
	}
	img2 := r.Body["image"].(string)
	if rawGet(t, e2eFrontend, img) != 404 || rawGet(t, e2eFrontend, thumb) != 404 || rawGet(t, e2eFrontend, img2) != 200 {
		t.Fatal("berkas lama harus hilang, berkas baru ada")
	}
	if r := e2e(t, "DELETE", upURL, adminHdr(t, admin, true), map[string]any{}); r.Code != 200 {
		t.Fatalf("hapus foto: %d", r.Code)
	}
	if rawGet(t, e2eFrontend, img2) != 404 {
		t.Fatal("berkas setelah hapus harus 404")
	}
	// Bersihkan produk uji.
	e2e(t, "DELETE", fmt.Sprintf("%s/api/admin/products/%d", e2eBackend, pid), adminHdr(t, admin, true), map[string]any{})
}
