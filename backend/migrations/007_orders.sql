-- 007_orders.sql
-- Pesanan (orders), item pesanan (snapshot harga), dan riwayat status.
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/007_orders.sql
--
-- Catatan:
-- - Semua nominal dalam rupiah (bilangan bulat). total = subtotal - discount + shipping_fee;
--   dihitung di server dan dijaga CHECK (ditulis tanpa pengurangan agar aman untuk UNSIGNED).
-- - order_no dibuat aplikasi SETELAH insert (butuh id) di transaksi yang sama, format
--   MS-YYMMDD-<id 4 digit>, mis. MS-261002-0001. NULL hanya sesaat di dalam transaksi itu.
-- - Status hanya 4: pending_payment -> paid -> completed, atau -> cancelled (final).
--   Aplikasi memakai UPDATE bersyarat (WHERE status = status_lama) untuk mencegah tabrakan.
-- - idempotency_key: UNIQUE per pengguna, checkout ganda dengan kunci yang sama
--   mengembalikan pesanan yang sama.
-- - order_items dan order_status_history bersifat append-only bagi user aplikasi
--   (hanya SELECT/INSERT, lihat 009_grants_orders.sql).
--
-- ROLLBACK (down): DROP TABLE order_status_history; DROP TABLE order_items; DROP TABLE orders;
--   (kode lama tidak membaca tabel ini; aman dibiarkan. Hapus HANYA bila data pesanan
--   memang tidak diperlukan lagi, setelah dump cadangan.)

CREATE TABLE orders (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_no VARCHAR(24) NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  status ENUM('pending_payment','paid','completed','cancelled') NOT NULL DEFAULT 'pending_payment',
  subtotal INT UNSIGNED NOT NULL,
  discount INT UNSIGNED NOT NULL DEFAULT 0,
  discount_note VARCHAR(255) NULL,
  shipping_fee INT UNSIGNED NOT NULL DEFAULT 0,
  total INT UNSIGNED NOT NULL,
  payment_method VARCHAR(30) NOT NULL DEFAULT 'bank_transfer',
  recipient_name VARCHAR(100) NOT NULL,
  recipient_phone VARCHAR(20) NOT NULL,
  address VARCHAR(500) NOT NULL,
  city VARCHAR(100) NOT NULL,
  postal_code VARCHAR(10) NULL,
  customer_note VARCHAR(500) NULL,
  admin_note VARCHAR(500) NULL,
  idempotency_key CHAR(36) NULL,
  paid_at DATETIME(3) NULL,
  paid_by BIGINT UNSIGNED NULL,
  payment_note VARCHAR(255) NULL,
  completed_at DATETIME(3) NULL,
  cancelled_at DATETIME(3) NULL,
  cancelled_by BIGINT UNSIGNED NULL,
  cancel_reason VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  UNIQUE KEY uq_order_no (order_no),
  UNIQUE KEY uq_user_idem (user_id, idempotency_key),
  KEY idx_status_created (status, created_at),
  KEY idx_user_created (user_id, created_at),
  KEY idx_created (created_at),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT chk_orders_discount CHECK (discount <= subtotal),
  CONSTRAINT chk_orders_shipping CHECK (shipping_fee <= 10000000),
  CONSTRAINT chk_orders_total CHECK (total + discount = subtotal + shipping_fee),
  CONSTRAINT fk_orders_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_orders_paid_by FOREIGN KEY (paid_by) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_orders_cancelled_by FOREIGN KEY (cancelled_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE order_items (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT UNSIGNED NOT NULL,
  product_id BIGINT UNSIGNED NULL,
  product_name VARCHAR(150) NOT NULL,
  unit_price INT UNSIGNED NOT NULL,
  qty INT UNSIGNED NOT NULL,
  line_total INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_order (order_id),
  KEY idx_product (product_id),
  CONSTRAINT chk_order_items_qty CHECK (qty BETWEEN 1 AND 999),
  CONSTRAINT chk_order_items_line CHECK (line_total = unit_price * qty),
  CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_order_items_product FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE order_status_history (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT UNSIGNED NOT NULL,
  from_status ENUM('pending_payment','paid','completed','cancelled') NULL,
  to_status ENUM('pending_payment','paid','completed','cancelled') NOT NULL,
  actor_user_id BIGINT UNSIGNED NULL,
  actor_label VARCHAR(100) NOT NULL,
  note VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_order_created (order_id, created_at),
  KEY idx_actor (actor_user_id),
  CONSTRAINT fk_osh_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_osh_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;
