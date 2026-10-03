# MihanStore - Simple Online Store

MihanStore is a simple online store application built with React (Frontend) and Go (Backend), deployed using Docker Compose.

## Features
- Katalog produk & kategori di database MySQL (20 produk awal dipindahkan dari kode)
- Pencarian produk
- Registrasi & login pelanggan: email/username + password (argon2id, Turnstile) atau "Masuk dengan Google"
- Menu admin `/admin` (produk, kategori, invoice, log aktivitas) yang dijaga Cloudflare Access
- Log aktivitas (append-only) dengan penghapusan manual log > 6 bulan
- Halaman Kebijakan Privasi (`/privasi`) dan Syarat & Ketentuan (`/syarat`)
- **Pesanan (Tahap 1)**: keranjang di database, checkout (wajib login), "Pesanan Saya", menu admin
  "Pesanan" dan "Pengaturan Toko", diskon & ongkir manual, invoice PDF dari pesanan, notifikasi Discord,
  tombol konfirmasi WhatsApp (lihat bagian [Pesanan](#pesanan-tahap-1))
- **Pelanggan & alias** (admin): daftar/detail pelanggan, alias internal hanya untuk admin, toggle nama alias di invoice
  (lihat bagian [Pelanggan & alias](#pelanggan--alias-admin))

## Tech Stack
- **Frontend:** React.js, Axios, React Router DOM
- **Backend:** Go 1.26+ (image golang:1.27-alpine), Gorilla Mux, Gorilla Handlers, GORM + MySQL 8.4, golang.org/x/crypto/argon2
- **Deployment:** Docker, Docker Compose, Nginx

## Directory Structure
```
mihanstore/
├── backend/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── frontend/
│   ├── Dockerfile
│   ├── nginx.conf
│   ├── package.json
│   ├── public/
│   │   ├── index.html
│   │   └── styles.css
│   ├── src/
│   │   ├── App.js
│   │   ├── index.js
│   │   ├── index.css
│   │   └── components/
│   │       ├── Login.js
│   │       └── Register.js
├── docker-compose.yml
├── .env.example
├── .gitignore
└── README.md
```

## Autentikasi Pengguna (MySQL)

Data disimpan di database `mihanstore` pada container `mysql_db`
(proyek `~/mysql-stack`, jaringan Docker internal `mysql-net`). Backend terhubung
sebagai user `mihanstore_app` (hanya dari subnet `mysql-net`) dengan hak **per tabel**
(lihat `backend/migrations/005_grants_per_table.sql`):

| Tabel / objek | Hak `mihanstore_app` |
|---|---|
| `users` | SELECT, INSERT, UPDATE (hapus = soft delete) |
| `sessions` | SELECT, INSERT, UPDATE, DELETE (bersih-bersih sesi kedaluwarsa) |
| `categories`, `products` | SELECT, INSERT, UPDATE (soft delete) |
| `activity_logs` | **SELECT, INSERT saja** (append-only) |
| `carts`, `orders`, `site_settings` | SELECT, INSERT, UPDATE (tanpa DELETE) |
| `cart_items` | SELECT, INSERT, UPDATE, DELETE (baris keranjang boleh dihapus) |
| `order_items`, `order_status_history` | **SELECT, INSERT saja** (snapshot/append-only) |
| `product_price_tiers` | SELECT, INSERT, UPDATE (hapus jenjang = soft delete) |
| `region_datasets`, `region_import_runs` | SELECT, INSERT, UPDATE (wilayah hilang = soft delete) |
| `region_import_staging` | SELECT, INSERT, DELETE (staging dihapus setelah Simpan/Batal) |
| procedure `purge_activity_logs` | EXECUTE (satu-satunya cara menghapus log, hanya > 180 hari) |

Skema dibuat oleh root lewat file migrasi, bukan oleh aplikasi (tanpa AutoMigrate).

### Migrasi (urut, dijalankan sebagai root)
| File | Isi | Rollback |
|---|---|---|
| `001_users_sessions.sql` | users + sessions | — |
| `002_users_google.sql` | `google_sub`, `avatar_url`, `password_hash` boleh NULL | komentar di file |
| `003_categories_products.sql` | tabel kategori & produk + seed 6 kategori, 20 produk (id 1–20) | `DROP TABLE products, categories` |
| `004_activity_logs.sql` | tabel log + procedure `purge_activity_logs` (DEFINER root) | komentar di file |
| `005_grants_per_table.sql` | ganti hak `mihanstore.*` dengan hak per tabel | komentar di file |
| `006_carts.sql` | `carts`, `cart_items` | `DROP TABLE cart_items, carts` |
| `007_orders.sql` | `orders`, `order_items`, `order_status_history` | komentar di file |
| `008_site_settings.sql` | `site_settings` + 5 kunci placeholder "BELUM DIISI" | `DROP TABLE site_settings` |
| `009_grants_orders.sql` | hak per tabel untuk tabel pesanan | komentar di file |
| `010_products_unit_price_tiers.sql` | `products.unit` (default `pcs`) + tabel `product_price_tiers` | komentar di file (aman dibiarkan) |
| `011_cart_order_price_snapshot.sql` | `cart_items.seen_unit_price`; `order_items.base_unit_price`, `tier_min_qty`, `unit` | komentar di file (aman dibiarkan) |
| `012_grants_price_tiers.sql` | `product_price_tiers`: SELECT, INSERT, UPDATE | komentar di file |
| `013_regions.sql` | `region_datasets`, `region_import_runs`, `region_import_staging` (data wilayah JSON) | komentar di file (aman dibiarkan) |
| `014_orders_region.sql` | `orders.province_code/name`, `regency_code/name`, `district_code/name`, `village_code/name` (NULL) | komentar di file (aman dibiarkan) |
| `015_grants_regions.sql` | `region_datasets`, `region_import_runs`: SELECT, INSERT, UPDATE; `region_import_staging`: SELECT, INSERT, DELETE | komentar di file |
| `016_users_alias.sql` | `users.alias VARCHAR(100) NULL` (alias pelanggan, khusus admin; tanpa perubahan hak) | komentar di file (aman dibiarkan) |

```bash
cd ~/mihanstore
for f in 002_users_google 003_categories_products 004_activity_logs; do
  docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/$f.sql
done
docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/005_grants_per_table.sql
```
Jalankan `~/mysql-stack/dump.sh` sebelum migrasi sebagai cadangan.

### Konfigurasi (`.env`, tidak di-commit)
Salin `.env.example` ke `.env` (`chmod 600 .env`) lalu isi `DB_PASSWORD`,
`TURNSTILE_SITE_KEY`, dan `TURNSTILE_SECRET_KEY`.
Tanpa `TURNSTILE_SECRET_KEY`, login dan registrasi **ditolak** (fail-closed, HTTP 503).
`ALLOW_NO_TURNSTILE=true` hanya untuk pengembangan lokal, jangan di produksi.
Site key Turnstile ditanam saat build frontend, jadi setelah mengubahnya jalankan ulang
`docker compose up -d --build frontend`; setelah mengubah secret cukup
`docker compose up -d backend`.

### Membuat database, user, dan skema (sekali saja, sudah dilakukan)
```bash
# Database + user (password dari .env, dijalankan sebagai root di dalam mysql_db)
PW=$(grep '^DB_PASSWORD=' .env | cut -d= -f2-)
printf "CREATE DATABASE IF NOT EXISTS mihanstore CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'mihanstore_app'@'172.20.0.0/255.255.0.0' IDENTIFIED BY '%s';
GRANT SELECT, INSERT, UPDATE, DELETE ON mihanstore.* TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';\n" "$PW" \
  | docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot'
# Skema
docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' \
  < backend/migrations/001_users_sessions.sql
```
Lanjutkan dengan migrasi 002–005 (bagian "Migrasi" di atas); 005 mengganti hak `mihanstore.*` di atas dengan hak per tabel.

### Endpoint
| Method | Path | Keterangan |
|---|---|---|
| POST | `/api/auth/register` | `{username, email, phone?, name, password, turnstileToken}` → 201 `{success, user, token, expiresAt}` |
| POST | `/api/auth/login` | `{identifier, password, turnstileToken}` (identifier = email bila ada `@`, selain itu username; field lama `email` juga diterima) |
| POST | `/api/auth/verify` | token via `Authorization: Bearer <token>` atau body `{token}` → 200 `{valid:true,user}` / 401 `{valid:false}` |
| POST | `/api/auth/logout` | mencabut sesi (idempoten) |
| GET  | `/api/auth/me` | data akun (termasuk `role`, `avatarUrl`, `googleLinked`) |
| POST | `/api/auth/google` | `{credential}` (ID token Google) → sesi, atau `{needsProfile:true, profileToken, profile}` |
| POST | `/api/auth/google/complete` | `{profileToken, username, phone?}` → 201 sesi (akun baru tanpa password) |
| GET  | `/api/products`, `/api/products/search?q=&category=` | publik, dari DB (hanya produk aktif), field lama sama seperti dulu + `unit`, `tiers` (lihat Harga grosir) |
| GET  | `/api/categories` | publik: `[{id, slug, name, sortOrder}]` |
| GET  | `/api/regions/provinces`, `/regencies/{kode}`, `/districts/{kode}`, `/villages/{kode}` | publik, data wilayah dari DB (lihat Data wilayah) |
| *    | `/api/admin/*` | admin (Cloudflare Access), lihat bagian Admin |
| GET  | `/health` | liveness; `/health/ready` memeriksa database |

### Login Google (pelanggan)
- Tombol "Masuk dengan Google" (Google Identity Services) tampil di Login & Daftar hanya bila
  `REACT_APP_GOOGLE_CLIENT_ID` diisi saat build. Backend memverifikasi ID token (RS256, JWKS
  Google dengan cache mengikuti `Cache-Control`, `iss`, `aud` = `GOOGLE_CLIENT_ID`, `exp`,
  `email_verified`). Tanpa `GOOGLE_CLIENT_ID`/`AUTH_HMAC_SECRET` → 503 "Login Google belum dikonfigurasi".
- Login Google pertama → layar "Lengkapi profil" (username wajib, telepon opsional).
  `profileToken` ditandatangani HMAC (`AUTH_HMAC_SECRET`), berlaku 10 menit.
- Aturan penyambungan akun:
  1. Email Google sudah punya akun password → akun disambungkan ke Google, **password lama dihapus**
     dan **semua sesi lama dicabut** (log `auth.google_link`). Selanjutnya hanya login Google.
  2. Daftar dengan password memakai email akun Google → 409 "Email sudah terdaftar, silakan masuk dengan Google".
  3. Akun tanpa password yang mencoba login password → gagal dengan pesan umum.
  4. Email yang ada di `ADMIN_EMAILS` otomatis mendapat role `admin` (log `user.role_change`).

### Admin (Cloudflare Access)
- Rute `/admin` (frontend) dan `/api/admin/*` (backend) dilindungi Cloudflare Access.
  Backend memverifikasi header `Cf-Access-Jwt-Assertion` (RS256, JWKS
  `https://<CF_ACCESS_TEAM_DOMAIN>/cdn-cgi/access/certs`, `aud` = `CF_ACCESS_AUD_STORE`, `iss`,
  `exp`, `nbf`) dan email harus ada di `ADMIN_EMAILS`. **Fail-closed**: bila salah satu dari
  `CF_ACCESS_TEAM_DOMAIN`, `CF_ACCESS_AUD_STORE`, `ADMIN_EMAILS` kosong → 503 "Akses admin belum dikonfigurasi".
- Admin tidak memakai password. Baris `users` untuk email admin dibuat otomatis (role admin,
  tanpa password) atau dinaikkan ke admin bila sudah ada. Role & status dibaca ulang dari DB di
  setiap permintaan: akun `suspended` atau bukan admin → 403 (dicatat `admin.access_denied`,
  maks. 1 catatan / 5 menit per IP+email+alasan).
- Permintaan yang mengubah data wajib `Content-Type: application/json`, header
  `X-Requested-With: mihanstore-admin`, dan `Origin`/`Sec-Fetch-Site` sama-origin (CSRF).
- Endpoint: `GET /api/admin/me`, `GET /api/admin/summary`, produk (`GET/POST /products`,
  `GET/PUT/DELETE /products/{id}`, `PATCH /products/{id}/active`), kategori
  (`GET/POST /categories`, `PUT/DELETE /categories/{id}`; hapus ditolak bila masih ada produk aktif),
  log (`GET /activity-logs?action=&actor=&entity_type=&from=YYYY-MM-DD&to=YYYY-MM-DD&page=&per_page=`,
  `POST /activity-logs/purge`). Hapus produk/kategori = soft delete.
- Tautan ke `/admin` harus anchor biasa (`<a href="/admin">`) agar Cloudflare Access mencegat navigasi.
- Rute admin frontend: `/admin` (Dashboard), `/admin/products` (Produk), `/admin/categories`
  (Kategori), `/admin/orders` (+ `/admin/orders/<id>`), `/admin/customers` (+ `/admin/customers/<id>`, Pelanggan), `/admin/invoice` (Buat Invoice),
  `/admin/pricelist` (Pricelist), `/admin/settings` (Pengaturan Toko), `/admin/activity` (Log aktivitas).
- `/admin/orders/<id>` juga menerima **nomor pesanan** (`/admin/orders/MS-261002-0001`, tidak peka huruf): dicari
  lewat `GET /api/admin/orders?q=<nomor>&per_page=100`, dicocokkan persis (pencarian backend memakai LIKE), lalu URL
  diganti (replace) ke `/admin/orders/<id>`; bila tidak ada: "Pesanan <nomor> tidak ditemukan" + tautan ke daftar.
  Dipakai tautan "Buka di admin: https://store.mihan.web.id/admin/orders/<nomor>" di teks WhatsApp konfirmasi
  pelanggan → toko (`buildCustomerConfirmText`, origin dari `window.location.origin`). Tetap dijaga Cloudflare Access.
- **Tampilan admin (gaya cPanel)** — `src/admin/AdminLayout.js`: area `/admin*` tidak memakai navbar/footer
  toko. Sidebar gelap kiri (260px) berisi logo + "Mihan Store Admin", kolom *Cari menu* (Enter membuka hasil
  pertama), dan grup menu **Utama** (Dashboard), **Katalog** (Produk, Kategori), **Penjualan** (Pesanan dengan
  lencana jumlah *menunggu pembayaran* dari `/api/admin/summary`, Pelanggan, Invoice, Pricelist), **Sistem** (Pengaturan Toko, Log
  Aktivitas); bagian bawah: email admin (`/api/admin/me`), tautan anchor biasa "Kembali ke toko" (`/`), versi UI.
  Topbar: judul + breadcrumb (mis. Admin / Katalog / Produk) dan nama admin singkat. Konten mengisi seluruh
  sisa lebar layar di kanan sidebar (tanpa `max-width`); tabel lebar di-scroll di dalam wadahnya
  (`overflow-x-auto`), halaman tidak pernah scroll horizontal. Di bawah 1024px sidebar menjadi **bilah ikon**
  sempit (64px, selalu terlihat, tanpa hamburger/overlay): logo kecil, ikon menu (target sentuh 44px, tooltip
  `title` + `aria-label`, menu aktif bergaris kiri + latar, `aria-current="page"`), angka kecil *menunggu
  pembayaran* di pojok ikon Pesanan, ikon "Kembali ke toko" di bawah; label teks hanya untuk pembaca layar
  (`sr-only`), kolom cari menu dan email admin disembunyikan (topbar menampilkan inisial). Breadcrumb di layar
  sempit diringkas menjadi dua tingkat terakhir. Dashboard (`src/admin/Dashboard.js`): kartu statistik, pintasan cepat
  (`/admin/products?tambah=1` membuka form tambah produk, `/admin/orders?status=pending_payment` memfilter
  pesanan), dan 10 aktivitas terakhir. Navigasi antar halaman admin memakai react-router (sudah di dalam area
  Cloudflare Access).
- **Buat Invoice** (`/admin/invoice`): PDF dibuat di browser dengan jsPDF (logo
  `/mihan-store-logo.png`, `/lunas-logo.png`), tanpa API backend. Rute lama `/invoice/create`
  tidak lagi tampil di toko; halaman itu hanya mengalihkan dengan muat ulang penuh
  (`window.location.replace('/admin/invoice')`) agar dicegat Cloudflare Access. Catatan: kode
  komponen invoice tetap ada di bundel JavaScript publik; yang dijaga adalah akses ke halamannya.
- **Pricelist** (`/admin/pricelist`, grup Penjualan; pintasan "Buat Pricelist" di Dashboard): membuat gambar PNG
  daftar harga di browser dengan Canvas 2D (tanpa dependensi/font/CDN baru, tanpa perubahan backend). Data dari
  `GET /api/admin/products` (semua halaman, `per_page=100`) dan `/api/admin/categories` (pengaturan toko
  `store_whatsapp` tidak dipakai Pricelist). Pengaturan: judul (default "Daftar Harga Mihan Store"), keterangan tanggal (default
  "Berlaku per <tanggal hari ini>"), catatan opsional ≤200 karakter, tema Ungu/Hijau/Biru, 1 atau 2 kolom, **Alamat web pemesanan** (default
  `window.location.origin`, di produksi `https://store.mihan.web.id`; hanya http/https tanpa spasi, tanpa skema
  dianggap https; bila tidak valid tombol unduh/bagikan dinonaktifkan), centang
  kategori/produk (default semua produk aktif; produk nonaktif hanya ikut bila dicentang manual dan diberi tanda
  + peringatan). Urutan: kategori menurut `sortOrder` lalu nama, produk menurut nama. Gambar lebar 1080 px, tinggi
  mengikuti isi (maks 2400 px per gambar, skala 1:1); kategori tidak dipotong bila muat di satu gambar (pindah ke
  gambar berikutnya), kategori yang lebih panjang dari satu gambar dilanjutkan dengan judul "(lanjutan)". Header
  memakai logo `/mihan-store-logo.png` (same-origin), footer "Pesan online: store.mihan.web.id" (alamat web tanpa
  skema; tanpa nomor WhatsApp), nama toko, dan "Halaman n/N". Tombol: **Unduh PNG** (semua gambar berurutan,
  `pricelist-mihan-store-YYYYMMDD-<n>.png`; ada juga tombol unduh per halaman) dan **Bagikan ke WhatsApp**: di HP
  memakai Web Share API (`navigator.share({files, text})`, gambar langsung terlampir); di komputer gambar diunduh lalu
  `https://wa.me/?text=…` dibuka di tab baru, dan file PNG harus dilampirkan manual (WhatsApp Web tidak menerima gambar
  dari tautan; `wa.me` tanpa nomor tujuan). Teks pendamping default berakhir "Pesan online di https://store.mihan.web.id"
  (tanpa nomor WA), bisa diedit dan disalin. Kode: logika murni `src/pricelist/layout.js` (diuji di
  `src/__tests__/pricelistLayout.test.js`), penggambaran `src/pricelist/render.js`, unduh/berbagi
  `src/pricelist/share.js`, halaman `src/admin/Pricelist.js`.
- **Tampilan HP (semua halaman, toko dan admin)** — `src/index.css`, satu media query `max-width: 639.98px` (di bawah
  breakpoint `sm`): ukuran dasar `html` 14px (87,5%) sehingga semua ukuran rem Tailwind (huruf, padding, gap) menyusut
  12,5%; batas bawah: `text-sm` 13px, `text-xs` 12px; input/select/textarea tetap 16px (cegah zoom otomatis iOS Safari);
  tombol min. 40×40px, tautan bergaya tombol min. 40px; bilah ikon admin tetap 64px dan tiap tombol menu 44px. Aturan
  kelas diberi awalan `:root` karena Tailwind CDN menyisipkan stylenya setelah CSS aplikasi. Layar ≥640px tidak
  berubah; kanvas PNG Pricelist tidak terpengaruh. Diuji di `src/__tests__/mobileStyles.test.js`.
- **Daftar produk toko di HP (2 kolom)** — `src/App.js` (`Home`, `ProductCard`, `ProductImage`, `TierBadge`,
  `AddToCartButton`). Di bawah 640px saja (kelas `max-sm:`; kelas tanpa awalan = tampilan ≥640px yang tidak berubah,
  terverifikasi identik per piksel di 640/768/1280px): grid `grid-cols-2` jarak `gap-2`; kartu `p-2` setinggi baris
  (tombol di dasar); area gambar rasio 4/3 (placeholder 🖼️ bila tidak ada/gagal dimuat — kolom `image` saat ini hanya
  nama berkas tanpa berkas yang disajikan, jadi `<img>` hanya dipakai untuk URL http(s) atau path berawalan `/`);
  nama 13px maks. 2 baris, kategori 11px 1 baris, harga 15px tebal + satuan 11px, deskripsi 11px maks. 2 baris;
  lencana Grosir 11px selebar kartu ("Grosir · mulai Rp …", daftar jenjang tetap bisa dibuka); tombol: ikon keranjang
  (SVG) di kiri + "Tambah" di kanan, min. 40px, `aria-label` "Tambah <nama> ke keranjang" (teks lama hanya
  disembunyikan visual). Diuji di `src/__tests__/productListing.test.js`.
- **Tidak perlu promosi manual**: cukup masukkan email ke `ADMIN_EMAILS`. Untuk mencabut akses,
  hapus email dari `ADMIN_EMAILS` (dan dari policy Access) lalu `docker compose up -d backend`;
  untuk memblokir segera: `UPDATE users SET status="suspended" WHERE email="..."`.

### Log aktivitas
Tabel `activity_logs` (tanpa FK, IP + user agent). Perubahan admin dicatat dalam transaksi yang
sama dengan perubahannya; log autentikasi best-effort. `details` tidak pernah memuat password,
hash, token, cookie/Authorization, atau id_token. Log disimpan 6 bulan; hapus dengan tombol
"Hapus log lebih dari 6 bulan" di `/admin/activity` (procedure `purge_activity_logs`).

Keamanan: password argon2id (m=19456 KiB, t=2, p=1), token sesi acak 32 byte yang
disimpan hanya sebagai SHA-256, sesi 7 hari (`SESSION_TTL_HOURS`), akun terkunci
15 menit setelah 5 kali gagal, batas laju per IP (login 10/menit, registrasi 5/jam),
Turnstile wajib untuk login dan registrasi, CORS hanya untuk `CORS_ALLOWED_ORIGINS`.

### Membuka kunci akun
`UPDATE users SET failed_logins=0, locked_until=NULL WHERE username="...";` (sebagai root).

### Tes
```bash
# Unit test
docker run --rm -v "$PWD/backend":/src -w /src golang:1.27-alpine go test ./...
# Tes integrasi: butuh database *_test BARU yang dibuat dari migrasi 001–016 (+ hak 005, 009, 012 & 015 untuk user uji)
# (user uji tidak punya hak DELETE pada users/activity_logs, jadi DB dibuat ulang tiap putaran)
docker run --rm --network mysql-net -e DB_NAME=mihanstore_test -e DB_USER=... -e DB_PASSWORD=... \
  -v "$PWD/backend":/src -w /src golang:1.27-alpine go test -tags integration ./...
# Tes end-to-end (tag e2e): proses tes menyajikan JWKS tiruan di :9000; container backend UJI
# diarahkan ke sana dengan TEST_ONLY_CF_ACCESS_JWKS_URL / TEST_ONLY_GOOGLE_JWKS_URL.
# Variabel TEST_ONLY_* hanya berlaku bila DB_NAME berakhiran _test dan TIDAK BOLEH diset di produksi.
# Webhook Discord uji diarahkan ke server tiruan di proses tes (http://<runner>:9000/discord/api/webhooks/...);
# URL http/host selain discord.com hanya diterima bila DB_NAME *_test.
go test -tags e2e -run E2E ./...   # env: E2E_BACKEND_URL, E2E_AUD, E2E_GOOGLE_CLIENT, E2E_ADMIN_EMAIL
# Tes foto produk butuh fixture backend/testdata/*.webp (sintetis). E2E foto: folder uploads uji 10001:101 2750
# di-mount ke backend uji (rw) dan frontend uji (ro), env E2E_FRONTEND_URL wajib.
# Frontend (jest/jsdom): cd frontend && CI=true npx react-scripts test --watchAll=false
```

## Pesanan (Tahap 1)

Belum termasuk: chat, stok, kupon, payment gateway, upload bukti transfer, email, kurir/resi.

**Alur pelanggan** (wajib login): "Tambah ke keranjang" di kartu produk (belum login → `/login`, lalu
kembali ke toko) → `/keranjang` (ubah qty, hapus; produk nonaktif ditandai) → `/checkout` (data penerima;
item & harga diambil server dari keranjang) → halaman pesanan `/pesanan/<nomor>` berisi instruksi transfer,
tombol "Konfirmasi via WhatsApp" (ke `store_whatsapp`, disembunyikan bila belum diisi), dan tombol batal selama
status *Menunggu pembayaran*. Daftar pesanan: `/pesanan` (menu "Pesanan Saya").

**Status** (hanya 4): `pending_payment` (Menunggu pembayaran) → `paid` (Dibayar) → `completed` (Selesai), atau
→ `cancelled` (Dibatalkan). Izin: pending→paid (admin); pending→cancelled (admin/pemilik); paid→completed (admin);
paid→cancelled (admin, alasan wajib). `completed`/`cancelled` final. Admin mengirim status lama (`from`); bila sudah
berubah (admin lain), server menjawab **409**. Pembatalan pesanan tak dibayar dilakukan manual oleh admin.

**Diskon & ongkir**: diisi admin di detail pesanan selama *Menunggu pembayaran* (diskon ≤ subtotal, ongkir
0–10.000.000), `total = subtotal − diskon + ongkir` dihitung ulang di server (juga dijaga CHECK di DB). Setelah
dibayar terkunci. Harga item adalah snapshot saat checkout (perubahan harga produk tidak memengaruhi pesanan).

**Invoice**: tombol "Cetak invoice" di detail pesanan admin memakai generator PDF yang sama dengan
`/admin/invoice` (src/invoicePdf.js) + nomor pesanan, data penerima, subtotal/diskon/ongkir, stempel LUNAS bila
status Dibayar/Selesai, dan rekening dari Pengaturan Toko (bila kosong: rekening default invoice manual).
Tombol "Kirim ringkasan ke WhatsApp pelanggan" membuka `wa.me` ke nomor penerima.

**Notifikasi Discord**: `DISCORD_ORDER_WEBHOOK_URL` (rahasia, hanya `https://discord.com/api/webhooks/...`).
Dikirim setelah commit, asinkron (timeout 5 detik, 1x ulang); gagal tidak menggagalkan pesanan (hanya log, tanpa
URL). Kejadian: pesanan dibuat, dibayar, dibatalkan. Isi: nomor pesanan, nama pemesan (markdown/mention
dinetralkan, `allowed_mentions` kosong), jumlah item, total, status, tautan `PUBLIC_BASE_URL/admin/orders/<id>`.
**Tidak** ada alamat, telepon, email, atau catatan. Kosong → log `PERINGATAN: notifikasi Discord dinonaktifkan`.

**Endpoint pelanggan** (Bearer sesi; tanpa login → 401; batas laju per pengguna: keranjang 120/menit,
checkout 10/10 menit, batal 10/10 menit):

| Method | Path | Keterangan |
|---|---|---|
| GET | `/api/cart` | `{items[{productId,name,price,qty,lineTotal,available,unit,unitPrice,…}], subtotal, itemCount, hasUnavailable, savings, hasPriceChanges}` |
| POST | `/api/cart/ack-prices` | "Mengerti": harga terlihat = harga terkini untuk semua baris |
| POST | `/api/cart/items` | `{productId, qty?=1}` tambah (maks. 999/produk, 50 produk) |
| PUT | `/api/cart/items` | `{productId, qty}` set qty (0 = hapus) |
| DELETE | `/api/cart/items/{productId}`, `/api/cart` | hapus item / kosongkan |
| POST | `/api/orders` | `{recipientName, recipientPhone, address, city, postalCode?, note?, idempotencyKey(UUID), expectedTotal?}` → 201; kunci sama → 200 pesanan yang sama; `expectedTotal` ≠ total server → 409 `price_changed`. Field harga dari browser lainnya diabaikan |
| GET | `/api/orders`, `/api/orders/{orderNo}` | milik sendiri; milik orang lain → 404 (sama seperti tidak ada) |
| POST | `/api/orders/{orderNo}/cancel` | `{reason?}` hanya pemilik, hanya dari `pending_payment` |
| GET | `/api/store-info` | WA toko & rekening (null bila belum diisi) |

**Endpoint admin** (Cloudflare Access + CSRF untuk mutasi): `GET /api/admin/orders?status=&from=&to=&q=&page=`,
`GET /api/admin/orders/{id}`, `PATCH /api/admin/orders/{id}/pricing {discount, discountNote, shippingFee}`,
`PATCH /api/admin/orders/{id}/status {from, to, reason?, paymentNote?, note?}`,
`PATCH /api/admin/orders/{id}/note {adminNote}`, `GET/PUT /api/admin/settings` (kunci `store_whatsapp`,
`bank_name`, `bank_account_number`, `bank_account_holder`, `payment_note`), `GET /api/admin/summary` (+ `orders`).

**Log aktivitas** (dalam transaksi yang sama dengan perubahannya): `order.create`, `order.cancel`, `order.pay`,
`order.complete`, `order.pricing_update` (dari→menjadi), `order.note_update` (hanya panjang, bukan isi),
`settings.update` (hanya nama kunci + "diubah", bukan nilai rekening). Tanpa alamat/telepon. Setiap perubahan status
juga menulis `order_status_history`. Cara memeriksa: buka `/admin/activity`, filter *Jenis entitas* `order` atau
aksi `order.pay` dsb.; atau sebagai root:
```bash
docker exec mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore -e \
  "SELECT created_at, action, entity_id, actor_label, summary FROM activity_logs WHERE entity_type IN (\"order\",\"site_settings\") ORDER BY id DESC LIMIT 30"'
```

**Migrasi pesanan** (root, setelah `~/mysql-stack/dump.sh`):
```bash
cd ~/mihanstore
for f in 006_carts 007_orders 008_site_settings; do
  docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/$f.sql
done
docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/009_grants_orders.sql
```

### Langkah pemilik (pesanan, berurutan)
a. **Webhook Discord**: buat channel baru (mis. `#pesanan-toko`) → Edit channel → Integrasi → Webhook → Webhook
   Baru → Salin URL. Ketik sendiri di `~/mihanstore/.env`: `DISCORD_ORDER_WEBHOOK_URL=<url>` (jangan dibagikan), lalu
   `cd ~/mihanstore && docker compose up -d backend`. Cek `docker logs mihanstore_backend 2>&1 | grep -i discord`
   menampilkan `notifikasi Discord aktif` dan **tidak** lagi `notifikasi Discord dinonaktifkan`.
b. **Admin → Pengaturan Toko** (`/admin/settings`): isi nomor WhatsApp toko, nama bank, nomor rekening, atas nama,
   catatan pembayaran → Simpan.
c. **Uji alur**: daftar/login sebagai pelanggan → tambah produk ke keranjang → checkout → cek notifikasi Discord
   (hanya nomor, nama, jumlah item, total, status; tanpa alamat/telepon) → buka `/admin/orders` → set diskon/ongkir →
   Tandai Dibayar (notifikasi "Pesanan dibayar") → Cetak invoice (ada LUNAS) → Tandai Selesai. Cek tampilan di HP.
d. **Log & rollback**: periksa `/admin/activity` (aksi `order.*`, `settings.update`). Rollback aplikasi:
   ```bash
   cd ~/mihanstore
   docker tag mihanstore-backend:pre-orders mihanstore-backend:latest
   docker tag mihanstore-frontend:pre-orders mihanstore-frontend:latest
   docker compose up -d --no-build backend frontend
   # Kode: agar build berikutnya tidak membawa fitur pesanan lagi, buat commit pembalik:
   git revert --no-edit pre-orders..HEAD
   ```
   Tabel baru boleh dibiarkan (kode lama tidak membacanya; hak per tabel tambahan tidak mengganggu).

## Harga grosir (berjenjang) & satuan jual

- **Siapa**: semua pelanggan, otomatis menurut jumlah **per produk** (bukan gabungan kategori/total). Harga
  jenjang berlaku untuk **semua unit** di baris itu. Data awal jenjang kosong; `products.price` tetap harga eceran.
- **Aturan (server, satu tempat: `backend/pricing.go` `effectiveUnitPrice`)**: jenjang dengan `min_qty` terbesar
  ≤ qty (qty ≥ 2). `fixed` = harga per unit (Rp); `percent` = `round half up(harga dasar × (100 − p) / 100)`.
  Harga efektif = min(harga jenjang, harga dasar), minimal Rp 1. Keranjang, checkout, pesanan, API publik, dan admin
  memakai fungsi ini; harga dari browser selalu diabaikan.
- **Validasi admin** (server, 422 + `tierErrors[{index,minQty,message}]`; form membantu dengan peringatan merah):
  maks. **20 jenjang**/produk, `min_qty` 2–1.000.000 unik, `fixed` bilangan bulat ≥ 1, `percent` 0,01–99,99 (2 desimal),
  harga efektif tiap jenjang **lebih murah** dari harga dasar dan **makin murah** untuk `min_qty` lebih besar. Mengubah
  harga dasar sehingga jenjang (mis. `fixed`) tidak valid → ditolak 422 dengan pesan jenjang yang bermasalah.
  Simpan = ganti seluruh daftar dalam satu transaksi (soft delete / update / insert) + log `product.update`
  (selisih `unit` dan `tiers`, ringkas: `min 10: Rp 42.000`, `min 50: 10%`).
- **Satuan** (`products.unit`, maks. 20: huruf/angka/spasi/titik/garis miring, disimpan huruf kecil, default `pcs`):
  tampil "Rp 42.000 / pak" di kartu produk, keranjang, checkout, pesanan, invoice PDF pesanan, Pricelist, admin.
- **Penanda perubahan harga**: `cart_items.seen_unit_price` = harga terakhir yang dilihat pelanggan (diisi saat
  tambah / ubah jumlah / "Mengerti"; NULL diisi saat keranjang dibuka). Bila harga efektif sekarang (qty sama)
  berbeda → `priceChanged` + banner. Perubahan karena pelanggan mengubah jumlah bukan "perubahan harga".
- **Checkout**: frontend mengirim `expectedTotal` (total di layar). Beda dengan hitungan server → **409**
  `{error:"price_changed", message, cart}` tanpa membuat pesanan / mengosongkan keranjang; pelanggan menekan
  "Konfirmasi & buat pesanan" (ack + kirim ulang dengan total baru, kunci idempotensi sama). Tanpa `expectedTotal`
  (klien lama) tidak diperiksa. Pesanan menyimpan snapshot `unit_price` (efektif), `base_unit_price`, `tier_min_qty`,
  `unit`; pesanan yang sudah ada **tidak pernah** berubah. Pesanan lama: kolom baru `null`, tampil seperti dulu.
- **API**: `GET /api/products` & `/search` menambah `unit` dan `tiers:[{minQty,type,value,unitPrice}]` di belakang field
  lama (bentuk lama utuh). Keranjang per baris: `unit, baseUnitPrice, unitPrice, tierMinQty, nextTier{minQty,unitPrice,
  moreQty}, savings, priceChanged, previousUnitPrice` (+ `savings`, `hasPriceChanges` di keranjang);
  `POST /api/cart/ack-prices` (wajib login). Item pesanan (pelanggan & admin): `unit, baseUnitPrice, tierMinQty`.
  Admin produk: `unit` dan `tiers:[{minQty,type,value}]` (tanpa field `tiers` = jenjang tidak diubah; `[]` = hapus semua).
- **Teks/Invoice**: ringkasan WhatsApp admin → pelanggan "2 x Rp 42.000 / pak = …" + "(harga grosir min. N)";
  invoice PDF pesanan: "12 PAK", "Rp 42.000 / PAK", catatan "HARGA GROSIR (MIN 10)". Invoice manual tidak berubah.
- **Pricelist**: pilihan *Eceran + grosir* (bawaan) / *Eceran saja* / *Grosir saja*. Jenjang tampil sebagai teks kecil
  di bawah nama produk ("10+ : Rp 42.000 · 50+ : Rp 40.500", dibungkus per jenjang, tanpa terpotong sampai 20 jenjang);
  satuan di samping harga ("/ pak"); *Grosir saja* menyembunyikan harga eceran produk berjenjang ("per pak").
- **Langkah pemilik**: Admin → Produk → Ubah satu produk → isi **Satuan** dan tambah jenjang di **Harga grosir**
  (cek pratinjau) → Simpan. Uji di toko: kartu produk (lencana Grosir), keranjang (ubah jumlah melewati jenjang,
  petunjuk "Tambah N lagi"), ubah harga di admin lalu buka keranjang lagi (penanda "Harga berubah" + Mengerti),
  checkout. Cetak Admin → Pricelist dengan tampilan "Eceran + grosir" / "Grosir saja".
- **Migrasi** (root, setelah `~/mysql-stack/dump.sh`):
  ```bash
  cd ~/mihanstore
  for f in 010_products_unit_price_tiers 011_cart_order_price_snapshot; do
    docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/$f.sql
  done
  docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/012_grants_price_tiers.sql
  ```
- **Rollback aplikasi** (DB boleh dibiarkan: kode lama tidak membaca kolom/tabel baru, kolom baru nullable/berdefault,
  hak tambahan pada `product_price_tiers` tidak dipakai kode lama):
  ```bash
  cd ~/mihanstore
  docker tag mihanstore-backend:pre-tiers mihanstore-backend:latest
  docker tag mihanstore-frontend:pre-tiers mihanstore-frontend:latest
  docker compose up -d --no-build backend frontend
  git revert --no-edit pre-tiers..HEAD   # agar build berikutnya tidak membawa fitur ini lagi
  ```

## Kolom nominal uang (`MoneyInput`)

Semua kolom isian rupiah memakai komponen bersama `frontend/src/components/MoneyInput.js`: tampil dengan titik
setiap 3 digit saat diketik (`1250000` → `1.250.000`), `inputMode="numeric"` tanpa panah spinner, ukuran huruf
mengikuti aturan global (16px di HP). Nilai yang diteruskan ke form tetap digit polos / angka bulat, jadi payload API
tidak berubah.

| Berkas | Kolom | Batas |
| --- | --- | --- |
| `admin/Products.js` | Harga eceran (Rp) | maks. 10 digit; validasi 0–1.000.000.000 tetap |
| `admin/Products.js` (TierEditor) | Nilai jenjang bertipe **Rp** | maks. 10 digit (jenjang **%** tetap kolom desimal biasa) |
| `admin/Orders.js` (detail pesanan) | Diskon (Rp), Ongkir (Rp) | maks. 10 digit; aturan "diskon ≤ subtotal", "ongkir ≤ Rp 10.000.000" tetap |
| `components/InvoiceCreate.js` | Harga Satuan (Rp) | maks. 10 digit |

Sengaja **tidak** memakai `MoneyInput` (bukan uang): jumlah minimal jenjang, nilai jenjang %, qty (keranjang & invoice),
kode pos, telepon/WhatsApp, nomor rekening, urutan kategori.

Perilaku:
- Hanya digit yang diterima; nol di depan dirapikan (`007` → `7`); kolom kosong tetap kosong (pemanggil yang
  memperlakukannya sebagai 0 bila perlu, mis. diskon/ongkir).
- **Tempel (paste)**: bila teks berakhir dengan koma/titik + tepat 1–2 digit (boleh diikuti teks non-digit) *dan*
  sebelumnya ada pemisah ribuan (titik/koma di antara dua digit), bagian desimal dibuang; selain itu semua karakter
  non-digit dibuang. Contoh: `Rp 1.500.000`, `1,500,000`, `1.500.000,00`, `1500000` → `1500000`;
  `IDR 25.000,-` → `25000`; `12,5` → `125` (tanpa pemisah ribuan tidak dianggap desimal). Teks tanpa digit diabaikan.
  Aturan desimal hanya untuk tempelan, bukan ketikan.
- **Kursor** dihitung ulang dari jumlah digit di kiri kursor (tidak melompat ke akhir saat menyunting di tengah).
  Backspace tepat setelah titik menghapus digit sebelum titik; Delete tepat sebelum titik menghapus digit sesudahnya.
- **Undo/redo** (Ctrl/Cmd+Z, Ctrl+Y, Ctrl/Cmd+Shift+Z, dan menu konteks) memakai riwayat milik komponen, karena
  riwayat undo asli browser hilang saat tampilan diformat ulang.
- `maxDigits` = batas keras (ketikan/tempelan yang melewati batas ditolak). `min`/`max` = batas lunak: kolom diberi
  `aria-invalid="true"`, pesan galat tetap dari validasi form saat simpan.
- Ganti jenis jenjang dari % ke Rp saat nilainya berdesimal (mis. `12,5`) mengosongkan nilai (rupiah harus bulat).
- Prop: `value` (string digit / angka / kosong), `onValueChange(digits, number)`; atribut lain (`id`, `name`,
  `placeholder`, `required`, `disabled`, `aria-*`, `className`, `onBlur`, `ref`) diteruskan ke `<input>`.
  Label, adornment "Rp", dan pesan galat tetap di pemanggil.
- Tes: `src/__tests__/moneyInput.test.js` (format/parse & perilaku komponen), `src/__tests__/moneyForms.test.js`
  (form produk, jenjang, diskon/ongkir, Buat Invoice; payload tetap angka bulat).

## Pelanggan & alias (admin)

- **Alias** = nama panggilan/label internal pelanggan (mis. "Bu Siti Toko Maju") yang **hanya dilihat admin**. Kolom
  `users.alias` (migrasi 016, NULL = tanpa alias), boleh sama antar pelanggan, maks. 100 karakter setelah dirapikan
  (karakter kontrol, bidi, dan tak terlihat dibuang; spasi dirapikan; kosong = hapus alias). Aturan sama di backend
  (`NormalizeAlias`, `backend/customers.go`) dan frontend (`src/admin/alias.js`).
- **Tidak pernah dikirim ke pelanggan**: struct `User`/`UserDTO` tidak memetakan kolom ini; alias hanya dibaca dengan SQL
  eksplisit di rute `/api/admin/*` dan untuk notifikasi Discord internal. Respons `/api/auth/*`, `/api/cart*`,
  `/api/orders*` dan teks WhatsApp ke pelanggan tidak memuat alias (diuji di unit, integrasi, e2e, dan jest).
- **Menu Admin → Pelanggan** (grup Penjualan, setelah Pesanan; juga di bilah ikon HP, pencarian menu, breadcrumb
  Admin / Penjualan / Pelanggan; kartu jumlah pelanggan dan pintasan di Dashboard):
  - `/admin/customers`: akun role `customer` yang belum dihapus (admin tidak tampil): alias (menonjol), nama akun,
    username, email, telepon, jumlah pesanan (semua status), total belanja (jumlah `total` pesanan berstatus **Dibayar**
    atau **Selesai**), tanggal daftar, status akun. Cari (nama, username, email, telepon termasuk format 08xx, alias),
    urutan Terbaru / Jumlah pesanan / Total belanja, paginasi 20 per halaman. Tombol pensil = modal ubah alias
    (penghitung karakter, kosongkan = hapus).
  - `/admin/customers/<id>`: data akun, alias + tombol ubah, statistik (jumlah pesanan, total belanja, pesanan terakhir),
    10 pesanan terakhir (nomor pesanan bertaut ke `/admin/orders/<nomor>`).
- **Pesanan admin**: kolom pemesan menampilkan alias tebal dengan nama akun kecil di bawahnya; kolom cari (`q`) juga
  mencocokkan alias; detail pesanan → kartu *Akun pemesan* menampilkan alias, tombol Beri/Ubah alias, tautan ke
  halaman pelanggan.
- **Invoice dari pesanan**: kotak centang **"Pakai nama alias"** di samping *Cetak invoice*, hanya muncul bila pelanggan
  punya alias, bawaan **mati** (nama di invoice = nama penerima seperti sebelumnya); dicentang → nama pelanggan di PDF =
  alias (blok "KIRIM KE" tetap data penerima). Teks *Kirim ringkasan ke WhatsApp pelanggan* dan invoice manual tidak berubah.
- **Notifikasi Discord**: field "Pemesan" = alias bila ada, selain itu nama akun (markdown/mention dinetralkan, dipotong
  64 karakter seperti sebelumnya). Tidak ada data pribadi tambahan.
- **API admin** (Cloudflare Access + `requireAdmin`; mutasi wajib CSRF/Origin; batas laju umum admin 300/menit/IP +
  ubah alias 60/menit per admin):
  | Method | Path | Keterangan |
  |---|---|---|
  | GET | `/api/admin/customers?q=&sort=newest\|orders\|spent&page=&per_page=` | `{items[{id,name,username,email,phone,alias,status,orderCount,totalSpent,lastOrderAt,createdAt}], total, page, perPage}`; `sort` di luar daftar putih → 400; `per_page` maks. 100 |
  | GET | `/api/admin/customers/{id}` | `{customer:{…}, recentOrders[{id,orderNo,status,statusLabel,total,createdAt}]}`; bukan customer/dihapus/tidak ada → 404 |
  | PATCH | `/api/admin/customers/{id}/alias` | `{alias}` (string; `""`/`null` = hapus; field wajib) → `{id, alias}`; > 100 karakter → 400; bukan customer → 404 |

  Respons `GET /api/admin/orders` (per item) dan `GET /api/admin/orders/{id}` menambah `customer.id` dan `customer.alias`;
  `GET /api/admin/summary` menambah `customers` (jumlah akun pelanggan).
- **Log aktivitas**: `customer.alias_update` (entitas `user`, `public_id`), details hanya `{"alias":{"dari":…,"menjadi":…}}`,
  ditulis dalam transaksi yang sama dengan perubahan; alias yang tidak berubah tidak dicatat.
- **Hak DB**: tidak berubah (`mihanstore_app` sudah punya SELECT, INSERT, UPDATE pada `users`).
- **Migrasi** (root, setelah `~/mysql-stack/dump.sh`; sudah dijalankan di produksi 3 Okt 2026):
  ```bash
  cd ~/mihanstore
  docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/016_users_alias.sql
  ```
- **Langkah pemilik**: buka Admin → Pelanggan, beri alias pada satu pelanggan; cek daftar pesanan (alias tebal) dan detail
  pesanan; coba *Cetak invoice* dengan "Pakai nama alias" menyala dan mati; buat satu pesanan uji dan lihat notifikasi
  Discord memakai alias. Pemasangan yang sama juga membawa perbaikan telepon checkout (4c71986) dan kolom uang MoneyInput
  (33930eb): uji kolom uang (titik ribuan) di form produk, jenjang grosir, diskon & ongkir, dan Buat Invoice; uji checkout
  dengan nomor telepon yang ditempel dari kontak HP.
- **Rollback aplikasi** (DB boleh dibiarkan: kolom `alias` nullable dan tidak dibaca kode lama). Image `:pre-alias-bundle`
  adalah image produksi SEBELUM bundel ini, jadi rollback juga **membatalkan perbaikan telepon dan MoneyInput**:
  ```bash
  cd ~/mihanstore
  docker tag mihanstore-backend:pre-alias-bundle mihanstore-backend:latest
  docker tag mihanstore-frontend:pre-alias-bundle mihanstore-frontend:latest
  docker compose up -d --no-build --no-deps backend frontend
  git revert --no-edit pre-alias-bundle..HEAD   # agar build berikutnya tidak membawa bundel ini lagi
  ```
  Cadangan database sebelum migrasi: `~/mihanstore_pre_alias_backup/mysql-all-2026-10-03-pre-alias.sql.gz`.

## Data wilayah & alamat checkout

- **Sumber**: https://wilayah.id/api (JSON statis, Kepmendagri). Disimpan di `region_datasets` dalam format JSON,
  satu baris per dataset induk: `provinces`, `regencies:<kodeProv>`, `districts:<kodeKabKota>`,
  `villages:<kodeKec>`; kolom `data` = array `[{code,name}]` seperti dari API. Kode bertitik (`31`, `31.74`,
  `31.74.06`, `31.74.06.1004`). API ini tidak menyediakan kode pos (diisi pelanggan).
- **Admin → Data Wilayah** (`/admin/regions`, grup Sistem): status data tersimpan (tanggal data sumber, jumlah
  dataset/item per tingkat, ukuran, terakhir disimpan oleh siapa), tombol **Fetch ulang dari wilayah.id**, kemajuan
  (polling tiap 3 detik hanya selama berjalan, tombol Batalkan), ringkasan (jumlah per tingkat tersimpan vs baru,
  dataset baru/berubah/hilang, peringatan bila turun >10% / tingkat kosong / ada permintaan gagal), popup
  **"Simpan data wilayah ke database?"** (Simpan / Batal), riwayat 10 run.
  - Simpan: upsert dari staging dalam SATU transaksi (hanya dataset yang `content_hash`-nya berubah di-UPDATE, yang
    baru di-INSERT, kunci yang hilang di sumber di-soft-delete, kunci yang muncul lagi dipulihkan), staging dihapus,
    cache memori dibersihkan. Data lama tetap dipakai sampai commit.
  - Batal: staging dihapus, status `discarded`, data lama tidak berubah. Run berjalan bisa dibatalkan (`cancelled`).
  - Hanya satu run `running`/`staged` (kolom `active_lock` UNIQUE). Hasil `staged` > 24 jam dibuang otomatis saat
    fetch berikutnya. Run `running` saat backend restart ditandai `failed` saat start. Jeda minimal antar fetch 5 menit.
  - Fetcher: konkurensi 4, jeda minimal 100 ms antar permintaan, timeout 15 dtk, retry 3x (429/5xx/jaringan) dengan
    backoff eksponensial + jitter dan menghormati `Retry-After`, respons maks. 5 MB, validasi JSON/kode/induk/duplikat,
    User-Agent `MihanStore/1.0 (+https://store.mihan.web.id; admin region sync)`. Gagal bila provinsi < 30 atau
    > 20 permintaan gagal setelah retry; permintaan gagal (≤ 20) → data lama wilayah itu dipertahankan.
  - Log aktivitas: `regions.fetch_start`, `regions.fetch_done`, `regions.fetch_failed`, `regions.save`, `regions.discard`.
  - Endpoint admin: `GET /api/admin/regions/status`, `POST /api/admin/regions/fetch` (202),
    `GET /api/admin/regions/runs/{id}`, `POST .../runs/{id}/save`, `POST .../runs/{id}/discard`.
- **API publik** `/api/regions/*`: `{"data":[{code,name}],"meta":{"updatedAt":"YYYY-MM-DD"}}`, `Cache-Control: public,
  max-age=3600` + `ETag` (304), kode divalidasi (400), induk tidak ada 404, belum ada data 503 "Data wilayah belum
  tersedia", 240 permintaan/menit per IP, cache memori 10 menit (dibersihkan saat Simpan dari admin).
  `meta.updatedAt` = tanggal data sumber saat dataset itu terakhir berubah.
- **Checkout**: nama penerima kosong di awal (telepon diisi dari akun bila ada); Provinsi → Kabupaten/Kota → Kecamatan →
  Kelurahan/Desa berupa kolom pilihan yang bisa diketik (dimuat per induk, memilih tingkat atas mengosongkan tingkat
  bawah), Alamat lengkap (jalan, RT/RW, nomor), Kode pos 5 digit. Semua wajib kecuali Catatan.
  `POST /api/orders` kini wajib `provinceCode, regencyCode, districtCode, villageCode` (kode saja); server memeriksa
  rantai di data tersimpan dan mengambil NAMA dari DB; `city` diisi nama kab/kota. Galat 422 `{error, field}`; klien
  lama tanpa kode → 422 "…Perbarui halaman lalu coba lagi."; data wilayah belum ada → 503. Respons pesanan
  (pelanggan & admin) memuat `recipient.region` (null untuk pesanan lama) dan `recipient.fullAddress`.
- **Alamat tersusun** (detail pesanan pelanggan/admin, invoice PDF, teks WhatsApp admin → pelanggan):
  `<alamat lengkap>, <kelurahan/desa>, Kec. <kecamatan>, <kab/kota>, <provinsi> <kode pos>`; pesanan lama:
  `<alamat>, <kota> <kode pos>`. Notifikasi Discord dan teks konfirmasi pelanggan → toko tidak memuat alamat.
- **Impor awal / manual (CLI, kode impor yang sama)**: `docker exec mihanstore_backend /app/main import-regions`
  (label run "CLI (impor awal)", log `regions.save` aktor sistem; `--no-save` = biarkan staged untuk diputuskan di admin).
  Impor awal produksi (3 Okt 2026): 38 provinsi, 514 kab/kota, 7.285 kecamatan, 83.762 kelurahan/desa (7.838 dataset,
  data sumber 2025-07-04), 7.838 permintaan, 0 retry, 0 gagal, 13 menit 15 detik; ±4,7 MB JSON (tabel ±7,4 MB).
- **Konfigurasi**: `REGION_API_BASE` (default `https://wilayah.id/api`; selain `https://wilayah.id/...` hanya berlaku
  bila `DB_NAME` berakhiran `_test`). Khusus uji: `TEST_ONLY_REGION_MIN_PROVINCES`, `TEST_ONLY_REGION_COOLDOWN_SECONDS`.
- **Migrasi** (root, setelah `~/mysql-stack/dump.sh`):
  ```bash
  cd ~/mihanstore
  for f in 013_regions 014_orders_region; do
    docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/$f.sql
  done
  docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/015_grants_regions.sql
  ```
- **Langkah pemilik**: buka Admin → Data Wilayah untuk melihat status; Fetch ulang nanti bila perlu (paling sering
  sebulan sekali, API gratis) lalu periksa ringkasan sebelum Simpan. Uji checkout sebagai pelanggan (nama kosong,
  wilayah bertingkat, semua wajib), lalu cek detail pesanan, Cetak invoice, dan teks "Kirim ringkasan ke WhatsApp".
- **Rollback aplikasi** (DB boleh dibiarkan: tabel/kolom baru terpisah/nullable, hak tambahan tidak dipakai kode lama;
  checkout kode lama tidak mengirim wilayah dan tetap berfungsi dengan kolom `city`):
  ```bash
  cd ~/mihanstore
  docker tag mihanstore-backend:pre-regions mihanstore-backend:latest
  docker tag mihanstore-frontend:pre-regions mihanstore-frontend:latest
  docker compose up -d --no-build --no-deps backend frontend
  git revert --no-edit pre-regions..HEAD   # agar build berikutnya tidak membawa fitur ini lagi
  ```

## Foto produk (disk VPS)

Satu foto utama per produk, disimpan sebagai berkas di disk VPS (bukan database, bukan layanan cloud).

**Arsitektur**
- Backend Go punya lapisan penyimpanan `ImageStore` (`backend/imagestore.go`: `Save` atomik, `Delete`, `URL`).
  Implementasi saat ini `LocalStore` (`IMAGE_STORE=local`) menulis ke `UPLOADS_DIR` (default `/data/uploads`).
- Tabel hanya menyimpan KUNCI di `products.image_path`: `products/<id>/<uuid v4>.jpg`; thumbnail diturunkan
  dengan akhiran `_t` (`products/<id>/<uuid>_t.jpg`). Nama berkas dari klien tidak pernah dipakai. Tidak ada migrasi baru.
- Nilai lama yang bukan pola itu (mis. `kerupuk1.jpg`, tanpa berkas fisik) dianggap TANPA foto.
- API publik `/api/products`, `/api/products/search`, keranjang `/api/cart`: `image` = URL foto utama
  (`/uploads/products/<id>/<uuid>.jpg`) atau `""`; field baru `thumb` = URL thumbnail atau `""`. Pesanan tidak menyimpan foto.
- Admin (Cloudflare Access + `requireAdmin` + CSRF; batas 20 unggah/hapus per menit per admin):
  - `POST /api/admin/products/{id}/image` (multipart/form-data, field `file`) -> `{image, thumb}`
  - `DELETE /api/admin/products/{id}/image` -> `{image:"", thumb:""}`
  - Ini satu-satunya rute admin yang menerima `multipart/form-data`; header `X-Requested-With` dan Origin/Sec-Fetch-Site tetap wajib.
  - `PUT /api/admin/products/{id}` tidak lagi mengubah foto unggahan (field `imagePath` diabaikan bila produk sudah
    punya foto; kunci foto tidak bisa disetel manual -> 400).
  - Log aktivitas: `product.image_update` (kunci baru/lama, ukuran byte) dan `product.image_delete`, dalam
    transaksi yang sama dengan perubahan `image_path` + `updated_by`.
- nginx frontend menyajikan `/uploads/` dari folder yang sama (mount READ-ONLY) hanya untuk pola kunci sah,
  dengan `Cache-Control: public, max-age=31536000, immutable` dan `X-Content-Type-Options: nosniff`; lainnya 404,
  tanpa daftar direktori, path mentah berisi `..`/`%2e`/`%2f`/`\` ditolak. NPM tidak diubah (`/` -> frontend).

**Batas dan pemrosesan**
- Browser (form admin) memperkecil foto dulu: canvas, sisi terpanjang <= 1600 px, JPEG ±0,85, orientasi EXIF
  diterapkan (`createImageBitmap(..., {imageOrientation: 'from-image'})`), kualitas diturunkan bertahap bila > 2 MB.
- Server: maks. 2 MB; hanya JPG/PNG/WebP (dicek dari ISI berkas); `DecodeConfig` dulu menolak dekompresi-bom
  (> 12.000 px per sisi atau > 40 MP); orientasi EXIF 1–8 diterapkan; PNG/WebP transparan diratakan ke putih;
  diperkecil Catmull-Rom ke foto utama 1200 px (JPEG q82) + thumbnail 400 px (JPEG q80); metadata dibuang.
  Pemrosesan paralel dibatasi 2. Galat: 413 (terlalu besar), 415 (tipe), 422 (rusak/resolusi), 507 (penuh), 503 (penyimpanan tidak tersedia).
- Pagar disk: `MAX_UPLOADS_MB` (default 2048). Ukuran folder dihitung ulang tiap 5 menit (cache), unggahan baru
  ditolak 507 bila melewati batas. Saat start backend memeriksa folder dapat ditulis (log
  "direktori unggahan /data/uploads dapat ditulis"); bila tidak, hanya unggah foto yang menjawab 503.
- Dependensi baru: `golang.org/x/image` (paket `draw` dan `webp`).

**Lokasi berkas**: host `~/mihanstore/uploads` (di `.gitignore`/`.dockerignore`), pemilik `10001:101`
(user `app` backend : grup `nginx` frontend), mode `2750` (setgid: subfolder/berkas baru ikut grup 101), berkas `0640`.
Di-mount ke backend `/data/uploads` (baca-tulis) dan frontend `/usr/share/nginx/uploads` (hanya baca).
```bash
sudo install -d -o 10001 -g 101 -m 2750 ~/mihanstore/uploads   # sekali, sebelum docker compose up
```

**Backup & pemulihan**: `mihanstore/uploads` ikut arsip harian terenkripsi `~/backup/backup.sh` (lihat
`~/backup/RESTORE.md` bagian 5a). Pulihkan folder lalu set ulang pemilik/izin:
`sudo chown -R 10001:101 ~/mihanstore/uploads && sudo find ~/mihanstore/uploads -type d -exec chmod 2750 {} + && sudo find ~/mihanstore/uploads -type f -exec chmod 0640 {} +`.
Foto yang dihapus dari admin langsung hilang dari disk (hanya ada di arsip backup sebelumnya); salinan di cache
Cloudflare bisa bertahan sampai kedaluwarsa, tetapi URL-nya acak (UUID) dan tidak lagi dirujuk.

**Mengganti ke Cloudflare R2 / Google Cloud Storage nanti**: tambah implementasi `ImageStore` baru
(`Save` = upload objek dengan kunci yang sama, `Delete` = hapus objek, `URL` = URL publik bucket/CDN), pilih lewat
`IMAGE_STORE` di `config.go`/`NewApp`, salin isi folder `uploads/` ke bucket dengan kunci yang sama. Tabel, API, dan
tampilan tidak berubah (frontend memakai `image`/`thumb` apa adanya).

**Langkah pemilik**: buka Admin -> Produk -> Ubah satu produk -> bagian "Foto produk" -> "Pilih foto" (dari HP
bisa kamera atau galeri) -> cek pratinjau -> Simpan. Lalu cek kartu produk di toko (HP dan komputer) dan keranjang;
buka Pricelist, centang "Tampilkan foto produk", unduh PNG; coba "Ganti foto" dan "Hapus foto". Untuk produk baru,
foto dipilih dulu lalu otomatis diunggah setelah produk tersimpan; bila unggah gagal, produk tetap ada dan ada
tombol "Coba unggah lagi".

**Rollback**: `docker tag mihanstore-backend:pre-uploads mihanstore-backend:latest && docker tag mihanstore-frontend:pre-uploads mihanstore-frontend:latest`
lalu `git checkout pre-uploads -- docker-compose.yml` (atau biarkan volume) dan `docker compose up -d --no-deps --no-build backend frontend`.
Folder `uploads/` dan kolom `image_path` boleh dibiarkan: kode lama mengabaikan foto (kunci `products/...` dianggap nama berkas biasa dan tampil placeholder).

## Tugas pemilik (sekali saja, berurutan)

**a. Google Cloud (login Google pelanggan)**
1. https://console.cloud.google.com → buat **project baru** "Mihan Store".
2. Google Auth Platform (OAuth consent screen): tipe **External**; nama app "Mihan Store"; email dukungan;
   logo (opsional); beranda `https://store.mihan.web.id`; kebijakan privasi `https://store.mihan.web.id/privasi`;
   syarat `https://store.mihan.web.id/syarat`; authorized domain `mihan.web.id`; scope dasar
   `openid`, `email`, `profile`; lalu **Publish app** ke Production.
3. Credentials → Create credentials → **OAuth client ID** → tipe **Web application**;
   Authorized JavaScript origins: `https://store.mihan.web.id` (tambah `https://mihankids.my.id` bila dipakai);
   **tanpa** redirect URI. Salin **Client ID** (bukan rahasia; client secret tidak dipakai).
4. Isi di `~/mihanstore/.env`: `GOOGLE_CLIENT_ID=<client id>` dan `REACT_APP_GOOGLE_CLIENT_ID=<client id yang sama>`, lalu:
   ```bash
   cd ~/mihanstore && docker compose up -d --build frontend && docker compose up -d backend
   ```

**b. Cloudflare (proteksi admin)**
1. DNS: ubah record `store` menjadi **Proxied** (oranye). Dampak: IP klien dibaca dari
   `CF-Connecting-IP` (backend sudah mendukung, hanya dipercaya dari rentang IP Cloudflare);
   mode SSL zona harus **Full (strict)** (sertifikat Let's Encrypt di NPM untuk host store valid).
2. Zero Trust → Access → Applications → Add → **Self-hosted**: nama "Mihan Store Admin";
   dua destinasi pada domain `store.mihan.web.id`: path `admin*` dan path `api/admin*`;
   policy **Allow** → Include → Emails: `ikhsan.sumaila@gmail.com`, `qomariahakmala@gmail.com`;
   login method **Google**; session duration **30 hari**.
3. Salin **Application Audience (AUD) tag** ke `~/mihanstore/.env` sebagai `CF_ACCESS_AUD_STORE=...`
   (ketik sendiri), lalu `cd ~/mihanstore && docker compose up -d backend`.

**c. Tes**: buka `https://store.mihan.web.id/admin` (harus muncul login Google Cloudflare), kelola produk,
cek `/admin/activity`.

**d. Admin**: tidak perlu promosi manual — email di `ADMIN_EMAILS` otomatis jadi admin saat masuk lewat
Cloudflare Access atau login Google. Catatan: bila akun password lama (mis. `octavarium`) login dengan
Google, password lamanya dihapus dan login selanjutnya lewat Google.

## Getting Started

### Prerequisites
- Docker and Docker Compose installed on your system.

### Installation
1.  **Clone the repository:**
    ```bash
    git clone <repository_url>
    cd mihanstore
    ```
2.  **Create `.env` file:**
    Copy the `.env.example` to `.env` and configure any necessary environment variables. For MihanStore, `REACT_APP_API_URL` should point to your backend.
    ```bash
    cp .env.example .env
    ```

3.  **Build and run with Docker Compose:**
    Navigate to the `mihanstore` root directory and run:
    ```bash
    docker compose up --build -d
    ```

    This will:
    - Build the Go backend image and start the `mihanstore_backend` container on port `8080`
      (jaringan: `mihanstore-net`, `nginx-proxy-manager-network`, `mysql-net`).
    - Build the React frontend image (nginx) and start the `mihanstore_frontend` container on port `3000`.
    - Di server ini Nginx Proxy Manager meneruskan `store.mihan.web.id` ke `mihanstore_frontend:3000`
      dan `/api` ke `mihanstore_backend:8080`.

## Accessing the Application
-   **Frontend:** Open your browser to `http://localhost:3000`
-   **Backend API:** Access directly at `http://localhost:8080` (for testing purposes, usually accessed via frontend's Nginx proxy)

## Nginx Configuration for Production (mihankids.my.id)

To serve MihanStore on `https://mihankids.my.id` (replacing the old Netdata config), you need to update your Nginx configuration.

Edit `/etc/nginx/sites-available/mihankids` to look like this:

```nginx
server {
    listen 80;
    server_name mihankids.my.id;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name mihankids.my.id;

    ssl_certificate /etc/letsencrypt/live/mihankids.my.id/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mihankids.my.id/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    # Frontend (React App)
    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_redirect off;
    }

    # Backend API (optional, can be proxied via frontend's Nginx)
    # If you want to expose backend directly on a subpath, e.g., /api/
    # location /api/ {
    #     proxy_pass http://localhost:8080/api/;
    #     proxy_set_header Host $host;
    #     proxy_set_header X-Real-IP $remote_addr;
    #     proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    #     proxy_set_header X-Forwarded-Proto $scheme;
    # }
}
```

After updating the Nginx configuration, test the config and reload Nginx:
```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Development with Hot Reload (Recommended)

To develop with live hot-reloading for both backend and frontend (without needing to manually rebuild containers on every change):

1. **Start the development environment:**
   ```bash
   cd mihanstore
   docker compose -f docker-compose.dev.yml up -d
   ```

2. **How it works:**
   - **Backend (Go):** Uses `air` for automatic recompilation on file changes (`main.go`).
   - **Frontend (React):** Runs standard `react-scripts start` (Webpack dev server) with live browser reloading.
   - Any changes to `/backend` or `/frontend` source code will immediately reflect in the running containers via volume mounts.

3. **Stop the development environment:**
   ```bash
   docker compose -f docker-compose.dev.yml down
   ```

## Local Development (Without Docker)

### Frontend
```bash
cd frontend
npm install
npm start
```

### Backend
```bash
cd backend
go mod tidy
go run main.go
```

