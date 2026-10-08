-- 026_drop_region_shipping_defaults_district.sql
-- Menghapus tabel cadangan default ongkir per KECAMATAN (region_shipping_defaults_district_025, dibuat oleh 025).
-- Jalankan HANYA setelah isi tabel baru region_shipping_defaults (kelurahan) diverifikasi benar.
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/026_drop_region_shipping_defaults_district.sql
--
-- ROLLBACK (down): lihat header 025 langkah (b) (kembalikan dari dump pra-migrasi).

DROP TABLE IF EXISTS region_shipping_defaults_district_025;
