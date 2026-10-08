-- 020_orders_pending_confirmation.sql
-- Status baru "pending_confirmation" (Menunggu konfirmasi): checkout pelanggan -> menunggu admin
-- mengonfirmasi ongkir/diskon (POST /api/admin/orders/{id}/confirm) -> pending_payment -> paid -> completed.
-- Nilai ENUM DITAMBAH DI AKHIR daftar (perubahan metadata saja di MySQL 8, data lama tidak disentuh).
-- Pesanan lama berstatus pending_payment TIDAK diubah. DEFAULT orders.status sengaja tetap 'pending_payment'
-- (aplikasi selalu menulis status secara eksplisit; kode lama tetap berperilaku sama bila di-rollback).
-- Hak DB tidak berubah (kolom pada tabel yang sudah diberi hak di 009).
-- Dijalankan oleh root (setelah ~/mysql-stack/dump.sh):
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/020_orders_pending_confirmation.sql
--
-- ROLLBACK (down) — HANYA bila tidak ada lagi baris berstatus pending_confirmation (pindahkan/batalkan dulu,
-- mis. UPDATE orders SET status='pending_payment' WHERE status='pending_confirmation'; riwayat juga harus bebas
-- dari nilai itu), lalu:
--   ALTER TABLE orders MODIFY status ENUM('pending_payment','paid','completed','cancelled') NOT NULL DEFAULT 'pending_payment';
--   ALTER TABLE order_status_history
--     MODIFY from_status ENUM('pending_payment','paid','completed','cancelled') NULL,
--     MODIFY to_status ENUM('pending_payment','paid','completed','cancelled') NOT NULL;
-- Kode lama (sebelum 020) TIDAK mengenal status ini; jangan rollback image backend selagi masih ada pesanan
-- pending_confirmation.

ALTER TABLE orders
  MODIFY status ENUM('pending_payment','paid','completed','cancelled','pending_confirmation') NOT NULL DEFAULT 'pending_payment';

ALTER TABLE order_status_history
  MODIFY from_status ENUM('pending_payment','paid','completed','cancelled','pending_confirmation') NULL,
  MODIFY to_status ENUM('pending_payment','paid','completed','cancelled','pending_confirmation') NOT NULL;
