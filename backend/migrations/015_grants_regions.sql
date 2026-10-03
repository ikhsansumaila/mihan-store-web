-- 015_grants_regions.sql
-- Hak akses per tabel untuk tabel data wilayah. User aplikasi TIDAK punya hak tingkat database
-- (lihat 005/009/012), jadi tabel baru wajib diberi hak eksplisit.
--
--   region_datasets        SELECT, INSERT, UPDATE   (hilang di sumber = soft delete lewat deleted_at)
--   region_import_runs     SELECT, INSERT, UPDATE   (riwayat run, tanpa DELETE)
--   region_import_staging  SELECT, INSERT, DELETE   (staging dihapus setelah Simpan/Batal)
--
-- Kolom baru di orders (014) otomatis tercakup hak tabel orders yang sudah ada.
-- Hanya menambah hak; kompatibel dengan kode lama. Dijalankan oleh root SETELAH 013–014:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/015_grants_regions.sql
--
-- ROLLBACK (down):
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`region_datasets` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`region_import_runs` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';
--   REVOKE ALL PRIVILEGES ON `mihanstore`.`region_import_staging` FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';

GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`region_datasets` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`region_import_runs` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, DELETE ON `mihanstore`.`region_import_staging` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
