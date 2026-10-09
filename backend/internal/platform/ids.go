package platform

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// NewPublicID menghasilkan UUID v4 acak (36 karakter). Dipakai banyak modul (id publik akun,
// kunci foto produk, kunci bukti transfer).
func NewPublicID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// PathID membaca variabel rute {id} (bilangan bulat > 0).
func PathID(r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	return id, err == nil && id > 0
}
