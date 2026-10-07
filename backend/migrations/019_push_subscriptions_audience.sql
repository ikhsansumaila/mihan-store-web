-- 019_push_subscriptions_audience.sql
-- Web Push Tahap 2 (pelanggan): pisahkan audiens langganan.
--   audience = 'admin'    -> didaftarkan dari panel admin (/api/admin/push/subscribe); menerima notifikasi admin
--                            (pesanan baru / dibatalkan pelanggan) bila pemiliknya admin aktif di ADMIN_EMAILS.
--   audience = 'customer' -> didaftarkan dari toko (/api/push/subscribe); HANYA menerima notifikasi pesanan
--                            milik user_id-nya sendiri (ongkir dikonfirmasi, dibayar, selesai, dibatalkan admin).
-- Satu browser punya SATU endpoint push, jadi endpoint yang sama boleh punya dua baris (admin dan customer):
-- UNIQUE dipindah dari (endpoint_hash) ke (endpoint_hash, audience). Baris lama otomatis 'admin' (DEFAULT).
-- Hak DB tidak berubah: mihanstore_app sudah punya SELECT, INSERT, UPDATE, DELETE pada push_subscriptions (018).
-- Kompatibel dengan kode lama (INSERT lama tanpa kolom audience -> 'admin'; ON DUPLICATE KEY tetap kena
-- UNIQUE (endpoint_hash, 'admin')).
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/019_push_subscriptions_audience.sql
--
-- ROLLBACK (down) — langganan pelanggan ikut hilang:
--   DELETE FROM push_subscriptions WHERE audience = 'customer';
--   ALTER TABLE push_subscriptions DROP INDEX uq_push_endpoint_audience, ADD UNIQUE KEY uq_push_endpoint_hash (endpoint_hash),
--     DROP INDEX idx_push_audience_user, DROP COLUMN audience;

ALTER TABLE push_subscriptions
  ADD COLUMN audience ENUM('admin','customer') NOT NULL DEFAULT 'admin' AFTER user_id,
  ADD UNIQUE KEY uq_push_endpoint_audience (endpoint_hash, audience),
  ADD KEY idx_push_audience_user (audience, user_id),
  DROP INDEX uq_push_endpoint_hash;
