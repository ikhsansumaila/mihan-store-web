-- 021_region_shipping_defaults.sql
-- Default ongkir per kecamatan (diisi admin lewat checklist "Tetapkan sebagai default ongkir wilayah ini" di
-- dialog Konfirmasi pesanan; belum ada halaman pengelolaan). Dipakai sebagai saran pertama ongkir
-- (GET /api/admin/orders/{id}/shipping-suggestions). shipping_fee 1..10.000.000 dijaga aplikasi (Rp 0 tidak
-- disimpan sebagai default). Baris boleh dihapus (DELETE) bila kelak ada pengelolaan; tanpa soft delete.
-- Indeks orders (district_code, created_at) & (regency_code, created_at) untuk mencari "ongkir terakhir" ke
-- kecamatan / kab-kota yang sama; "ongkir terakhir pelanggan" memakai indeks lama idx_user_created.
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh), lalu 022 untuk hak akses:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/021_region_shipping_defaults.sql
--
-- ROLLBACK (down) — kode lama tidak membaca tabel/indeks ini, jadi AMAN DIBIARKAN. Bila tetap ingin
-- (semua default ongkir hilang):
--   DROP TABLE region_shipping_defaults;   (setelah REVOKE di 022)
--   ALTER TABLE orders DROP INDEX idx_orders_district_created, DROP INDEX idx_orders_regency_created;

CREATE TABLE region_shipping_defaults (
  district_code VARCHAR(8) NOT NULL PRIMARY KEY,
  district_name VARCHAR(100) NOT NULL,
  regency_code VARCHAR(5) NULL,
  regency_name VARCHAR(100) NULL,
  shipping_fee INT UNSIGNED NOT NULL,
  updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  CONSTRAINT chk_region_shipping_fee CHECK (shipping_fee > 0 AND shipping_fee <= 10000000),
  CONSTRAINT fk_region_shipping_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

ALTER TABLE orders
  ADD KEY idx_orders_district_created (district_code, created_at),
  ADD KEY idx_orders_regency_created (regency_code, created_at);
