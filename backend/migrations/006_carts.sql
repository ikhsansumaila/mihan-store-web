-- 006_carts.sql
-- Keranjang belanja per pengguna (disimpan di database, bukan localStorage).
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/006_carts.sql
--
-- - Satu keranjang aktif per pengguna: UNIQUE (user_id, alive); `alive` = 1 bila belum
--   dihapus (soft delete), NULL bila sudah. Jangan ditulis dari aplikasi.
-- - Harga TIDAK disimpan di keranjang: selalu harga produk terkini. Harga dibekukan
--   (snapshot) baru saat checkout ke order_items.
-- - Baris cart_items boleh dihapus permanen (DELETE) saat produk dikeluarkan dari keranjang
--   atau keranjang dikosongkan setelah checkout. Hak DELETE hanya diberikan pada cart_items.
--
-- ROLLBACK (down): DROP TABLE cart_items; DROP TABLE carts;
--   (kode lama tidak membaca tabel ini; aman dibiarkan)

CREATE TABLE carts (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  UNIQUE KEY uq_cart_user (user_id, alive),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT fk_carts_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE cart_items (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  cart_id BIGINT UNSIGNED NOT NULL,
  product_id BIGINT UNSIGNED NOT NULL,
  qty INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  UNIQUE KEY uq_cart_product (cart_id, product_id, alive),
  KEY idx_product (product_id),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT chk_cart_items_qty CHECK (qty BETWEEN 1 AND 999),
  CONSTRAINT fk_cart_items_cart FOREIGN KEY (cart_id) REFERENCES carts(id),
  CONSTRAINT fk_cart_items_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB;
