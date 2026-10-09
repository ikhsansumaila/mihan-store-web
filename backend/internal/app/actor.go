package app

import (
	"net/http"

	"mihanstore/internal/admin"
	"mihanstore/internal/identity"
	"mihanstore/internal/regions"
)

// Pelaku admin untuk rute impor wilayah (UID/ActorOf dari internal/identity).

// adminRegionActor: pelaku admin untuk rute impor wilayah (internal/regions).
func (a *App) adminRegionActor(r *http.Request) regions.Actor {
	u := admin.From(r.Context())
	return regions.Actor{UserID: identity.UID(u), Label: identity.ActorOf(u), IP: a.ips.ClientIP(r), UA: r.UserAgent()}
}
