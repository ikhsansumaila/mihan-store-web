-- 018_grants_push_subscriptions.sql
-- Hak akses per tabel untuk tabel push_subscriptions (017). User aplikasi TIDAK punya hak tingkat
-- database (lihat 005/009/012/015), jadi tabel baru wajib diberi hak eksplisit.
--
--   push_subscriptions   SELECT, INSERT, UPDATE, DELETE   (langganan kedaluwarsa/dimatikan dihapus; tanpa soft delete)
--
-- Hanya menambah hak; kompatibel dengan kode lama. Dijalankan oleh root SETELAH 017:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/018_grants_push_subscriptions.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`push_subscriptions` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';

GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.`push_subscriptions` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
