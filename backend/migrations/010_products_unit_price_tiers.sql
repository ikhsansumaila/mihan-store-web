-- 010_products_unit_price_tiers.sql
-- Satuan jual produk + harga berjenjang (grosir) per produk.
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/010_products_unit_price_tiers.sql
--
-- - products.unit: satuan jual (huruf kecil, maks. 20, mis. pcs/pak/dus). Produk lama otomatis 'pcs'.
--   products.price tetap harga dasar (eceran).
-- - product_price_tiers: jenjang per produk. discount_type 'fixed' = value harga per unit (rupiah,
--   bilangan bulat); 'percent' = value diskon persen dari harga dasar (0,01–99,99).
--   Hapus = soft delete (deleted_at); `alive` (1 / NULL) membuat min_qty unik hanya di antara
--   jenjang yang belum dihapus. Jangan ditulis dari aplikasi. Data awal KOSONG (tanpa seed).
-- - Aturan harga & validasi (monoton turun, < harga dasar, maks. 20 jenjang) dijaga aplikasi
--   (backend/pricing.go); CHECK di bawah hanya pengaman dasar.
--
-- ROLLBACK (down) — kode lama tidak membaca kolom/tabel ini, jadi AMAN DIBIARKAN. Bila tetap ingin:
--   DROP TABLE product_price_tiers;
--   ALTER TABLE products DROP COLUMN unit;

ALTER TABLE products
  ADD COLUMN unit VARCHAR(20) NOT NULL DEFAULT 'pcs' AFTER price;

CREATE TABLE product_price_tiers (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  product_id BIGINT UNSIGNED NOT NULL,
  min_qty INT UNSIGNED NOT NULL,
  discount_type ENUM('fixed','percent') NOT NULL,
  value DECIMAL(12,2) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  UNIQUE KEY uq_tier_product_qty (product_id, min_qty, alive),
  KEY idx_product (product_id),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT chk_tiers_min_qty CHECK (min_qty >= 2),
  CONSTRAINT chk_tiers_value CHECK (value > 0),
  CONSTRAINT fk_tiers_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB;
