# Baseline refactor clean architecture (Tahap 0)

Tanggal: 2026-10-09. Cabang kerja: `refactor/clean-arch`. Tag titik awal: `pre-refactor` =
`0a19b8758c5c7a962733fc84d560e384750472b9` (HEAD `main` saat refactor dimulai).
Panduan: `panduan-refactor-mihanstore-v2.md` (bagian 0, 1, 2, 7, 9).

Aturan: refactor tidak boleh mengubah perilaku. Setelah SETIAP langkah jalankan semua tes di bawah
dan bandingkan dengan angka ini. **Jumlah tes tidak boleh turun** (boleh bertambah; tes yang pindah
paket tetap harus dihitung).

## 1. Baseline tes (pada `pre-refactor`)

Jumlah = tes tingkat atas (`--- PASS/SKIP/FAIL`), semua paket (`mihanstore`, `notify`, `push`).

| Jenis | Lulus | Skip | Gagal | Catatan |
|---|---|---|---|---|
| Unit (tanpa tag) | 117 | 1 | 0 | skip: `TestLocalStoreReadOnlyDir` (hanya skip bila dijalankan sebagai root) |
| Integrasi (`-tags integration`, termasuk unit) | 164 | 2 | 0 | skip: `TestLocalStoreReadOnlyDir` (root) dan `TestIntegrationTierSeenNullAndOldOrder` (fixture pesanan lama tidak ada di DB baru). Lewat `scripts/integration.sh` (user biasa, bukan root) hasilnya 165 lulus / 1 skip |
| E2E (`-tags e2e`, backend saja, tanpa frontend) | 7 | 2 | 0 | pada `pre-refactor` asli 6/2/1; asersi `order.pricing_update` diganti `order.confirm` (lihat catatan) |
| Kontrak (`-tags contract`) | 1 tes (`TestContract`, 12 bagian) | 0 | 0 | 448 skenario, 72 rute, lihat bagian 3 |

Frontend (opsional, `cd frontend && CI=true npx react-scripts test --watchAll=false`): 24 suite, 358 tes lulus.

`go build ./...`, `go vet ./...`, serta `go vet -tags integration|e2e|contract ./...` bersih; `gofmt -l .` kosong.

Catatan e2e (sudah gagal SEBELUM refactor, bukan akibat Tahap 0):
- `TestE2EOrderFlow` gagal di pemeriksaan terakhir `e2e_orders_test.go:259`: menuntut aksi log
  `order.pricing_update`, padahal sejak alur "Menunggu konfirmasi" (commit `0aea1ea`) harga ditetapkan lewat
  `/confirm` (log `order.confirm`); `PATCH /pricing` baru boleh setelah konfirmasi dan tidak dipanggil oleh e2e.
  Seluruh alur sebelum pemeriksaan itu lulus. Perbaikan usulan (terpisah dari refactor): ganti
  `order.pricing_update` dengan `order.confirm` di daftar itu.
- Skip: `TestE2EProductImages` (butuh frontend; `E2E_FRONTEND_URL` kosong) dan `TestE2EServeMockOnly`
  (hanya untuk CLI import-regions).
- Perbaikan sudah diterapkan di working tree (2026-10-09) dan diverifikasi: e2e 7 lulus / 2 skip / 0 gagal.

## 2. Menjalankan tes

Semua skrip ada di `backend/scripts/`, membuat database uji SEMENTARA di MySQL bersama (container `mysql_db`,
user uji dibatasi 15 koneksi, hak hanya SELECT/INSERT/UPDATE/DELETE seperti produksi) dan SELALU menghapusnya
di akhir. Tidak menyentuh database `mihanstore`, tidak memakai `docker compose`.

```
cd ~/mihanstore/backend
docker run --rm -v mihanstore-gomod:/go/pkg/mod -v $PWD:/src -w /src golang:1.27-alpine \
  sh -c 'go build ./... && go vet ./... && go test -count=1 ./...'   # unit + build + vet (tanpa DB)
scripts/integration.sh -v        # integrasi (DB uji mihanstore_test)
scripts/e2e.sh                   # e2e backend saja (build image uji sementara, dihapus lagi)
scripts/contract.sh              # kontrak API: bandingkan dengan testdata/golden/
UPDATE_GOLDEN=1 scripts/contract.sh   # REKAM ulang golden (hanya bila perubahan kontrak disengaja)
```

