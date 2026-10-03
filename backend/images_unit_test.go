package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---------- pembuat gambar sintetis ----------

// markerImage: latar biru dengan blok merah di kiri-atas (seperempat lebar/tinggi) sebagai penanda orientasi.
func markerImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{20, 40, 220, 255}
			if x < w/4 && y < h/4 {
				c = color.RGBA{230, 20, 20, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func encJPEG(t testing.TB, img image.Image, q int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encPNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// tiffOrient membuat blok TIFF (big-endian) berisi tag Orientation + teks rahasia tiruan (metadata).
func tiffOrient(o int) []byte {
	var b bytes.Buffer
	b.WriteString("MM\x00\x2a\x00\x00\x00\x08")
	binary.Write(&b, binary.BigEndian, uint16(1))
	binary.Write(&b, binary.BigEndian, uint16(0x0112))
	binary.Write(&b, binary.BigEndian, uint16(3))
	binary.Write(&b, binary.BigEndian, uint32(1))
	binary.Write(&b, binary.BigEndian, uint16(o))
	binary.Write(&b, binary.BigEndian, uint16(0))
	binary.Write(&b, binary.BigEndian, uint32(0))
	b.WriteString("RAHASIA-GPS-UJI -6.2,106.8")
	return b.Bytes()
}

// withJPEGExif menyisipkan APP1 Exif (orientasi o) tepat setelah SOI.
func withJPEGExif(jpg []byte, o int) []byte {
	payload := append([]byte("Exif\x00\x00"), tiffOrient(o)...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func pngChunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	return b.Bytes()
}

// withPNGExif menyisipkan chunk eXIf setelah IHDR.
func withPNGExif(p []byte, o int) []byte {
	ihdrEnd := 8 + 8 + 13 + 4
	out := append([]byte{}, p[:ihdrEnd]...)
	out = append(out, pngChunk("eXIf", tiffOrient(o))...)
	return append(out, p[ihdrEnd:]...)
}

// patchPNGSize mengganti lebar/tinggi di IHDR (CRC diperbarui) tanpa mengubah data piksel.
func patchPNGSize(p []byte, w, h uint32) []byte {
	out := append([]byte{}, p...)
	binary.BigEndian.PutUint32(out[16:], w)
	binary.BigEndian.PutUint32(out[20:], h)
	binary.BigEndian.PutUint32(out[29:], crc32.ChecksumIEEE(out[12:29]))
	return out
}

// patchJPEGSize mengganti tinggi/lebar di segmen SOF0/SOF2.
func patchJPEGSize(t *testing.T, j []byte, w, h uint16) []byte {
	out := append([]byte{}, j...)
	for i := 2; i+9 < len(out); i++ {
		if out[i] == 0xFF && (out[i+1] == 0xC0 || out[i+1] == 0xC2) {
			binary.BigEndian.PutUint16(out[i+5:], h)
			binary.BigEndian.PutUint16(out[i+7:], w)
			return out
		}
	}
	t.Fatal("SOF tidak ditemukan")
	return nil
}

// withWebPExif membungkus bitstream VP8 dari berkas WebP sederhana ke kontainer VP8X + chunk EXIF.
func withWebPExif(t *testing.T, wp []byte, w, h int, o int) []byte {
	t.Helper()
	if string(wp[12:16]) != "VP8 " {
		t.Fatalf("fixture bukan VP8 sederhana: %q", wp[12:16])
	}
	vp8 := wp[12:]
	var body bytes.Buffer
	body.WriteString("WEBP")
	body.WriteString("VP8X")
	binary.Write(&body, binary.LittleEndian, uint32(10))
	x := make([]byte, 10)
	x[0] = 0x08 // flag EXIF
	put24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(x[4:], w-1)
	put24(x[7:], h-1)
	body.Write(x)
	body.Write(vp8)
	ex := append([]byte("Exif\x00\x00"), tiffOrient(o)...)
	body.WriteString("EXIF")
	binary.Write(&body, binary.LittleEndian, uint32(len(ex)))
	body.Write(ex)
	if len(ex)%2 == 1 {
		body.WriteByte(0)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeOut(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("keluaran bukan JPEG valid: %v", err)
	}
	return img
}

func isRedAt(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r>>8 > 150 && g>>8 < 90 && b>>8 < 90
}

func isWhiteAt(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r>>8 > 235 && g>>8 > 235 && b>>8 > 235
}

// ---------- pemrosesan: ukuran ----------

func TestImageResizeSizes(t *testing.T) {
	cases := []struct {
		name         string
		data         func(t *testing.T) []byte
		w, h, tw, th int
		kind         imageKind
	}{
		{"jpeg lanskap besar", func(t *testing.T) []byte { return encJPEG(t, markerImage(3000, 2000), 90) }, 1200, 800, 400, 267, kindJPEG},
		{"jpeg potret besar", func(t *testing.T) []byte { return encJPEG(t, markerImage(1000, 3000), 90) }, 400, 1200, 133, 400, kindJPEG},
		{"jpeg kecil tidak diperbesar", func(t *testing.T) []byte { return encJPEG(t, markerImage(300, 200), 90) }, 300, 200, 300, 200, kindJPEG},
		{"jpeg sedang", func(t *testing.T) []byte { return encJPEG(t, markerImage(800, 600), 90) }, 800, 600, 400, 300, kindJPEG},
		{"png persegi", func(t *testing.T) []byte { return encPNG(t, markerImage(1500, 1500)) }, 1200, 1200, 400, 400, kindPNG},
		{"webp lossy", func(t *testing.T) []byte { return readFixture(t, "lossy_1600x900.webp") }, 1200, 675, 400, 225, kindWebP},
		{"webp lossless alfa", func(t *testing.T) []byte { return readFixture(t, "alpha_300x200.webp") }, 300, 200, 300, 200, kindWebP},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := processProductImage(c.data(t))
			if err != nil {
				t.Fatal(err)
			}
			if p.Width != c.w || p.Height != c.h || p.ThumbWidth != c.tw || p.ThumbHeight != c.th || p.SourceKind != c.kind {
				t.Fatalf("ukuran %dx%d thumb %dx%d (%s), mau %dx%d thumb %dx%d", p.Width, p.Height, p.ThumbWidth, p.ThumbHeight, p.SourceKind, c.w, c.h, c.tw, c.th)
			}
			m, th := decodeOut(t, p.Main), decodeOut(t, p.Thumb)
			if m.Bounds().Dx() != c.w || m.Bounds().Dy() != c.h || th.Bounds().Dx() != c.tw || th.Bounds().Dy() != c.th {
				t.Fatalf("berkas keluaran berukuran %v / %v", m.Bounds(), th.Bounds())
			}
			if max(c.w, c.h) > mainImageMaxSide || max(c.tw, c.th) > thumbMaxSide {
				t.Fatal("melebihi batas sisi")
			}
		})
	}
}

func TestImageFitSizeRatio(t *testing.T) {
	for _, c := range [][4]int{{4000, 3000, 1200, 900}, {3000, 4000, 900, 1200}, {1201, 1, 1200, 1}, {1, 5000, 1, 1200}, {1200, 1200, 1200, 1200}, {10, 10, 10, 10}} {
		w, h := fitSize(c[0], c[1], 1200)
		if w != c[2] || h != c[3] {
			t.Errorf("fitSize(%d,%d) = %d,%d; mau %d,%d", c[0], c[1], w, h, c[2], c[3])
		}
	}
}

func TestImagePNGTransparentFlattenedWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 40; y < 60; y++ {
		for x := 90; x < 110; x++ {
			img.Set(x, y, color.NRGBA{220, 10, 10, 255})
		}
	}
	p, err := processProductImage(encPNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOut(t, p.Main)
	if !isWhiteAt(out, 2, 2) || !isWhiteAt(out, 197, 97) {
		t.Fatalf("area transparan harus putih, sudut = %v", out.At(2, 2))
	}
	if !isRedAt(out, 100, 50) {
		t.Fatalf("isi buram harus tetap: %v", out.At(100, 50))
	}
	// WebP lossless dengan alfa: sudut kanan-bawah transparan -> putih; kotak hijau kiri-atas tetap.
	p, err = processProductImage(readFixture(t, "alpha_300x200.webp"))
	if err != nil {
		t.Fatal(err)
	}
	out = decodeOut(t, p.Main)
	if !isWhiteAt(out, 295, 195) {
		t.Fatalf("webp transparan harus putih: %v", out.At(295, 195))
	}
	if r, g, b, _ := out.At(10, 10).RGBA(); !(g>>8 > 200 && r>>8 < 60 && b>>8 < 60) {
		t.Fatalf("kotak hijau hilang: %v", out.At(10, 10))
	}
}

// ---------- orientasi EXIF ----------

// Posisi blok penanda (kiri-atas data tersimpan) setelah orientasi diterapkan.
var orientWant = map[int]struct {
	corner string // TL, TR, BL, BR
	swap   bool
}{1: {"TL", false}, 2: {"TR", false}, 3: {"BR", false}, 4: {"BL", false}, 5: {"TL", true}, 6: {"TR", true}, 7: {"BR", true}, 8: {"BL", true}}

func checkOrientation(t *testing.T, label string, data []byte, o, sw, sh int) {
	t.Helper()
	p, err := processProductImage(data)
	if err != nil {
		t.Fatalf("%s o=%d: %v", label, o, err)
	}
	if p.Orientation != o {
		t.Fatalf("%s: orientasi terbaca %d, mau %d", label, p.Orientation, o)
	}
	want := orientWant[o]
	ew, eh := sw, sh
	if want.swap {
		ew, eh = sh, sw
	}
	if p.Width != ew || p.Height != eh {
		t.Fatalf("%s o=%d: ukuran %dx%d, mau %dx%d", label, o, p.Width, p.Height, ew, eh)
	}
	img := decodeOut(t, p.Main)
	pos := map[string][2]int{"TL": {ew / 16, eh / 16}, "TR": {ew - 1 - ew/16, eh / 16}, "BL": {ew / 16, eh - 1 - eh/16}, "BR": {ew - 1 - ew/16, eh - 1 - eh/16}}
	for name, xy := range pos {
		red := isRedAt(img, xy[0], xy[1])
		if (name == want.corner) != red {
			t.Fatalf("%s o=%d: sudut %s merah=%v (penanda harus di %s)", label, o, name, red, want.corner)
		}
	}
}

func TestImageEXIFOrientationJPEG(t *testing.T) {
	base := encJPEG(t, markerImage(400, 200), 92)
	for o := 1; o <= 8; o++ {
		checkOrientation(t, "jpeg", withJPEGExif(base, o), o, 400, 200)
	}
	// Tanpa EXIF -> 1; nilai di luar 1..8 diabaikan.
	checkOrientation(t, "jpeg tanpa exif", base, 1, 400, 200)
	if p, _ := processProductImage(withJPEGExif(base, 9)); p == nil || p.Orientation != 1 {
		t.Fatal("orientasi 9 harus diabaikan")
	}
	// Orientasi diterapkan sebelum pengecilan: 3000x1500 dengan o=6 -> 600x1200.
	big := withJPEGExif(encJPEG(t, markerImage(3000, 1500), 85), 6)
	checkOrientation(t, "jpeg besar", big, 6, 1200, 600)
}

func TestImageEXIFOrientationPNGAndWebP(t *testing.T) {
	base := encPNG(t, markerImage(400, 200))
	for _, o := range []int{3, 6, 8} {
		checkOrientation(t, "png", withPNGExif(base, o), o, 400, 200)
	}
	wp := readFixture(t, "lossy_1600x900.webp")
	p, err := processProductImage(withWebPExif(t, wp, 1600, 900, 6))
	if err != nil {
		t.Fatal(err)
	}
	if p.Orientation != 6 || p.Width != 675 || p.Height != 1200 {
		t.Fatalf("webp o=6: %d %dx%d", p.Orientation, p.Width, p.Height)
	}
}

func TestImageMetadataStripped(t *testing.T) {
	src := withJPEGExif(encJPEG(t, markerImage(500, 300), 90), 6)
	if !bytes.Contains(src, []byte("RAHASIA-GPS")) {
		t.Fatal("fixture tidak memuat metadata")
	}
	p, err := processProductImage(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range [][]byte{p.Main, p.Thumb} {
		if bytes.Contains(out, []byte("RAHASIA")) || bytes.Contains(out, []byte("Exif\x00\x00")) || jpegExif(out) != nil {
			t.Fatal("metadata EXIF masih ada di keluaran")
		}
		if !bytes.HasPrefix(out, []byte{0xFF, 0xD8, 0xFF}) {
			t.Fatal("keluaran bukan JPEG")
		}
		// Tidak ada segmen APP1..APP15 (metadata) sama sekali.
		for i := 2; i+3 < len(out) && out[i] == 0xFF && out[i+1] != 0xDA; {
			if out[i+1] >= 0xE1 && out[i+1] <= 0xEF {
				t.Fatalf("segmen APP%d masih ada", out[i+1]-0xE0)
			}
			i += 2 + int(binary.BigEndian.Uint16(out[i+2:]))
		}
	}
	if p.Orientation != 6 {
		t.Fatal("orientasi harus tetap diterapkan")
	}
}

// ---------- penolakan ----------

func TestImageRejections(t *testing.T) {
	good := encJPEG(t, markerImage(400, 300), 85)
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, markerImage(20, 20), nil); err != nil {
		t.Fatal(err)
	}
	smallPNG := encPNG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	cases := map[string]struct {
		data []byte
		want error
	}{
		"gif":                   {gifBuf.Bytes(), errImageType},
		"svg":                   {[]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), errImageType},
		"svg tanpa xml":         {[]byte(`<svg onload="alert(1)"></svg>`), errImageType},
		"teks berekstensi .jpg": {[]byte("ini bukan foto, hanya teks biasa yang diberi nama foto.jpg"), errImageType},
		"html":                  {[]byte("<html><body>x</body></html>"), errImageType},
		"avif":                  {append([]byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"), make([]byte, 64)...), errImageType},
		"heic":                  {append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), make([]byte, 64)...), errImageType},
		"bmp":                   {append([]byte("BM"), make([]byte, 64)...), errImageType},
		"kosong":                {nil, errImageType},
		"riff bukan webp":       {append([]byte("RIFF\x10\x00\x00\x00WAVEfmt "), make([]byte, 32)...), errImageType},
		"jpeg terpotong":        {good[:len(good)/2], errImageCorrupt},
		"jpeg hanya header":     {good[:20], errImageCorrupt},
		"jpeg isi acak":         {append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x5A}, 500)...), errImageCorrupt},
		"png rusak":             {append(append([]byte{}, smallPNG[:40]...), bytes.Repeat([]byte{1}, 40)...), errImageCorrupt},
		"lebih dari 2 MB":       {append(append([]byte{}, good...), make([]byte, maxUploadBytes)...), errImageTooLarge},
		"sisi > 12000 (jpeg)":   {patchJPEGSize(t, good, 12001, 10), errImageResolution},
		"bom piksel png 7000²":  {patchPNGSize(smallPNG, 7000, 7000), errImageResolution},
		"bom png 60000x60000":   {patchPNGSize(smallPNG, 60000, 60000), errImageResolution},
		"bom jpeg 65000x65000":  {patchJPEGSize(t, good, 65000, 65000), errImageResolution},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := processProductImage(c.data)
			if !errors.Is(err, c.want) || p != nil {
				t.Fatalf("galat %v, mau %v", err, c.want)
			}
		})
	}
	// Tepat 2 MB masih boleh dibaca (batas inklusif), 2 MB + 1 ditolak.
	if _, err := processProductImage(append(append([]byte{}, good...), make([]byte, maxUploadBytes-len(good))...)); errors.Is(err, errImageTooLarge) {
		t.Fatal("tepat 2 MB tidak boleh ditolak karena ukuran")
	}
}

