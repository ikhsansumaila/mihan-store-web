-- 017_push_subscriptions.sql
-- Langganan Web Push (notifikasi pesanan baru / dibatalkan pelanggan) untuk ADMIN saja.
-- Satu baris = satu browser/perangkat admin. Pemilik langganan = baris users admin (user_id) yang dibuat/
-- dibaca ensureAdminUser dari email Cloudflare Access; saat mengirim, backend hanya memakai langganan milik
-- users yang role='admin', status='active', belum dihapus, DAN email-nya masih ada di ADMIN_EMAILS.
--
-- endpoint: URL layanan push (berisi token perangkat; perlakukan seperti rahasia, jangan dicatat di log).
--   Hanya https ke layanan push yang dikenal (divalidasi aplikasi, anti-SSRF). Maks. 2000 karakter ASCII.
-- endpoint_hash: SHA-256 hex dari endpoint, UNIQUE (endpoint terlalu panjang untuk diindeks langsung).
--   Endpoint yang sama didaftarkan ulang = UPSERT (kunci diperbarui, pemilik = admin yang mendaftar).
-- p256dh / auth: kunci publik langganan dari browser (base64url), bukan rahasia server.
-- Langganan kedaluwarsa (layanan push menjawab 404/410) atau dimatikan admin DIHAPUS (DELETE),
-- jadi tanpa soft delete / kolom alive. Maks. 10 langganan per admin (yang terlama dibuang aplikasi).
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh), lalu 018 untuk hak akses:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/017_push_subscriptions.sql
--
-- ROLLBACK (down) — kode lama tidak membaca tabel ini, jadi AMAN DIBIARKAN. Bila tetap ingin
-- (semua langganan hilang; admin perlu menekan "Aktifkan notifikasi" lagi):
--   DROP TABLE push_subscriptions;   (setelah REVOKE di 018)

CREATE TABLE push_subscriptions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  endpoint VARCHAR(2000) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  endpoint_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  p256dh VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  auth VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  user_agent VARCHAR(200) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_used_at DATETIME(3) NULL,
  UNIQUE KEY uq_push_endpoint_hash (endpoint_hash),
  KEY idx_push_user_created (user_id, created_at),
  CONSTRAINT fk_push_subscriptions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB;
