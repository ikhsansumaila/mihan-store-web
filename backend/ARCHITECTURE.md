# Arsitektur backend Mihan Store

Modul Go `mihanstore`. Satu paket per modul (bukan per lapisan); antarmuka (port) hanya di batas yang
perlu dites atau ditukar. Semua kode aplikasi ada di `internal/`, sehingga tidak bisa diimpor dari luar modul.

## Peta paket

```
cmd/server/              biner: config -> app.Build -> http.Server -> graceful shutdown; subperintah import-regions
internal/
  app/                   COMPOSITION ROOT: struct App, NewApp/Build + opsi, Start (tugas latar + koneksi DB),
                         routes.go (SATU-SATUNYA daftar rute), adaptor antar-modul (events.go, cartReader,
                         catalogPricing, personOf), respondTxError. Tes integrasi/kontrak/e2e ada di sini.
  platform/              config, db (DBHolder atomik), httputil (JSON, galat, header keamanan, recoverer),
                         ratelimit, clientip, clock, text (CleanText), ids (NewPublicID, PathID), jwks (JWT RS256)
  platform/imaging/      pemrosesan gambar unggahan (sniff, decode aman, orientasi EXIF, encode ulang JPEG),
                         pembacaan multipart, pemetaan galat -> HTTP, Slots (batas pemrosesan paralel)
  audit/                 activity log (Logger.Log menerima tx) + rute admin log + purge
  catalog/               produk, kategori, jenjang harga: pricing.go (murni), tiers_store.go, validate.go,
                         http_public.go, http_admin.go, http_admin_images.go
  catalog/media/         ImageStore (port) + LocalStore, kunci foto, pagar disk (foto produk PUBLIK /uploads)
  identity/              daftar/login/verify/logout/me, sesi, argon2id, lockout, Turnstile, Google (+lengkapi profil),
                         middleware pelanggan (Customer/CustomerFrom), menu admin Pelanggan + alias
  admin/                 middleware /api/admin/* (Cloudflare Access JWT + ADMIN_EMAILS + CSRF), akun admin otomatis,
                         AdminMe, AdminSummary (kueri baca-saja products/categories/orders)
  cart/                  keranjang
  orders/                domain.go (murni: status & transisi, total, nomor pesanan), validate.go, repo.go,
                         http_customer.go, http_admin.go, proofs.go + proofstore.go + proofs_purge.go
                         (bukti transfer PRIVAT, retensi 180 hari), shipping.go (saran & default ongkir kelurahan),
                         service.go (Deps + port)
  regions/               data wilayah: fetch, import, API publik/admin, CLI import
  regions/regionstest/   SATU server tiruan wilayah.id untuk semua tes (tidak ikut biner)
  notify/                webhook Discord
  push/                  Web Push (VAPID) + langganan & rute push untuk audiens admin dan customer (terpisah)
  archtest/              tes pagar arsitektur (aturan impor di bawah)
migrations/              skema SQL (tidak diubah oleh refactor)
testdata/golden/         golden tes kontrak API
```

## Aturan ketergantungan

- Arah: `cmd/server -> internal/app -> modul -> platform`. Hanya `cmd/server` yang mengimpor `internal/app`.
- `domain.go` dan `pricing.go` tidak mengimpor `gorm`, `net/http`, atau paket `mihanstore/*`.
- Modul tidak mengimpor modul lain. Kebutuhan lintas modul = antarmuka/fungsi di SISI PEMAKAI (`Deps`), diisi
  di `internal/app`. Pengecualian yang disengaja (diperiksa `internal/archtest`):
  - `cart -> catalog`: aturan harga murni (pricing.go) dan tipe jenjang.
  - `admin -> identity`: prinsipal admin adalah `identity.User`; akun admin otomatis memakai aturan username/nama.
    `identity` tidak mengimpor `admin` (admin pelaku diberikan lewat `identity.Deps.AdminUser`).
  - `orders -> regions`: hanya tipe data (`regions.DTO`, `CodesInput`, `ComposeAddress`, pesan galat);
    resolusi kode wilayah lewat port `orders.RegionResolver`.
  - `push -> notify`: konstanta jenis kejadian bersama.
- Yang dipakai banyak modul tinggal di `platform` (atau subpaketnya), bukan paket `common`/`utils`.
- Pemeriksa: `go test ./internal/archtest/` (ikut `go test ./...`). `.golangci.yml` memuat aturan depguard yang
  sama untuk golangci-lint v2 (belum terpasang di server).

