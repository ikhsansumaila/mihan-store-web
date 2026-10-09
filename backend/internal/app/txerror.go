package app

import (
	"errors"
	"log"
	"net/http"

	"mihanstore/internal/catalog"
	"mihanstore/internal/platform"
)

// respondTxError menulis galat dari transaksi admin/pelanggan: *platform.HTTPError -> status+pesan,
// *catalog.TierValidationError -> 422 + tierErrors, selain itu 500. Dipakai lintas modul (diberikan
// ke modul lewat Deps.RespondTxError).
func (a *App) respondTxError(w http.ResponseWriter, err error, ctx string) {
	var he *platform.HTTPError
	if errors.As(err, &he) {
		platform.WriteError(w, he.Status, he.Msg)
		return
	}
	var tve *catalog.TierValidationError
	if errors.As(err, &tve) {
		platform.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": tve.Error(), "tierErrors": tve.Errors})
		return
	}
	log.Printf("%s: %v", ctx, err)
	platform.WriteError(w, http.StatusInternalServerError, msgServerError)
}
