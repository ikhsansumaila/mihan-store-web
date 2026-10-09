package orders

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mihanstore/internal/notify"
	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
	"mihanstore/internal/push"
)

func TestProcessPaymentProof(t *testing.T) {
	// JPEG dengan EXIF (orientasi 6 + teks "GPS" tiruan): orientasi diterapkan, metadata hilang.
	src := withJPEGExif(encJPEG(t, markerImage(3000, 1500), 90), 6)
	p, err := processPaymentProof(src)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1000 || p.Height != 2000 {
		t.Fatalf("ukuran: %dx%d (maks. 2000 px, orientasi diterapkan)", p.Width, p.Height)
	}
	if bytes.Contains(p.Data, []byte("RAHASIA-GPS-UJI")) || bytes.Contains(p.Data, []byte("Exif")) {
		t.Fatal("metadata EXIF harus dibuang")
	}
	if _, err := jpeg.Decode(bytes.NewReader(p.Data)); err != nil || len(p.SHA256) != 64 {
		t.Fatalf("hasil JPEG/hash: %v %d", err, len(p.SHA256))
	}
	// PNG diterima (dinormalkan ke JPEG); gambar kecil tidak diperbesar.
	if q, err := processPaymentProof(encPNG(t, markerImage(300, 200))); err != nil || q.Width != 300 || imaging.SniffImage(q.Data) != imaging.KindJPEG {
		t.Fatalf("png: %v", err)
	}
	// Teks berekstensi .jpg, HEIC, terlalu besar, rusak.
	if _, err := processPaymentProof([]byte("bukan gambar, hanya teks")); err != imaging.ErrImageType {
		t.Fatalf("teks: %v", err)
	}
	heic := append([]byte{0, 0, 0, 0x18}, []byte("ftypheic")...)
	if _, err := processPaymentProof(append(heic, make([]byte, 64)...)); err != imaging.ErrImageType {
		t.Fatalf("heic harus ditolak sebagai tipe tidak didukung: %v", err)
	}
	if _, err := processPaymentProof(make([]byte, ProofMaxUploadBytes+1)); err != imaging.ErrImageTooLarge {
		t.Fatalf("terlalu besar: %v", err)
	}
	if _, err := processPaymentProof([]byte{0xFF, 0xD8, 0xFF, 0x00, 0x01}); err != imaging.ErrImageCorrupt {
		t.Fatalf("rusak: %v", err)
	}
}

func TestProofStorePrivate(t *testing.T) {
	base := filepath.Join(t.TempDir(), "private", "payment-proofs")
	s := NewProofStore(base)
	k1, _ := newProofKey()
	k2, _ := newProofKey()
	if k1 == k2 || !proofKeyRe.MatchString(k1) {
		t.Fatalf("kunci acak: %s %s", k1, k2)
	}
	if err := s.Save(k1, []byte("data")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(base, k1))
	di, _ := os.Stat(base)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("izin: berkas %v folder %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("berkas sementara tertinggal: %v", entries)
	}
	for _, bad := range []string{"../x.jpg", "a/b.jpg", "x.png", "", "nama-asli.jpg"} {
		if err := s.Save(bad, []byte("x")); err == nil {
			t.Errorf("kunci %q harus ditolak", bad)
		}
	}
	if err := s.Delete(k1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(k1); err != nil {
		t.Fatal("hapus berkas yang sudah hilang harus idempoten")
	}
	if !strings.HasSuffix(LoadConfigDefaultsProofDir(), "/private-uploads/payment-proofs") || strings.Contains(LoadConfigDefaultsProofDir(), "/uploads/") {
		t.Fatalf("folder bawaan harus di luar /uploads: %s", LoadConfigDefaultsProofDir())
	}
}

// LoadConfigDefaultsProofDir: folder bawaan PAYMENT_PROOF_DIR (tanpa env).
func LoadConfigDefaultsProofDir() string {
	os.Unsetenv("PAYMENT_PROOF_DIR")
	return platform.LoadConfig().PaymentProofDir
}

func TestProofNotificationsMinimal(t *testing.T) {
	b, err := notify.BuildPayload(notify.OrderEvent{Kind: notify.KindPaymentProof, OrderNo: "MS-261008-0001", CustomerName: "Bu Siti",
		ItemCount: 3, Total: 123456, Status: StatusPending, AdminURL: "https://store.mihan.web.id/admin/orders/1"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "Bukti pembayaran MS-261008-0001") || strings.Contains(s, "123.456") || strings.Contains(s, "/admin/") ||
		!strings.Contains(s, "Menunggu pembayaran") || !strings.Contains(s, "Bu Siti") {
		t.Fatalf("payload Discord bukti: %s", s)
	}
	pb, err := push.BuildPayload(push.Event{Kind: push.KindPaymentProof, OrderNo: "MS-261008-0001", Customer: "Bu Siti", Recipient: "Siti"})
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	json.Unmarshal(pb, &p)
	if p["title"] != "Bukti Pembayaran" || p["body"] != "dari Bu Siti\npenerima Siti\nMS-261008-0001" || p["url"] != "/admin/orders/MS-261008-0001" || p["tag"] != "pesanan-MS-261008-0001" {
		t.Fatalf("push bukti: %v", p)
	}
}
