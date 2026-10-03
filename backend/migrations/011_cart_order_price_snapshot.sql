-- 011_cart_order_price_snapshot.sql
-- Kolom tambahan untuk harga berjenjang di keranjang dan snapshot pesanan.
-- Dijalankan oleh root SETELAH 010:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/011_cart_order_price_snapshot.sql
--
-- - cart_items.seen_unit_price: harga satuan efektif yang terakhir DILIHAT pelanggan untuk baris
--   itu (penanda "harga berubah"). NULL = belum pernah dilihat (diisi saat keranjang dibuka).
-- - order_items (snapshot, tetap append-only bagi aplikasi):
--     base_unit_price  harga dasar produk saat checkout
--     tier_min_qty     min_qty jenjang grosir yang dipakai (NULL = harga eceran)
--     unit             satuan jual saat checkout
--   unit_price tetap harga efektif yang dipakai. Pesanan lama: NULL (tanpa migrasi data).
--
-- ROLLBACK (down) — kode lama tidak membaca kolom ini, AMAN DIBIARKAN. Bila tetap ingin:
--   ALTER TABLE order_items DROP COLUMN unit, DROP COLUMN tier_min_qty, DROP COLUMN base_unit_price;
--   ALTER TABLE cart_items DROP COLUMN seen_unit_price;

ALTER TABLE cart_items
  ADD COLUMN seen_unit_price INT UNSIGNED NULL AFTER qty;

ALTER TABLE order_items
  ADD COLUMN base_unit_price INT UNSIGNED NULL AFTER unit_price,
  ADD COLUMN tier_min_qty INT UNSIGNED NULL AFTER base_unit_price,
  ADD COLUMN unit VARCHAR(20) NULL AFTER tier_min_qty;
