-- 023_order_payment_proofs.sql
-- Bukti transfer (konfirmasi pembayaran) pelanggan: maksimal SATU per pesanan (UNIQUE order_id).
-- Berkas disimpan PRIVAT di disk (PAYMENT_PROOF_DIR, di luar /uploads publik, tanpa URL statis); tabel hanya
-- menyimpan kunci berkas acak (UUID, tanpa nama asli), tipe, ukuran, dan SHA-256 hasil proses ulang.
-- Status pesanan TIDAK berubah karena unggahan (tetap pending_payment sampai admin menandai Dibayar).
-- Mengganti bukti = UPDATE baris (berkas lama dihapus); pelanggan menghapus bukti selama pending_payment =
-- DELETE baris + berkas. Retensi: dihapus otomatis (berkas + baris) 180 hari setelah pesanan selesai/dibatalkan
-- oleh backend (goroutine harian). Tanpa soft delete.
-- Pencarian purge memakai JOIN ke orders lewat uq_payment_proof_order; tabel kecil (maks. 1 baris per pesanan),
-- jadi tidak ada indeks tambahan pada orders.
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh), lalu 024 untuk hak akses:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/023_order_payment_proofs.sql
--
-- ROLLBACK (down) — kode lama tidak membaca tabel ini, jadi AMAN DIBIARKAN. Bila tetap ingin (semua bukti
-- hilang; hapus juga folder privatnya):
--   DROP TABLE order_payment_proofs;   (setelah REVOKE di 024)

CREATE TABLE order_payment_proofs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  file_key VARCHAR(100) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  mime VARCHAR(50) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  size_bytes INT UNSIGNED NOT NULL,
  width INT UNSIGNED NOT NULL,
  height INT UNSIGNED NOT NULL,
  sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_payment_proof_order (order_id),
  UNIQUE KEY uq_payment_proof_file (file_key),
  KEY idx_payment_proof_user (user_id),
  CONSTRAINT fk_payment_proof_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
  CONSTRAINT fk_payment_proof_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT chk_payment_proof_size CHECK (size_bytes > 0)
) ENGINE=InnoDB;
