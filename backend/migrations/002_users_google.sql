-- 002_users_google.sql
-- Login Google untuk pelanggan: kolom google_sub + avatar_url, password_hash boleh NULL
-- (akun Google tidak punya password).
-- Dijalankan oleh root MySQL di dalam container mysql_db:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/002_users_google.sql
--
-- ROLLBACK (down) — hanya bila belum ada akun tanpa password:
--   SELECT COUNT(*) FROM users WHERE password_hash IS NULL;   -- harus 0
--   ALTER TABLE users DROP INDEX uq_google_sub, DROP COLUMN avatar_url, DROP COLUMN google_sub,
--     MODIFY password_hash VARCHAR(255) NOT NULL;
-- Kode lama (sebelum fitur ini) tetap berjalan dengan skema baru selama tidak ada
-- password_hash NULL yang dicoba login (verifikasi gagal dengan aman).

ALTER TABLE users
  ADD COLUMN google_sub VARCHAR(64) NULL AFTER password_hash,
  ADD COLUMN avatar_url VARCHAR(500) NULL AFTER google_sub,
  MODIFY password_hash VARCHAR(255) NULL,
  ADD UNIQUE KEY uq_google_sub (google_sub, alive);
