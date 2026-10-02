-- 009_grants_orders.sql
-- Hak akses per tabel (least privilege) untuk tabel pesanan. User aplikasi TIDAK punya hak
-- tingkat database (lihat 005), jadi setiap tabel baru wajib diberi hak eksplisit.
--
--   carts                 SELECT, INSERT, UPDATE            (soft delete)
--   cart_items            SELECT, INSERT, UPDATE, DELETE    (baris keranjang boleh dihapus)
--   orders                SELECT, INSERT, UPDATE            (tanpa DELETE)
--   order_items           SELECT, INSERT                    (snapshot, tidak bisa diubah)
--   order_status_history  SELECT, INSERT                    (append-only)
--   site_settings         SELECT, INSERT, UPDATE
--
-- Hanya menambah hak; tidak mengubah hak tabel lain (kompatibel dengan kode lama).
-- Dijalankan oleh root SETELAH 006–008:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/009_grants_orders.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`carts` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';
--   (ulangi untuk cart_items, orders, order_items, order_status_history, site_settings)

GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`carts` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.`cart_items` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`orders` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT ON `mihanstore`.`order_items` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT ON `mihanstore`.`order_status_history` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`site_settings` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
