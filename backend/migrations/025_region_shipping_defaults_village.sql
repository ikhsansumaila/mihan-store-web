-- 025_region_shipping_defaults_village.sql
-- Default ongkir wilayah dipindah dari KECAMATAN ke KELURAHAN/DESA (radius lebih kecil).
--
-- Langkah:
--   1. Tabel lama (kunci district_code, 021) di-RENAME menjadi region_shipping_defaults_district_025 (cadangan).
--   2. Tabel baru region_shipping_defaults dibuat dengan kunci village_code (+ info kecamatan/kab-kota).
--      Hak tabel mihanstore_app (022) melekat pada NAMA tabel di mysql.tables_priv dan tidak ikut RENAME, jadi
--      tabel baru dengan nama yang sama otomatis memakai hak SELECT/INSERT/UPDATE/DELETE yang sudah ada.
--   3. Data lama dikonversi (keputusan pemilik, opsi 2): setiap default kecamatan dipindah ke KELURAHAN pesanan
--      sumbernya. Pesanan sumber = orderNo pada log aktivitas region.shipping_default_set TERAKHIR untuk kecamatan
--      itu; kode/nama kelurahan, kecamatan, kab/kota diambil dari baris pesanan tersebut (bukan diketik manual).
--      Nominal, updated_by, created_at, updated_at dipertahankan. Saat migrasi produksi (2026-10-08) isinya:
--        17.03.21 Arma Jaya       Rp 10.000 (MS-261008-0036) -> 17.03.21.2003 Sidodadi
--        31.72.06 Kelapa Gading   Rp 63.000 (MS-261008-0038) -> 31.72.06.1003 Kelapa Gading Barat
--      (Isi tabel lama diverifikasi persis 2 baris tersebut SEBELUM migrasi dijalankan.)
--   4. Satu log aktivitas region.shipping_default_migrate mencatat pemindahan (hanya bila ada baris dipindah).
--   5. Indeks orders (village_code, created_at) untuk saran "ongkir terakhir ke kelurahan".
-- Tabel cadangan region_shipping_defaults_district_025 dihapus oleh 026 SETELAH verifikasi isi tabel baru.
--
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/025_region_shipping_defaults_village.sql
--
-- ROLLBACK (down) — kembali ke default per kecamatan (kode lama, 021):
--   (a) bila 026 BELUM dijalankan:
--       DROP TABLE region_shipping_defaults;
--       RENAME TABLE region_shipping_defaults_district_025 TO region_shipping_defaults;
--   (b) bila 026 SUDAH dijalankan: buat ulang tabel lama dari dump pra-migrasi
--       (~/mihanstore_pre_village_default_backup/mysql-all-*-pre-village-default.sql.gz), mis.:
--       DROP TABLE region_shipping_defaults;
--       zcat <dump> | sed -n '/^USE `mihanstore`;/,/^USE `/p' \
--         | sed -n '/^DROP TABLE IF EXISTS `region_shipping_defaults`;/,/^UNLOCK TABLES;/p' \
--         | docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore'
--       (mengembalikan 2 baris kecamatan lama persis seperti sebelum migrasi; hak 022 tetap melekat pada nama).
--   Indeks boleh dibiarkan; bila ingin: ALTER TABLE orders DROP INDEX idx_orders_village_created;

RENAME TABLE region_shipping_defaults TO region_shipping_defaults_district_025;

CREATE TABLE region_shipping_defaults (
  village_code VARCHAR(13) NOT NULL PRIMARY KEY,
  village_name VARCHAR(100) NOT NULL,
  district_code VARCHAR(8) NULL,
  district_name VARCHAR(100) NULL,
  regency_code VARCHAR(5) NULL,
  regency_name VARCHAR(100) NULL,
  shipping_fee INT UNSIGNED NOT NULL,
  updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_region_shipping_district (district_code),
  CONSTRAINT chk_region_shipping_village_fee CHECK (shipping_fee > 0 AND shipping_fee <= 10000000),
  CONSTRAINT fk_region_shipping_village_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

-- Konversi: kecamatan -> kelurahan pesanan sumber (log region.shipping_default_set terakhir per kecamatan).
INSERT INTO region_shipping_defaults
  (village_code, village_name, district_code, district_name, regency_code, regency_name, shipping_fee, updated_by, created_at, updated_at)
SELECT o.village_code, o.village_name, o.district_code, o.district_name, o.regency_code, o.regency_name,
       d.shipping_fee, d.updated_by, d.created_at, d.updated_at
FROM region_shipping_defaults_district_025 d
JOIN activity_logs a ON a.id = (
  SELECT MAX(a2.id) FROM activity_logs a2
  WHERE a2.action = 'region.shipping_default_set' AND a2.entity_id = d.district_code
)
JOIN orders o ON o.order_no = JSON_UNQUOTE(JSON_EXTRACT(a.details, '$.orderNo'))
WHERE o.district_code = d.district_code AND o.village_code IS NOT NULL AND o.village_name IS NOT NULL;

INSERT INTO activity_logs (user_id, actor_label, action, entity_type, entity_id, summary, details, created_at)
SELECT NULL, 'sistem (migrasi 025)', 'region.shipping_default_migrate', 'region', NULL,
       CONCAT(COUNT(*), ' default ongkir kecamatan dipindah ke kelurahan'),
       JSON_OBJECT('jumlah', COUNT(*), 'pemindahan', JSON_ARRAYAGG(JSON_OBJECT(
         'kecamatan', v.district_name, 'kodeKecamatan', v.district_code,
         'kelurahan', v.village_name, 'kodeKelurahan', v.village_code, 'ongkir', v.shipping_fee))),
       UTC_TIMESTAMP(3)
FROM region_shipping_defaults v
HAVING COUNT(*) > 0;

ALTER TABLE orders ADD KEY idx_orders_village_created (village_code, created_at);

-- Ringkasan untuk diperiksa operator.
SELECT 'lama' AS tabel, district_code AS kode, district_name AS nama, shipping_fee FROM region_shipping_defaults_district_025
UNION ALL
SELECT 'baru', village_code, village_name, shipping_fee FROM region_shipping_defaults;
