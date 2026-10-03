-- 013_regions.sql
-- Data wilayah Indonesia (provinsi, kab/kota, kecamatan, kelurahan/desa) dari https://wilayah.id/api
-- dalam format JSON, satu baris per "dataset induk", plus riwayat impor dan tabel staging.
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/013_regions.sql
--
-- region_datasets (data hidup):
--   dataset_key: 'provinces' | 'regencies:<kodeProv>' | 'districts:<kodeKabKota>' | 'villages:<kodeKec>'
--   data: array [{code, name}] seperti dari API. Pembaruan = UPSERT di tempat oleh aplikasi (hanya baris
--   yang content_hash-nya berubah di-UPDATE; kunci yang hilang di sumber di-soft-delete), jadi tabel
--   tidak membengkak. `alive` (1 / NULL) membuat dataset_key unik hanya di antara baris hidup.
-- region_import_runs: satu baris per fetch (admin atau CLI). `active_lock` (1 bila running/staged)
--   + UNIQUE menjamin hanya SATU run berjalan/menunggu keputusan pada satu waktu (lintas proses).
-- region_import_staging: hasil fetch sementara sampai admin menekan Simpan/Batal; baris dihapus
--   (DELETE) setelah disimpan atau dibuang, jadi tanpa soft delete.
-- Kolom generated (alive, active_lock) jangan ditulis aplikasi.
--
-- ROLLBACK (down) — kode lama tidak membaca tabel ini, jadi AMAN DIBIARKAN. Bila tetap ingin:
--   DROP TABLE region_import_staging; DROP TABLE region_datasets; DROP TABLE region_import_runs;
--   (setelah 014 di-rollback bila perlu; pesanan menyimpan salinan nama wilayah sendiri)

CREATE TABLE region_import_runs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  status ENUM('running','staged','saved','discarded','failed','cancelled') NOT NULL DEFAULT 'running',
  started_by BIGINT UNSIGNED NULL,
  started_label VARCHAR(100) NOT NULL,
  started_at DATETIME(3) NOT NULL,
  finished_at DATETIME(3) NULL,
  saved_at DATETIME(3) NULL,
  saved_by BIGINT UNSIGNED NULL,
  saved_label VARCHAR(100) NULL,
  progress_done INT UNSIGNED NOT NULL DEFAULT 0,
  progress_total INT UNSIGNED NOT NULL DEFAULT 0,
  counts JSON NULL,
  diff JSON NULL,
  stats JSON NULL,
  source_updated_at DATE NULL,
  error VARCHAR(500) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  active_lock TINYINT GENERATED ALWAYS AS (IF(status IN ('running','staged') AND deleted_at IS NULL, 1, NULL)) STORED,
  UNIQUE KEY uq_region_runs_active (active_lock),
  KEY idx_status (status),
  KEY idx_started (started_at),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT fk_region_runs_started_by FOREIGN KEY (started_by) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_region_runs_saved_by FOREIGN KEY (saved_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE region_datasets (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  dataset_key VARCHAR(40) NOT NULL,
  level TINYINT UNSIGNED NOT NULL,
  parent_code VARCHAR(20) NULL,
  data JSON NOT NULL,
  item_count INT UNSIGNED NOT NULL,
  content_hash CHAR(64) NOT NULL,
  source_updated_at DATE NULL,
  imported_run_id BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED,
  UNIQUE KEY uq_region_dataset_key (dataset_key, alive),
  KEY idx_level_parent (level, parent_code),
  KEY idx_deleted_at (deleted_at),
  CONSTRAINT chk_region_level CHECK (level BETWEEN 1 AND 4),
  CONSTRAINT chk_region_data_array CHECK (JSON_TYPE(data) = 'ARRAY'),
  CONSTRAINT fk_region_datasets_run FOREIGN KEY (imported_run_id) REFERENCES region_import_runs(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE region_import_staging (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  run_id BIGINT UNSIGNED NOT NULL,
  dataset_key VARCHAR(40) NOT NULL,
  level TINYINT UNSIGNED NOT NULL,
  parent_code VARCHAR(20) NULL,
  data JSON NOT NULL,
  item_count INT UNSIGNED NOT NULL,
  content_hash CHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_staging_run_key (run_id, dataset_key),
  CONSTRAINT chk_staging_level CHECK (level BETWEEN 1 AND 4),
  CONSTRAINT chk_staging_data_array CHECK (JSON_TYPE(data) = 'ARRAY'),
  CONSTRAINT fk_staging_run FOREIGN KEY (run_id) REFERENCES region_import_runs(id)
) ENGINE=InnoDB;