// Dekompresi-bom tidak boleh mengalokasikan memori besar: ditolak dari DecodeConfig saja.
func TestImageBombRejectedCheaply(t *testing.T) {
	bomb := patchPNGSize(encPNG(t, image.NewGray(image.Rect(0, 0, 8, 8))), 11000, 11000)
	allocs := testing.AllocsPerRun(5, func() { _, _ = processProductImage(bomb) })
	if allocs > 200 {
		t.Fatalf("terlalu banyak alokasi untuk bom: %v", allocs)
	}
}

func TestImageSniff(t *testing.T) {
	if sniffImage([]byte{0xFF, 0xD8, 0xFF, 0xDB}) != kindJPEG || sniffImage([]byte("\x89PNG\r\n\x1a\n....")) != kindPNG ||
		sniffImage([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")) != kindWebP || sniffImage([]byte("GIF89a")) != "" || sniffImage([]byte("RIFF\x00\x00\x00\x00WEBPXXXX")) != "" {
		t.Fatal("sniff salah")
	}
}

func TestImageErrorStatus(t *testing.T) {
	cases := map[error]int{errImageTooLarge: 413, errImageType: 415, errImageCorrupt: 422, errImageResolution: 422,
		errStoreFull: 507, errStoreUnavailable: 503, errors.New("lain"): 500}
	for err, want := range cases {
		if st, msg := imageErrorStatus(err); st != want || msg == "" {
			t.Errorf("%v -> %d (%q), mau %d", err, st, msg, want)
		}
	}
	if st, _ := imageErrorStatus(wrapStoreErr(os.NewSyscallError("write", syscall.ENOSPC))); st != 507 {
		t.Errorf("ENOSPC -> %d", st)
	}
}

// ---------- kunci & URL ----------

func TestProductImageKeys(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		k, err := newProductImageKey(12)
		if err != nil || !isProductImageKey(k) || !productImageKeyFor(k, 12) || productImageKeyFor(k, 1) || seen[k] {
			t.Fatalf("kunci tidak valid/unik: %q %v", k, err)
		}
		seen[k] = true
		uuid := strings.TrimSuffix(strings.TrimPrefix(k, "products/12/"), ".jpg")
		if len(uuid) != 36 || uuid[14] != '4' || !strings.ContainsRune("89ab", rune(uuid[19])) {
			t.Fatalf("bukan UUID v4: %s", uuid)
		}
		if thumbKey(k) != "products/12/"+uuid+"_t.jpg" {
			t.Fatalf("thumbKey %s", thumbKey(k))
		}
	}
	valid := []string{"products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "products/0/00000000-0000-4000-8000-000000000000.jpg"}
	invalid := []string{"", "kerupuk1.jpg", "produk/kerupuk-5.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e_t.jpg",
		"products/12/0F8FAD5B-D9CB-469F-A165-70867728950E.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e.png",
		"products/x/0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "/products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg",
		"products/12/../0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg\n",
		"products/12/------------------------------------.jpg", "https://x/products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg"}
	for _, v := range valid {
		if !isProductImageKey(v) {
			t.Errorf("harus sah: %q", v)
		}
	}
	for _, v := range invalid {
		if isProductImageKey(v) {
			t.Errorf("harus ditolak: %q", v)
		}
	}
	s := NewLocalStore(t.TempDir(), "")
	if i, th := publicImageURLs(s, valid[0]); i != "/uploads/"+valid[0] || th != "/uploads/products/12/0f8fad5b-d9cb-469f-a165-70867728950e_t.jpg" {
		t.Fatalf("URL publik: %q %q", i, th)
	}
	for _, legacy := range []string{"", "kerupuk1.jpg", "bumbu/rendang.jpg"} {
		if i, th := publicImageURLs(s, legacy); i != "" || th != "" {
			t.Fatalf("nilai lama %q harus kosong: %q %q", legacy, i, th)
		}
	}
	if i, th := publicImageURLs(nil, valid[0]); i != "" || th != "" {
		t.Fatal("tanpa store harus kosong")
	}
}

// ---------- LocalStore ----------

func TestLocalStoreSaveDelete(t *testing.T) {
	base := t.TempDir()
	s := NewLocalStore(base, "/uploads/")
	ctx := context.Background()
	key := "products/7/0f8fad5b-d9cb-469f-a165-70867728950e.jpg"
	if err := s.Save(ctx, key, []byte("isi-1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, key, []byte("isi-2-lebih-panjang")); err != nil { // menimpa secara atomik
		t.Fatal(err)
	}
	p := filepath.Join(base, "products", "7", "0f8fad5b-d9cb-469f-a165-70867728950e.jpg")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "isi-2-lebih-panjang" {
		t.Fatalf("isi: %q %v", b, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode berkas %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("berkas sementara tertinggal: %v", entries)
	}
	if u, err := s.Usage(ctx); err != nil || u != int64(len("isi-2-lebih-panjang")) {
		t.Fatalf("usage %d %v", u, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("berkas belum terhapus")
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("hapus berkas yang tidak ada bukan galat: %v", err)
	}
	if err := s.Writable(); err != nil {
		t.Fatal(err)
	}
	if s.URL(key) != "/uploads/"+key {
		t.Fatal("URL")
	}
}

func TestLocalStoreRejectsEscapingKeys(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "uploads")
	s := NewLocalStore(base, "")
	ctx := context.Background()
	outside := filepath.Join(root, "rahasia.txt")
	os.WriteFile(outside, []byte("jangan dihapus"), 0o600)
	bad := []string{"", "../rahasia.txt", "../../etc/passwd", "/etc/passwd", "products/../../rahasia.txt", "products/1/../../../rahasia.txt",
		"products\\..\\..\\rahasia.txt", "products//1/x.jpg", "./x.jpg", "products/1/.", "Products/1/X.JPG", "products/1/x.jpg\x00",
		"products/1/%2e%2e/x.jpg", "products/1/ x.jpg", strings.Repeat("a", 256), "..", "products/.."}
	for _, k := range bad {
		if err := s.Save(ctx, k, []byte("x")); !errors.Is(err, errBadKey) {
			t.Errorf("Save(%q) harus ditolak: %v", k, err)
		}
		if err := s.Delete(ctx, k); !errors.Is(err, errBadKey) {
			t.Errorf("Delete(%q) harus ditolak: %v", k, err)
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "jangan dihapus" {
		t.Fatal("berkas di luar direktori dasar berubah")
	}
	if _, err := os.Stat(filepath.Join(root, "x.jpg")); !os.IsNotExist(err) {
		t.Fatal("berkas tertulis di luar direktori dasar")
	}
}

func TestLocalStoreReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root mengabaikan izin berkas")
	}
	base := t.TempDir()
	os.Chmod(base, 0o500)
	defer os.Chmod(base, 0o700)
	s := NewLocalStore(base, "")
	if err := s.Writable(); err == nil {
		t.Fatal("direktori read-only harus terdeteksi")
	}
	err := s.Save(context.Background(), "products/1/0f8fad5b-d9cb-469f-a165-70867728950e.jpg", []byte("x"))
	if !errors.Is(err, errStoreUnavailable) {
		t.Fatalf("galat %v", err)
	}
}

// ---------- pagar disk ----------

type fakeHealth struct {
	usage, calls atomic.Int64
	mu           sync.Mutex
	writeErr     error
}

func (f *fakeHealth) setWriteErr(e error) { f.mu.Lock(); f.writeErr = e; f.mu.Unlock() }
func (f *fakeHealth) Writable() error     { f.mu.Lock(); defer f.mu.Unlock(); return f.writeErr }
func (f *fakeHealth) Usage(context.Context) (int64, error) {
	f.calls.Add(1)
	return f.usage.Load(), nil
}

func TestDiskGuard(t *testing.T) {
	h := &fakeHealth{}
	g := newDiskGuard(h, 1000, 5*time.Minute)
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	g.startupCheck("/uji")
	ctx := context.Background()
	h.usage.Store(500)
	if err := g.check(ctx); err != nil {
		t.Fatal(err)
	}
	h.usage.Store(5000) // berubah di disk, tetapi cache masih berlaku
	for i := 0; i < 10; i++ {
		if err := g.check(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if h.calls.Load() != 1 {
		t.Fatalf("disk dipindai %d kali; harus memakai cache", h.calls.Load())
	}
	g.add(600) // 500 + 600 >= 1000 -> penuh walau cache belum kedaluwarsa
	if err := g.check(ctx); !errors.Is(err, errStoreFull) {
		t.Fatalf("harus penuh: %v", err)
	}
	g.add(-600)
	if err := g.check(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute) // cache kedaluwarsa -> pindai ulang (5000)
	if err := g.check(ctx); !errors.Is(err, errStoreFull) || h.calls.Load() != 2 {
		t.Fatalf("pindai ulang: %v %d", err, h.calls.Load())
	}

	// Tidak dapat ditulis -> 503; diperiksa ulang paling cepat tiap menit.
	h2 := &fakeHealth{}
	h2.setWriteErr(errors.New("read-only"))
	g2 := newDiskGuard(h2, 1000, time.Minute)
	g2.now = func() time.Time { return now }
	g2.startupCheck("/uji")
	if err := g2.check(ctx); !errors.Is(err, errStoreUnavailable) {
		t.Fatalf("harus 503: %v", err)
	}
	h2.setWriteErr(nil)
	if err := g2.check(ctx); !errors.Is(err, errStoreUnavailable) {
		t.Fatal("pemeriksaan ulang terlalu cepat")
	}
	now = now.Add(time.Minute)
	if err := g2.check(ctx); err != nil {
		t.Fatalf("setelah pulih harus boleh: %v", err)
	}
	var nilGuard *diskGuard
	if nilGuard.check(ctx) != nil {
		t.Fatal("guard nil = tanpa batas")
	}
}

// ---------- CSRF multipart ----------

func TestCSRFMultipartOnlyForUpload(t *testing.T) {
	origins := []string{"https://store.mihan.web.id"}
	mk := func(ct string, xrw bool, origin string) struct{ r *http.Request } {
		r := httptest.NewRequest("POST", "/api/admin/products/1/image", nil)
		r.Header.Set("Content-Type", ct)
		if xrw {
			r.Header.Set("X-Requested-With", adminRequestedWith)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return struct{ r *http.Request }{r}
	}
	mp := "multipart/form-data; boundary=abc"
	if err := checkCSRFOpts(mk(mp, true, origins[0]).r, origins, true); err != nil {
		t.Fatalf("multipart sah ditolak: %v", err)
	}
	if err := checkCSRFOpts(mk(mp, true, origins[0]).r, origins, false); err == nil {
		t.Fatal("multipart di rute lain harus ditolak")
	}
	if err := checkCSRF(mk(mp, true, origins[0]).r, origins); err == nil {
		t.Fatal("checkCSRF lama tetap hanya JSON")
	}
	if err := checkCSRFOpts(mk(mp, false, origins[0]).r, origins, true); err == nil {
		t.Fatal("tanpa X-Requested-With harus ditolak")
	}
	if err := checkCSRFOpts(mk(mp, true, "https://jahat.example").r, origins, true); err == nil {
		t.Fatal("origin asing harus ditolak")
	}
	if err := checkCSRFOpts(mk("application/x-www-form-urlencoded", true, origins[0]).r, origins, true); err == nil {
		t.Fatal("form biasa harus ditolak")
	}
	if err := checkCSRFOpts(mk(mp, true, "").r, origins, true); err == nil {
		t.Fatal("tanpa Origin dan Sec-Fetch-Site harus ditolak")
	}
	for _, p := range []string{"/api/admin/products/1/image", "/api/admin/products/123/image"} {
		if !imageUploadPathRe.MatchString(p) {
			t.Errorf("%s harus cocok", p)
		}
	}
	for _, p := range []string{"/api/admin/products/1", "/api/admin/products/1/image/x", "/api/admin/products/a/image", "/api/admin/settings"} {
		if imageUploadPathRe.MatchString(p) {
			t.Errorf("%s tidak boleh cocok", p)
		}
	}
}
