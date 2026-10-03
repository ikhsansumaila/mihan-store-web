package main

// Pemrosesan foto produk (murni, tanpa I/O): deteksi tipe dari ISI berkas, penolakan
// dekompresi-bom lewat DecodeConfig, orientasi EXIF (1–8), perataan transparansi ke latar
// putih, pengecilan berkualitas (Catmull-Rom), dan encode ulang JPEG tanpa metadata.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	maxUploadBytes   = 2 << 20 // 2 MB (isi berkas)
	maxImagePixels   = 40_000_000
	maxImageSide     = 12000
	mainImageMaxSide = 1200
	thumbMaxSide     = 400
	mainJPEGQuality  = 82
	thumbJPEGQuality = 80
)

// Galat pemrosesan; dipetakan ke status HTTP oleh handler (lihat imageErrorStatus).
var (
	errImageTooLarge   = errors.New("Ukuran foto melebihi 2 MB. Pilih foto yang lebih kecil.")
	errImageType       = errors.New("Format foto tidak didukung. Gunakan JPG, PNG, atau WebP.")
	errImageCorrupt    = errors.New("Berkas foto rusak atau tidak bisa dibaca. Coba foto lain.")
	errImageResolution = errors.New("Resolusi foto terlalu besar (maksimal 12.000 piksel per sisi dan 40 megapiksel).")
)

type imageKind string

const (
	kindJPEG imageKind = "jpeg"
	kindPNG  imageKind = "png"
	kindWebP imageKind = "webp"
)

// sniffImage menentukan tipe dari byte awal berkas (bukan ekstensi atau header klien).
// Hanya JPEG, PNG, dan WebP yang dikenali; selain itu "" (GIF, SVG, AVIF, HEIC, teks, ...).
func sniffImage(b []byte) imageKind {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return kindJPEG
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return kindPNG
	case len(b) >= 16 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")) &&
		(bytes.Equal(b[12:16], []byte("VP8 ")) || bytes.Equal(b[12:16], []byte("VP8L")) || bytes.Equal(b[12:16], []byte("VP8X"))):
		return kindWebP
	}
	return ""
}

func decodeConfig(kind imageKind, b []byte) (image.Config, error) {
	r := bytes.NewReader(b)
	switch kind {
	case kindJPEG:
		return jpeg.DecodeConfig(r)
	case kindPNG:
		return png.DecodeConfig(r)
	case kindWebP:
		return webp.DecodeConfig(r)
	}
	return image.Config{}, errImageType
}

func decodeImage(kind imageKind, b []byte) (image.Image, error) {
	r := bytes.NewReader(b)
	switch kind {
	case kindJPEG:
		return jpeg.Decode(r)
	case kindPNG:
		return png.Decode(r)
	case kindWebP:
		return webp.Decode(r)
	}
	return nil, errImageType
}

// ProcessedImage adalah hasil akhir: JPEG utama dan thumbnail (tanpa metadata).
type ProcessedImage struct {
	Main, Thumb             []byte
	Width, Height           int // ukuran foto utama
	ThumbWidth, ThumbHeight int
	SourceKind              imageKind
	Orientation             int
}

// processProductImage memeriksa dan memproses isi berkas unggahan.
func processProductImage(data []byte) (*ProcessedImage, error) {
	if len(data) > maxUploadBytes {
		return nil, errImageTooLarge
	}
	kind := sniffImage(data)
	if kind == "" {
		return nil, errImageType
	}
	cfg, err := decodeConfig(kind, data)
	if err != nil {
		return nil, errImageCorrupt
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errImageCorrupt
	}
	// Dekompresi-bom: ukuran piksel dicek SEBELUM decode penuh (alokasi memori).
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, errImageResolution
	}
	src, err := decodeImage(kind, data)
	if err != nil {
		return nil, errImageCorrupt
	}
	b := src.Bounds()
	if b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return nil, errImageCorrupt
	}
	orient := exifOrientation(kind, data)
	main := scaleOriented(src, orient, mainImageMaxSide)
	thumb := scaleOriented(main, 1, thumbMaxSide)
	mainJPG, err := encodeJPEG(main, mainJPEGQuality)
	if err != nil {
		return nil, err
	}
	thumbJPG, err := encodeJPEG(thumb, thumbJPEGQuality)
	if err != nil {
		return nil, err
	}
	return &ProcessedImage{
		Main: mainJPG, Thumb: thumbJPG,
		Width: main.Bounds().Dx(), Height: main.Bounds().Dy(),
		ThumbWidth: thumb.Bounds().Dx(), ThumbHeight: thumb.Bounds().Dy(),
		SourceKind: kind, Orientation: orient,
	}, nil
}

