# MihanStore - Simple Online Store

MihanStore is a simple online store application built with React (Frontend) and Go (Backend), deployed using Docker Compose.

## Features
- Product listing with categories (kerupuk, tepung, saos, sambal, sendok plastik, box hampers)
- Product search
- User registration and login (MySQL + GORM, argon2id, sesi di database, Cloudflare Turnstile)
- Basic UI for browsing products

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

Data pengguna disimpan di database `mihanstore` pada container `mysql_db`
(proyek `~/mysql-stack`, jaringan Docker internal `mysql-net`). Backend terhubung
sebagai user `mihanstore_app` yang hanya punya hak `SELECT, INSERT, UPDATE, DELETE`
pada `mihanstore.*` dan hanya dari subnet `mysql-net`. Skema dibuat oleh root lewat
file migrasi, bukan oleh aplikasi (tanpa AutoMigrate).

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

### Endpoint
| Method | Path | Keterangan |
|---|---|---|
| POST | `/api/auth/register` | `{username, email, phone?, name, password, turnstileToken}` → 201 `{success, user, token, expiresAt}` |
| POST | `/api/auth/login` | `{identifier, password, turnstileToken}` (identifier = email bila ada `@`, selain itu username; field lama `email` juga diterima) |
| POST | `/api/auth/verify` | token via `Authorization: Bearer <token>` atau body `{token}` → 200 `{valid:true,user}` / 401 `{valid:false}` |
| POST | `/api/auth/logout` | mencabut sesi (idempoten) |
| GET  | `/api/auth/me` | data akun (termasuk `role`) |
| GET  | `/health` | liveness; `/health/ready` memeriksa database |

Keamanan: password argon2id (m=19456 KiB, t=2, p=1), token sesi acak 32 byte yang
disimpan hanya sebagai SHA-256, sesi 7 hari (`SESSION_TTL_HOURS`), akun terkunci
15 menit setelah 5 kali gagal, batas laju per IP (login 10/menit, registrasi 5/jam),
Turnstile wajib untuk login dan registrasi, CORS hanya untuk `CORS_ALLOWED_ORIGINS`.

### Menjadikan akun sebagai admin
Tidak ada akun admin bawaan. Setelah akun didaftarkan lewat situs, promosikan dengan:
```bash
docker exec -it mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore -e \
  "UPDATE users SET role=\"admin\" WHERE username=\"USERNAME_ANDA\" AND deleted_at IS NULL; \
   SELECT username, email, role FROM users WHERE username=\"USERNAME_ANDA\";"'
```
Kembalikan ke pelanggan biasa dengan `role="customer"`. Membuka kunci akun:
`UPDATE users SET failed_logins=0, locked_until=NULL WHERE username="...";`

### Tes
```bash
# Unit test
docker run --rm -v "$PWD/backend":/src -w /src golang:1.27-alpine go test ./...
# Tes integrasi: butuh database *_test terpisah (DB_NAME harus berakhiran _test)
docker run --rm --network mysql-net -e DB_NAME=mihanstore_test -e DB_USER=... -e DB_PASSWORD=... \
  -v "$PWD/backend":/src -w /src golang:1.27-alpine go test -tags integration ./...
```

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

