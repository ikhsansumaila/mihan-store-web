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
| GET  | `/api/products`, `/api/products/search?q=&category=` | publik, dari DB (hanya produk aktif), bentuk respons sama seperti dulu |
| GET  | `/api/categories` | publik: `[{id, slug, name, sortOrder}]` |
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
- Rute admin frontend: `/admin` (Ringkasan), `/admin/products` (Produk), `/admin/categories`
  (Kategori), `/admin/invoice` (Buat Invoice), `/admin/activity` (Log aktivitas).
- **Buat Invoice** (`/admin/invoice`): PDF dibuat di browser dengan jsPDF (logo
  `/mihan-store-logo.png`, `/lunas-logo.png`), tanpa API backend. Rute lama `/invoice/create`
  tidak lagi tampil di toko; halaman itu hanya mengalihkan dengan muat ulang penuh
  (`window.location.replace('/admin/invoice')`) agar dicegat Cloudflare Access. Catatan: kode
  komponen invoice tetap ada di bundel JavaScript publik; yang dijaga adalah akses ke halamannya.
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
# Tes integrasi: butuh database *_test BARU yang dibuat dari migrasi 001–009 (+ hak 005 & 009 untuk user uji)
# (user uji tidak punya hak DELETE pada users/activity_logs, jadi DB dibuat ulang tiap putaran)
docker run --rm --network mysql-net -e DB_NAME=mihanstore_test -e DB_USER=... -e DB_PASSWORD=... \
  -v "$PWD/backend":/src -w /src golang:1.27-alpine go test -tags integration ./...
# Tes end-to-end (tag e2e): proses tes menyajikan JWKS tiruan di :9000; container backend UJI
# diarahkan ke sana dengan TEST_ONLY_CF_ACCESS_JWKS_URL / TEST_ONLY_GOOGLE_JWKS_URL.
# Variabel TEST_ONLY_* hanya berlaku bila DB_NAME berakhiran _test dan TIDAK BOLEH diset di produksi.
# Webhook Discord uji diarahkan ke server tiruan di proses tes (http://<runner>:9000/discord/api/webhooks/...);
# URL http/host selain discord.com hanya diterima bila DB_NAME *_test.
go test -tags e2e -run E2E ./...   # env: E2E_BACKEND_URL, E2E_AUD, E2E_GOOGLE_CLIENT, E2E_ADMIN_EMAIL
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
| GET | `/api/cart` | `{items[{productId,name,price,qty,lineTotal,available}], subtotal, itemCount, hasUnavailable}` |
| POST | `/api/cart/items` | `{productId, qty?=1}` tambah (maks. 999/produk, 50 produk) |
| PUT | `/api/cart/items` | `{productId, qty}` set qty (0 = hapus) |
| DELETE | `/api/cart/items/{productId}`, `/api/cart` | hapus item / kosongkan |
| POST | `/api/orders` | `{recipientName, recipientPhone, address, city, postalCode?, note?, idempotencyKey(UUID)}` → 201; kunci sama → 200 pesanan yang sama. Field harga/total dari browser diabaikan |
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

