package imaging

// Bagian HTTP bersama untuk unggahan gambar (foto produk dan bukti transfer): pembacaan multipart dengan batas ukuran, pemetaan galat ke status HTTP + pesan Indonesia,
// dan pembatas pemrosesan gambar paralel (SATU instance dipakai bersama oleh semua unggahan).

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"mihanstore/internal/platform"
)

const (
	MsgImageNoFile      = "Pilih berkas foto terlebih dahulu (field \"file\")."
	MsgImageStoreDown   = "Penyimpanan foto sedang tidak tersedia. Coba lagi nanti; bagian lain toko tetap berjalan."
	MsgImageStoreFull   = "Penyimpanan foto penuh. Hapus foto yang tidak dipakai atau hubungi pengelola server."
	MsgImageBusy        = "Server sedang memproses foto lain. Coba lagi sebentar lagi."
	multipartFileField  = "file"
	multipartMaxPartHdr = 8 << 10
)

// ImageErrorStatus memetakan galat pemrosesan/penyimpanan ke status HTTP dan pesan Indonesia.
func ImageErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrImageTooLarge):
		return http.StatusRequestEntityTooLarge, ErrImageTooLarge.Error()
	case errors.Is(err, ErrImageType):
		return http.StatusUnsupportedMediaType, ErrImageType.Error()
	case errors.Is(err, ErrImageCorrupt):
		return http.StatusUnprocessableEntity, ErrImageCorrupt.Error()
	case errors.Is(err, ErrImageResolution):
		return http.StatusUnprocessableEntity, ErrImageResolution.Error()
	case errors.Is(err, ErrStoreFull):
		return http.StatusInsufficientStorage, MsgImageStoreFull
	case errors.Is(err, ErrStoreUnavailable):
		return http.StatusServiceUnavailable, MsgImageStoreDown
	}
	return http.StatusInternalServerError, platform.MsgServerError
}

// ReadUploadFile membaca isi field "file" dari body multipart (maks. MaxUploadBytes).
func ReadUploadFile(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	return ReadUploadFileLimit(w, r, MaxUploadBytes, ErrImageTooLarge.Error())
}

// ReadUploadFileLimit seperti ReadUploadFile dengan batas isi berkas & pesan "terlalu besar" sendiri.
func ReadUploadFileLimit(w http.ResponseWriter, r *http.Request, maxBytes int, tooLarge string) ([]byte, int, string) {
	maxBody := int64(maxBytes) + 256<<10 // isi berkas + overhead multipart
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, http.StatusBadRequest, "Unggahan harus berupa multipart/form-data dengan field \"file\"."
	}
	if r.ContentLength > maxBody {
		return nil, http.StatusRequestEntityTooLarge, tooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, http.StatusBadRequest, "Format unggahan tidak valid."
	}
	for parts := 0; parts < 8; parts++ {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, tooLarge
			}
			return nil, http.StatusBadRequest, "Format unggahan tidak valid."
		}
		if p.FormName() != multipartFileField {
			_, _ = io.Copy(io.Discard, io.LimitReader(p, multipartMaxPartHdr))
			p.Close()
			continue
		}
		// Nama berkas dan Content-Type dari klien sengaja diabaikan.
		data, err := io.ReadAll(io.LimitReader(p, int64(maxBytes)+1))
		p.Close()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, tooLarge
			}
			return nil, http.StatusBadRequest, "Unggahan terputus. Coba lagi."
		}
		if len(data) > maxBytes {
			return nil, http.StatusRequestEntityTooLarge, tooLarge
		}
		if len(data) == 0 {
			return nil, http.StatusBadRequest, MsgImageNoFile
		}
		return data, 0, ""
	}
	return nil, http.StatusBadRequest, MsgImageNoFile
}

// Slots membatasi pemrosesan gambar paralel (decode foto besar memakan memori). Satu instance
// dipakai bersama oleh unggah foto produk dan bukti transfer.
type Slots chan struct{}

func NewSlots(n int) Slots { return make(Slots, n) }

// Acquire menunggu slot (maks. 20 detik atau sampai ctx selesai); false = tidak dapat slot.
func (s Slots) Acquire(ctx context.Context) (func(), bool) {
	select {
	case s <- struct{}{}:
		return func() { <-s }, true
	case <-ctx.Done():
		return nil, false
	case <-time.After(20 * time.Second):
		return nil, false
	}
}