## Port modul pesanan (internal/orders/service.go)

| Port | Diisi oleh (internal/app) | Fungsi |
|---|---|---|
| `Events` | `orderEvents` (notify + push) | notifikasi setelah commit: pesanan baru, batal (pelanggan/admin), bukti transfer, ongkir ditetapkan, dibayar, selesai |
| `CartReader` | `cartReader` (cart) | `Lock` keranjang dalam transaksi checkout, `Snapshot` keranjang terkini untuk 409 price_changed |
| `Pricing` | `catalogPricing` (catalog) | harga satuan efektif (jenjang grosir), cek `expectedTotal` |
| `RegionResolver` | `*regions.Service` | kode wilayah -> nama dari DB |
| `Slots` | `imaging.Slots` (satu instance bersama foto produk) | batas pemrosesan gambar paralel |
| `Customer`/`Admin`/`NormalizePhone` | identity/admin | pelaku (`orders.Person`) dan normalisasi telepon |

Modul lain memakai pola yang sama: `catalog.Deps.Actor`, `identity.Deps.AdminUser/StatusLabel`,
`push.Deps.AdminID/CustomerID/Sender`, `cart.Deps.Tiers/ImageURLs/UserID`, `regions.Deps.Actor`.

## Invarian yang tidak boleh berubah

Skema & migrasi; bentuk JSON, status HTTP, dan pesan galat (bahasa Indonesia = kontrak dengan frontend & Android);
status pesanan dan transisinya (`pending_confirmation -> pending_payment` hanya lewat `/confirm`); perubahan
admin + log aktivitas dalam SATU transaksi; `FOR UPDATE` pada baris pesanan; kolom generated `alive` dan soft
delete; audiens push `admin` dan `customer` terpisah; bukti transfer privat (di luar `/uploads`); override uji
(JWKS Google/Access, URL wilayah) hanya bila `DB_NAME` berakhiran `_test`; urutan berkas + DB (tulis berkas baru ->
transaksi -> hapus berkas lama).

## Menambah fitur

1. Tentukan modul pemiliknya. Aturan murni ke `domain.go`/berkas murni (tanpa gorm/net/http), SQL ke repo,
   handler tipis memanggil keduanya.
2. Butuh data/kemampuan modul lain? Tambah field fungsi atau antarmuka kecil di `Deps` modul pemakai, lalu isi di
   `internal/app/app.go` (adaptor di `internal/app` bila perlu). Jangan mengimpor modul lain.
3. Daftarkan rute di `internal/app/routes.go` (rute admin otomatis di balik `admin.RequireAdmin`; rute pelanggan
   dibungkus `identity.Customer`). Tambah skenario di tes kontrak bila rute baru.
4. Limiter baru: field ekspor di Service modul, didaftarkan di `App.limiters()` (dipakai `WithRateLimit` tes).
5. Paket baru: daftarkan di `internal/archtest/imports_test.go` beserta alasan impornya.

## Menjalankan tes

```
cd ~/mihanstore/backend
G="docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
   -v $HOME/.cache/mihanstore-gocache:/tmp/gocache -v mihanstore-gomod:/go/pkg/mod -v $PWD:/src -w /src golang:1.27-alpine"
$G sh -c 'gofmt -l . && go build ./... && go vet ./... && go test -count=1 ./...'   # unit + archtest
scripts/integration.sh -v          # integrasi (DB uji sementara mihanstore_test)
scripts/contract.sh                # kontrak API vs testdata/golden (harus identik)
UPDATE_GOLDEN=1 scripts/contract.sh  # rekam ulang golden (HANYA untuk perubahan kontrak yang disengaja)
scripts/e2e.sh                     # e2e: image uji sementara dari Dockerfile + DB uji, lalu dihapus
```

Tes integrasi/kontrak merakit aplikasi lewat `app.Build(cfg, opts...)` (lihat `internal/app/build.go`:
`WithDB`, `WithClock`, `WithNotifier`, `WithPusher`, `WithAccessVerifier`, `WithGoogleVerifier`,
`WithTurnstileEndpoint`, `WithRateLimit`, `WithImageStore`, `WithProofStore`, `WithRegionFetch`).
Semua skrip membuat dan menghapus database uji sendiri; tidak menyentuh database produksi.
