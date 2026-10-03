-- 014_orders_region.sql
-- Wilayah pengiriman pada pesanan: kode dan NAMA (salinan dari data wilayah saat checkout, bukan dari
-- klien). Semua kolom NULL agar pesanan lama tetap valid dan tampil seperti sebelumnya.
-- Pesanan baru: city = nama kab/kota (kompatibel dengan kode lama), address = alamat lengkap
-- (jalan, RT/RW, nomor), postal_code wajib 5 digit (dijaga aplikasi).
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/014_orders_region.sql
--
-- ROLLBACK (down) — kode lama tidak membaca kolom ini (INSERT-nya tidak menyebut kolom ini), jadi AMAN
-- DIBIARKAN. Bila tetap ingin (data wilayah pesanan baru ikut hilang; city/address tetap ada):
--   ALTER TABLE orders DROP COLUMN province_code, DROP COLUMN province_name, DROP COLUMN regency_code,
--     DROP COLUMN regency_name, DROP COLUMN district_code, DROP COLUMN district_name,
--     DROP COLUMN village_code, DROP COLUMN village_name;

ALTER TABLE orders
  ADD COLUMN province_code VARCHAR(2) NULL AFTER postal_code,
  ADD COLUMN province_name VARCHAR(100) NULL AFTER province_code,
  ADD COLUMN regency_code VARCHAR(5) NULL AFTER province_name,
  ADD COLUMN regency_name VARCHAR(100) NULL AFTER regency_code,
  ADD COLUMN district_code VARCHAR(8) NULL AFTER regency_name,
  ADD COLUMN district_name VARCHAR(100) NULL AFTER district_code,
  ADD COLUMN village_code VARCHAR(13) NULL AFTER district_name,
  ADD COLUMN village_name VARCHAR(100) NULL AFTER village_code;