func encodeJPEG(img image.Image, q int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fitSize: ukuran (w,h) diperkecil agar sisi terpanjang <= max, rasio terjaga, tidak diperbesar.
func fitSize(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	if w >= h {
		nh := int(float64(h)*float64(max)/float64(w) + 0.5)
		return max, maxInt(nh, 1)
	}
	nw := int(float64(w)*float64(max)/float64(h) + 0.5)
	return maxInt(nw, 1), max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// swapsAxes: orientasi 5–8 memutar 90°, jadi lebar dan tinggi tampilan tertukar.
func swapsAxes(o int) bool { return o >= 5 && o <= 8 }

// scaleOriented mengecilkan src (bila perlu) ke latar putih (transparansi diratakan), lalu menerapkan
// orientasi EXIF pada hasil kecil (lebih hemat daripada memutar gambar asli yang besar).
func scaleOriented(src image.Image, orient, max int) *image.RGBA {
	sb := src.Bounds()
	dw, dh := sb.Dx(), sb.Dy()
	if swapsAxes(orient) {
		dw, dh = dh, dw
	}
	tw, th := fitSize(dw, dh, max) // ukuran akhir (setelah orientasi)
	sw, sh := tw, th               // ukuran dalam orientasi sumber
	if swapsAxes(orient) {
		sw, sh = th, tw
	}
	scaled := image.NewRGBA(image.Rect(0, 0, sw, sh))
	// Sumber buram (mis. JPEG/YCbCr): salin langsung (jalur cepat). Selain itu tumpuk di atas putih.
	op := draw.Over
	if o, ok := src.(interface{ Opaque() bool }); ok && o.Opaque() {
		op = draw.Src
	} else {
		draw.Draw(scaled, scaled.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	}
	if sw == sb.Dx() && sh == sb.Dy() {
		draw.Draw(scaled, scaled.Bounds(), src, sb.Min, op)
	} else {
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, op, nil)
	}
	return applyOrientation(scaled, orient)
}

// applyOrientation memetakan piksel sesuai tag EXIF Orientation (1 = apa adanya).
func applyOrientation(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if swapsAxes(o) {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // cermin horizontal
				nx, ny = w-1-x, y
			case 3: // putar 180°
				nx, ny = w-1-x, h-1-y
			case 4: // cermin vertikal
				nx, ny = x, h-1-y
			case 5: // transpose
				nx, ny = y, x
			case 6: // putar 90° searah jarum jam
				nx, ny = h-1-y, x
			case 7: // transverse
				nx, ny = h-1-y, w-1-x
			case 8: // putar 90° berlawanan jarum jam
				nx, ny = y, w-1-x
			}
			si := src.PixOffset(x, y)
			di := dst.PixOffset(nx, ny)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

// ---------- EXIF Orientation ----------

// exifOrientation membaca tag Orientation (0x0112) dari blok EXIF: JPEG (APP1 "Exif"), PNG (chunk eXIf),
// atau WebP (chunk EXIF). 1 bila tidak ada atau tidak valid.
func exifOrientation(kind imageKind, b []byte) int {
	var tiff []byte
	switch kind {
	case kindJPEG:
		tiff = jpegExif(b)
	case kindPNG:
		tiff = pngExif(b)
	case kindWebP:
		tiff = webpExif(b)
	}
	if o := tiffOrientation(tiff); o >= 1 && o <= 8 {
		return o
	}
	return 1
}

func jpegExif(b []byte) []byte {
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 || marker == 0xFF {
			i += 2
			if marker == 0xFF {
				i-- // padding 0xFF
			}
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // awal data gambar: tidak ada EXIF lagi
			return nil
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 || i+2+n > len(b) {
			return nil
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) >= 6 && bytes.Equal(seg[:6], []byte("Exif\x00\x00")) {
			return seg[6:]
		}
		i += 2 + n
	}
	return nil
}

func pngExif(b []byte) []byte {
	i := 8
	for i+8 <= len(b) {
		n := int(binary.BigEndian.Uint32(b[i : i+4]))
		typ := string(b[i+4 : i+8])
		if n < 0 || i+12+n > len(b) {
			return nil
		}
		if typ == "eXIf" {
			return b[i+8 : i+8+n]
		}
		if typ == "IDAT" || typ == "IEND" {
			return nil
		}
		i += 12 + n
	}
	return nil
}

func webpExif(b []byte) []byte {
	i := 12
	for i+8 <= len(b) {
		typ := string(b[i : i+4])
		n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if n < 0 || i+8+n > len(b) {
			return nil
		}
		if typ == "EXIF" {
			d := b[i+8 : i+8+n]
			if len(d) >= 6 && bytes.Equal(d[:6], []byte("Exif\x00\x00")) {
				d = d[6:]
			}
			return d
		}
		i += 8 + n + n%2
	}
	return nil
}

// tiffOrientation membaca IFD0 dari header TIFF (II/MM) dan mengembalikan nilai tag 0x0112 (0 bila tidak ada).
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:4]) != 42 {
		return 0
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	count := int(bo.Uint16(t[off : off+2]))
	for k := 0; k < count && k < 512; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			typ := bo.Uint16(t[e+2 : e+4])
			if typ != 3 { // SHORT
				return 0
			}
			return int(bo.Uint16(t[e+8 : e+10]))
		}
	}
	return 0
}
