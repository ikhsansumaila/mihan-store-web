package identity

// Autentikasi pelanggan (Bearer sesi) untuk rute yang wajib login.

import (
	"context"
	"log"
	"net/http"

	"mihanstore/internal/platform"
)

type ctxKey int

const (
	msgLoginRequired = "Silakan login terlebih dahulu"
	customerUserKey  = ctxKey(2)
)

func CustomerFrom(ctx context.Context) *User {
	u, _ := ctx.Value(customerUserKey).(*User)
	return u
}

// Customer membungkus handler yang wajib login pelanggan (Bearer token sesi).
func (a *Service) Customer(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if !wellFormedToken(token) {
			platform.WriteError(w, http.StatusUnauthorized, msgLoginRequired)
			return
		}
		db := a.db()
		if db == nil {
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		u, _, err := a.authenticate(r.Context(), db, token)
		if err != nil {
			log.Printf("customer auth: %v", err)
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		if u == nil {
			platform.WriteError(w, http.StatusUnauthorized, msgInvalidSession)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), customerUserKey, u)))
	})
}
