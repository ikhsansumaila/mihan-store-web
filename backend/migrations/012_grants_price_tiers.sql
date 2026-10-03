-- 012_grants_price_tiers.sql
-- Hak akses per tabel untuk tabel baru product_price_tiers. User aplikasi TIDAK punya hak tingkat
-- database (lihat 005/009), jadi tabel baru wajib diberi hak eksplisit.
--
--   product_price_tiers   SELECT, INSERT, UPDATE   (hapus = soft delete lewat deleted_at, tanpa DELETE)
--
-- Kolom baru di products / cart_items / order_items (010, 011) otomatis tercakup hak tabel yang sudah
-- ada (order_items tetap SELECT, INSERT saja). Hanya menambah hak; kompatibel dengan kode lama.
-- Dijalankan oleh root SETELAH 010–011:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/012_grants_price_tiers.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`product_price_tiers` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';

GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`product_price_tiers` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
