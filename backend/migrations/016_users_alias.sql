-- 016_users_alias.sql
-- Alias pelanggan: nama panggilan/label internal (mis. "Bu Siti Toko Maju") yang HANYA dilihat admin.
-- Boleh kosong (NULL), boleh sama antar pelanggan (tanpa UNIQUE), maks. 100 karakter setelah dirapikan
-- aplikasi (karakter kontrol/bidi dibuang, spasi dirapikan). Tidak pernah dikirim ke API pelanggan.
-- Hak DB tidak berubah: mihanstore_app sudah punya SELECT, INSERT, UPDATE pada users (005).
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/016_users_alias.sql
--
-- ROLLBACK (down) — kode lama tidak membaca kolom ini dan INSERT-nya tidak menyebut kolom ini, jadi AMAN
-- DIBIARKAN. Bila tetap ingin dihapus (semua alias ikut hilang):
--   ALTER TABLE users DROP COLUMN alias;

ALTER TABLE users
  ADD COLUMN alias VARCHAR(100) NULL AFTER name;