`scripts/lib_testdb.sh` memuat pembuat/penghapus database uji (dipakai ketiga skrip). Cache build Go ada di
`~/.cache/mihanstore-gocache`. Pastikan server MySQL tidak kehabisan koneksi (batas 60; produksi memakai
sebagian): jangan menjalankan banyak skrip bersamaan.

## 3. Kontrak API (golden)

`contract_test.go` + `contract_scenarios_test.go` (build tag `contract`) merakit server di dalam proses
(`NewApp` + `newRouter`) lalu memanggil rute dengan: permintaan sah, tanpa login (401/403), input tidak
valid (400/409/422), dan payload tak lazim (nama skenario berawalan `unusual`). Hasilnya (status, SEMUA header
respons kecuali `Date`/`Content-Length`, badan JSON) dibandingkan dengan `testdata/golden/*.json` setelah
menormalkan nilai berubah-ubah: waktu, UUID, token/JWT/kunci panjang, nomor pesanan (tanggalnya), nama
berkas acak, username otomatis admin, port server tiruan, `durationMs`. Nilai yang dinormalkan diberi nomor
urut (`<TOKEN#1>`) supaya hubungan antar nilai tetap terlihat. Badan non-JSON (bukti transfer) dicatat
sebagai panjang + SHA-256.

Dependensi luar yang ditiru: Turnstile (server lokal), JWKS Google & Cloudflare Access (kunci uji), notifier
Discord & pusher Web Push (perekam, tidak ada pengiriman nyata), jam (melangkah 1 detik per pemanggilan),
folder foto & bukti transfer (folder sementara), sumber data wilayah (server tiruan).
Database HARUS baru (id auto-increment ikut kontrak); `scripts/contract.sh` memastikannya.

Berkas golden: `00_coverage.json` (rute -> kode status teramati), `01_public` ... `12_regions_admin`
(masing-masing berisi daftar skenario), `13_notification_events.json` (kejadian Discord/push yang dipicu
alur, diurutkan).

Bila pembanding gagal, keluarannya menampilkan nama skenario dan selisih baris (`-` golden, `+` sekarang).
Selisih yang disengaja (mis. perbaikan galat) direkam ulang dengan `UPDATE_GOLDEN=1` di commit tersendiri
beserta alasannya. Selisih yang tidak disengaja = regresi refactor.

Bukti determinisme (2026-10-09): tiga perekaman berurutan menghasilkan berkas identik byte-per-byte
(`diff -r` kosong) dan pembanding lulus pada kode `pre-refactor`.

### Rute yang tercakup (72, sesuai bagian 7 panduan)

- Publik: `GET /health`, `/health/ready`, `/api/products` (+ `?category=`), `/api/products/search`,
  `/api/categories`, `/api/regions/{provinces, regencies/{kode}, districts/{kode}, villages/{kode}}`
  (+ CORS preflight, origin tidak diizinkan, rute tak dikenal, metode salah).
- Auth: `POST /api/auth/{register, login, verify, logout, google, google/complete}`, `GET /api/auth/me`
  (Google memakai token yang ditandatangani kunci uji).
- Pelanggan: `/api/cart` (GET, DELETE), `/api/cart/items` (POST, PUT), `/api/cart/items/{id}` (DELETE),
  `POST /api/cart/ack-prices`; `/api/orders` (GET, POST), `/api/orders/{no}` (GET), `.../cancel`,
  `.../payment-proof` (POST, GET, DELETE); `GET /api/store-info`; `/api/push/public-key`,
  `/api/push/subscribe` (POST, DELETE).
- Admin (semua rute `/api/admin/...` juga diuji tanpa login dan dengan email bukan admin, plus CSRF/origin):
  `me`, `summary`, `settings` (GET, PUT); `products` (+ `{id}`, `active`, `image` POST/DELETE); `categories`
  (+ `{id}`); `orders` (+ `{id}`, `pricing`, `confirm`, `shipping-suggestions`, `shipping-default`,
  `payment-proof`, `status`, `note`); `customers` (+ `{id}`, `alias`); `activity-logs` (+ `purge`);
  `push/{public-key, subscribe, test}`; `regions/{status, fetch, runs/{id}, runs/{id}/save, runs/{id}/discard}`.
