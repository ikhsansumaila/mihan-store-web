-- 024_grants_order_payment_proofs.sql
-- Hak akses per tabel untuk order_payment_proofs (023). User aplikasi TIDAK punya hak tingkat database
-- (lihat 005/009/012/015/018/022), jadi tabel baru wajib diberi hak eksplisit.
--
--   order_payment_proofs   SELECT, INSERT, UPDATE, DELETE   (ganti = UPDATE; hapus pelanggan & purge 180 hari = DELETE)
--
-- Hanya menambah hak; kompatibel dengan kode lama. Dijalankan oleh root SETELAH 023:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/024_grants_order_payment_proofs.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`order_payment_proofs` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';

GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.`order_payment_proofs` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
