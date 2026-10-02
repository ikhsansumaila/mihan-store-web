-- 001_users_sessions.sql
-- Skema autentikasi MihanStore (users + sessions).
-- Dijalankan oleh root MySQL di dalam container mysql_db (user aplikasi tidak punya hak DDL):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot mihanstore' < backend/migrations/001_users_sessions.sql
-- Kolom `alive` dihitung otomatis oleh MySQL (1 = aktif, NULL = soft-deleted) agar
-- email/username unik hanya di antara akun yang belum dihapus. Jangan ditulis dari aplikasi.

CREATE TABLE users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  public_id CHAR(36) NOT NULL UNIQUE,
  username VARCHAR(30) NOT NULL,
  email VARCHAR(254) NOT NULL,
  phone VARCHAR(20) NULL,
  name VARCHAR(100) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role ENUM('customer','admin') NOT NULL DEFAULT 'customer',
  status ENUM('active','suspended') NOT NULL DEFAULT 'active',
  email_verified_at DATETIME(3) NULL,
  failed_logins TINYINT UNSIGNED NOT NULL DEFAULT 0,
  locked_until DATETIME(3) NULL,
  last_login_at DATETIME(3) NULL,
  password_changed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  UNIQUE KEY uq_email (email, alive),
  UNIQUE KEY uq_username (username, alive),
  KEY idx_deleted_at (deleted_at)
) ENGINE=InnoDB;
CREATE TABLE sessions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  token_hash CHAR(64) NOT NULL UNIQUE,
  user_agent VARCHAR(255) NULL,
  ip VARBINARY(16) NULL,
  expires_at DATETIME(3) NOT NULL,
  revoked_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_user (user_id), KEY idx_exp (expires_at), KEY idx_deleted_at (deleted_at),
  FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;