- Alur pesanan lengkap: checkout -> `pending_confirmation`; `/confirm` (diskon + ongkir, dan ongkir 0);
  saran ongkir; default ongkir kelurahan; unggah/ganti/hapus bukti transfer (gambar uji dibuat di kode);
  Dibayar (bukti terkunci setelahnya); Selesai; batal dari kedua status menunggu (pelanggan dan admin);
  batal dari Dibayar (alasan wajib); IDOR pesanan orang lain (404 identik); idempotency checkout;
  `expectedTotal` tidak cocok (409); harga grosir di keranjang; audiens push `admin` vs `customer`
  (endpoint yang sama, berhenti langganan tidak saling menghapus).

### Sengaja BELUM tercakup

- Unggah gambar besar / batas ukuran / gambar rusak yang memakan memori (sudah dicakup tes integrasi
  `integration_images_test.go`, `integration_proof_test.go`); golden hanya memakai gambar kecil.
- Login Google nyata (JWKS Google asli), Cloudflare Access nyata (JWT asli), Turnstile nyata,
  webhook Discord nyata, pengiriman Web Push nyata, `wilayah.id` asli.
- Batas laju (429) dan penguncian akun: limiter dilonggarkan di tes kontrak; dicakup tes unit/integrasi.
- Goroutine latar: purge bukti transfer 180 hari, sweeper run wilayah saat start, shutdown.
- Respons gagal DB (503/500), readiness saat DB putus, race/konkurensi (`FOR UPDATE`, klik ganda).
- Berkas statis `/uploads` (disajikan nginx frontend, bukan backend), CLI `import-regions`.
- Frontend (React) dan aplikasi Android.
- Data produksi (hitungan pesanan per status, `/api/products` identik byte-per-byte, 38 provinsi): diperiksa
  manual sebelum/sesudah deploy sesuai bagian 2.4 dan 9 panduan.

## 4. Kendala merakit server di tes (memengaruhi urutan refactor)

- Seluruh dependensi ada di `App` (`NewApp(cfg)` + isi manual): untuk tes harus menimpa langsung field privat
  (`app.turnstile.endpoint`, `app.access`, `app.google`, `app.notifier`, `app.pusher`, `app.now`, `app.proofs`,
  14 rate limiter satu per satu, `app.regionFetchCfg`, `app.regionCooldown`, `app.db.Store(db)`,
  `app.setImageStore(...)`). Daftar field itu adalah kebutuhan opsi `app.Build(cfg, opts...)`.
- Database dipasang sesudah konstruksi lewat `atomic.Pointer` (`app.db.Store`); cache wilayah harus
  di-`invalidate()` setelah data di-seed.
- `newRouter` membungkus handler tanpa mengekspos router mux, sehingga daftar rute tidak bisa diambil
  otomatis; tabel rute di `contract_test.go` (`buildRoutes`) ditulis manual dan harus diperbarui bila rute berubah.
- Verifier Access/Google dibuat di `NewApp` dari `cfg` (JWKS jaringan); tes mengganti `.jwks` secara langsung.
- Tes kontrak mandiri (hanya bergantung pada helper tes tanpa tag: `signJWT`, `stubJWKS`, `mockRegionTree`);
  helper integrasi (`setupOrders` dll.) tidak dipakai karena bertag `integration`. Saat tes berpindah paket,
  `newContractEnv` (perakitan) adalah satu-satunya bagian yang perlu disesuaikan ke `app.Build`.
- `admin_unit_test.go` membuat kunci RSA di `init()` dan `e2e_test.go` memakai `TestMain`; keduanya berada di
  paket `main` bersama tes lain, jadi pemindahan tes ke paket modul harus memperhatikan helper bersama itu.
- E2E membutuhkan image backend uji (dibangun dari `backend/Dockerfile`); setelah struktur berubah
  (`cmd/server`), Dockerfile harus diperbarui lebih dulu agar `scripts/e2e.sh` tetap bisa membangun image.

## 5. Daftar periksa tiap langkah refactor

1. `go build ./... && go vet ./...` bersih.
2. Unit >= 117 lulus; integrasi >= 164 lulus (skip tidak bertambah); e2e >= 7 lulus.
3. `scripts/contract.sh` hijau tanpa merekam ulang golden.
4. Satu commit kecil per langkah; jangan campur dengan fitur/perbaikan bug.
