-- 022_grants_region_shipping_defaults.sql
-- Hak akses per tabel untuk region_shipping_defaults (021). User aplikasi TIDAK punya hak tingkat database
-- (lihat 005/009/012/015/018), jadi tabel baru wajib diberi hak eksplisit.
--
--   region_shipping_defaults   SELECT, INSERT, UPDATE, DELETE
--
-- Hanya menambah hak; kompatibel dengan kode lama. Dijalankan oleh root SETELAH 021:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/022_grants_region_shipping_defaults.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`region_shipping_defaults` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';

GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.`region_shipping_defaults` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
