package orders

// Pembuat gambar sintetis untuk tes bukti transfer (salinan helper tes internal/platform/imaging).

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
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

// patchPNGSize mengganti lebar/tinggi di IHDR (CRC diperbarui) tanpa mengubah data piksel.
func patchPNGSize(p []byte, w, h uint32) []byte {
	out := append([]byte{}, p...)
	binary.BigEndian.PutUint32(out[16:], w)
	binary.BigEndian.PutUint32(out[20:], h)
	binary.BigEndian.PutUint32(out[29:], crc32.ChecksumIEEE(out[12:29]))
	return out
}
