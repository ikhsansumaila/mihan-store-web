package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mihanstore/internal/platform/imaging"
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

// ---------- kunci & URL ----------

func TestProductImageKeys(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		k, err := NewProductImageKey(12)
		if err != nil || !IsProductImageKey(k) || !ProductImageKeyFor(k, 12) || ProductImageKeyFor(k, 1) || seen[k] {
			t.Fatalf("kunci tidak valid/unik: %q %v", k, err)
		}
		seen[k] = true
		uuid := strings.TrimSuffix(strings.TrimPrefix(k, "products/12/"), ".jpg")
		if len(uuid) != 36 || uuid[14] != '4' || !strings.ContainsRune("89ab", rune(uuid[19])) {
			t.Fatalf("bukan UUID v4: %s", uuid)
		}
		if ThumbKey(k) != "products/12/"+uuid+"_t.jpg" {
			t.Fatalf("ThumbKey %s", ThumbKey(k))
		}
	}
	valid := []string{"products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "products/0/00000000-0000-4000-8000-000000000000.jpg"}
	invalid := []string{"", "kerupuk1.jpg", "produk/kerupuk-5.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e_t.jpg",
		"products/12/0F8FAD5B-D9CB-469F-A165-70867728950E.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e.png",
		"products/x/0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "/products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg",
		"products/12/../0f8fad5b-d9cb-469f-a165-70867728950e.jpg", "products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg\n",
		"products/12/------------------------------------.jpg", "https://x/products/12/0f8fad5b-d9cb-469f-a165-70867728950e.jpg"}
	for _, v := range valid {
		if !IsProductImageKey(v) {
			t.Errorf("harus sah: %q", v)
		}
	}
	for _, v := range invalid {
		if IsProductImageKey(v) {
			t.Errorf("harus ditolak: %q", v)
		}
	}
	s := NewLocalStore(t.TempDir(), "")
	if i, th := PublicImageURLs(s, valid[0]); i != "/uploads/"+valid[0] || th != "/uploads/products/12/0f8fad5b-d9cb-469f-a165-70867728950e_t.jpg" {
		t.Fatalf("URL publik: %q %q", i, th)
	}
	for _, legacy := range []string{"", "kerupuk1.jpg", "bumbu/rendang.jpg"} {
		if i, th := PublicImageURLs(s, legacy); i != "" || th != "" {
			t.Fatalf("nilai lama %q harus kosong: %q %q", legacy, i, th)
		}
	}
	if i, th := PublicImageURLs(nil, valid[0]); i != "" || th != "" {
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
		if err := s.Save(ctx, k, []byte("x")); !errors.Is(err, imaging.ErrBadKey) {
			t.Errorf("Save(%q) harus ditolak: %v", k, err)
		}
		if err := s.Delete(ctx, k); !errors.Is(err, imaging.ErrBadKey) {
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
	if !errors.Is(err, imaging.ErrStoreUnavailable) {
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
	g := NewDiskGuard(h, 1000, 5*time.Minute)
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	g.StartupCheck("/uji")
	ctx := context.Background()
	h.usage.Store(500)
	if err := g.Check(ctx); err != nil {
		t.Fatal(err)
	}
	h.usage.Store(5000) // berubah di disk, tetapi cache masih berlaku
	for i := 0; i < 10; i++ {
		if err := g.Check(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if h.calls.Load() != 1 {
		t.Fatalf("disk dipindai %d kali; harus memakai cache", h.calls.Load())
	}
	g.Add(600) // 500 + 600 >= 1000 -> penuh walau cache belum kedaluwarsa
	if err := g.Check(ctx); !errors.Is(err, imaging.ErrStoreFull) {
		t.Fatalf("harus penuh: %v", err)
	}
	g.Add(-600)
	if err := g.Check(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute) // cache kedaluwarsa -> pindai ulang (5000)
	if err := g.Check(ctx); !errors.Is(err, imaging.ErrStoreFull) || h.calls.Load() != 2 {
		t.Fatalf("pindai ulang: %v %d", err, h.calls.Load())
	}

	// Tidak dapat ditulis -> 503; diperiksa ulang paling cepat tiap menit.
	h2 := &fakeHealth{}
	h2.setWriteErr(errors.New("read-only"))
	g2 := NewDiskGuard(h2, 1000, time.Minute)
	g2.now = func() time.Time { return now }
	g2.StartupCheck("/uji")
	if err := g2.Check(ctx); !errors.Is(err, imaging.ErrStoreUnavailable) {
		t.Fatalf("harus 503: %v", err)
	}
	h2.setWriteErr(nil)
	if err := g2.Check(ctx); !errors.Is(err, imaging.ErrStoreUnavailable) {
		t.Fatal("pemeriksaan ulang terlalu cepat")
	}
	now = now.Add(time.Minute)
	if err := g2.Check(ctx); err != nil {
		t.Fatalf("setelah pulih harus boleh: %v", err)
	}
	var nilGuard *DiskGuard
	if nilGuard.Check(ctx) != nil {
		t.Fatal("guard nil = tanpa batas")
	}
}
