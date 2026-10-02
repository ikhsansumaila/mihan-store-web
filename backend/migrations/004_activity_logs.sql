-- 004_activity_logs.sql
-- Log aktivitas (append-only bagi aplikasi) + procedure penghapusan log > 180 hari.
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/004_activity_logs.sql
--
-- Sengaja TANPA foreign key ke users agar log tetap utuh walau akun dihapus.
-- User aplikasi hanya punya SELECT + INSERT pada tabel ini (lihat 005_grants_per_table.sql);
-- satu-satunya jalan menghapus log adalah CALL purge_activity_logs(@n), yang berjalan
-- dengan hak DEFINER (root) dan HANYA menghapus baris lebih tua dari 180 hari.
-- created_at disimpan dalam UTC (aplikasi memakai time_zone '+00:00'), jadi batasnya
-- dihitung dengan UTC_TIMESTAMP(), tidak bergantung zona waktu sesi.
--
-- ROLLBACK (down): DROP PROCEDURE purge_activity_logs; DROP TABLE activity_logs;

CREATE TABLE activity_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NULL,
  actor_label VARCHAR(100) NULL,
  action VARCHAR(50) NOT NULL,
  entity_type VARCHAR(50) NULL,
  entity_id VARCHAR(64) NULL,
  summary VARCHAR(255) NOT NULL,
  details JSON NULL,
  ip VARBINARY(16) NULL,
  user_agent VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),
  deleted_at DATETIME(3) NULL,
  KEY idx_created (created_at),
  KEY idx_user_created (user_id, created_at),
  KEY idx_entity (entity_type, entity_id),
  KEY idx_action_created (action, created_at)
) ENGINE=InnoDB;

DROP PROCEDURE IF EXISTS purge_activity_logs;
DELIMITER //
CREATE DEFINER=`root`@`localhost` PROCEDURE purge_activity_logs(OUT deleted_count BIGINT)
  MODIFIES SQL DATA
  SQL SECURITY DEFINER
  COMMENT 'Hapus activity_logs yang lebih tua dari 180 hari'
BEGIN
  DELETE FROM activity_logs WHERE created_at < UTC_TIMESTAMP(3) - INTERVAL 180 DAY;
  SET deleted_count = ROW_COUNT();
END//
DELIMITER ;
