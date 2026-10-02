-- 008_site_settings.sql
-- Pengaturan toko yang bisa diubah admin lewat menu "Pengaturan Toko"
-- (nomor WhatsApp toko dan info rekening transfer manual).
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/008_site_settings.sql
--
-- Nilai awal sengaja placeholder "BELUM DIISI": aplikasi menganggapnya belum diatur
-- (tombol WhatsApp disembunyikan, halaman pesanan menampilkan peringatan netral).
-- Kunci: store_whatsapp, bank_name, bank_account_number, bank_account_holder, payment_note.
--
-- ROLLBACK (down): DROP TABLE site_settings;

CREATE TABLE site_settings (
  setting_key VARCHAR(64) NOT NULL PRIMARY KEY,
  setting_value VARCHAR(1000) NOT NULL DEFAULT '',
  updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT fk_site_settings_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

INSERT INTO site_settings (setting_key, setting_value) VALUES
  ('store_whatsapp', 'BELUM DIISI'),
  ('bank_name', 'BELUM DIISI'),
  ('bank_account_number', 'BELUM DIISI'),
  ('bank_account_holder', 'BELUM DIISI'),
  ('payment_note', 'BELUM DIISI');
