-- 005_grants_per_table.sql
-- Hak akses user aplikasi per tabel agar activity_logs benar-benar append-only.
--
-- Hak tingkat database (`mihanstore`.*) tidak bisa "dikurangi" per tabel, jadi hak itu
-- DICABUT dan diganti hak per tabel. GRANT dijalankan lebih dulu, REVOKE terakhir,
-- sehingga backend yang sedang berjalan tidak pernah kehilangan akses ke users/sessions.
-- Hak tingkat tabel berlaku untuk koneksi yang sudah terbuka pada statement berikutnya.
--
--   users       SELECT, INSERT, UPDATE           (hapus akun = soft delete / UPDATE)
--   sessions    SELECT, INSERT, UPDATE, DELETE   (DELETE untuk membersihkan sesi kedaluwarsa)
--   categories  SELECT, INSERT, UPDATE           (soft delete)
--   products    SELECT, INSERT, UPDATE           (soft delete)
--   activity_logs  SELECT, INSERT SAJA           (tanpa UPDATE/DELETE)
--   purge_activity_logs  EXECUTE                 (satu-satunya jalan menghapus log > 180 hari)
--
-- Kompatibel dengan kode lama (hanya memakai users + sessions).
-- Dijalankan oleh root SETELAH 002–004:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' < backend/migrations/005_grants_per_table.sql
--
-- ROLLBACK (down) ke hak lama:
--   GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.* TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
--   lalu REVOKE hak per tabel/procedure di bawah (opsional).

GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`users` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.`sessions` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`categories` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT, UPDATE ON `mihanstore`.`products` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT SELECT, INSERT ON `mihanstore`.`activity_logs` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
GRANT EXECUTE ON PROCEDURE `mihanstore`.`purge_activity_logs` TO 'mihanstore_app'@'172.20.0.0/255.255.0.0';
REVOKE SELECT, INSERT, UPDATE, DELETE ON `mihanstore`.* FROM 'mihanstore_app'@'172.20.0.0/255.255.0.0';
SHOW GRANTS FOR 'mihanstore_app'@'172.20.0.0/255.255.0.0';
