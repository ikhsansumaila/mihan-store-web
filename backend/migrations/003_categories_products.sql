-- 003_categories_products.sql
-- Kategori dan produk di database (menggantikan 20 produk hardcoded di main.go).
-- Dijalankan oleh root:
--   docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot mihanstore' < backend/migrations/003_categories_products.sql
--
-- Kolom `alive` (1 = aktif, NULL = soft-deleted) membuat slug unik hanya di antara
-- kategori yang belum dihapus. Jangan ditulis dari aplikasi.
-- Harga dalam rupiah (bilangan bulat). image_path disiapkan untuk gambar (belum ada upload).
--
-- ROLLBACK (down): DROP TABLE products; DROP TABLE categories;
--   (kode lama memakai daftar hardcoded dan tidak membaca tabel ini)

CREATE TABLE categories (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(50) NOT NULL,
  name VARCHAR(80) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  alive TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  UNIQUE KEY uq_slug (slug, alive),
  KEY idx_sort (sort_order, id),
  KEY idx_deleted_at (deleted_at)
) ENGINE=InnoDB;

CREATE TABLE products (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  category_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(150) NOT NULL,
  description TEXT NULL,
  price INT UNSIGNED NOT NULL,
  image_path VARCHAR(255) NULL,
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  created_by BIGINT UNSIGNED NULL,
  updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_category (category_id),
  KEY idx_active_deleted (is_active, deleted_at),
  KEY idx_name (name),
  CONSTRAINT fk_products_category FOREIGN KEY (category_id) REFERENCES categories(id),
  CONSTRAINT fk_products_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_products_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

INSERT INTO categories (id, slug, name, sort_order) VALUES
  (1, 'kerupuk', 'Kerupuk', 10),
  (2, 'tepung', 'Tepung', 20),
  (3, 'saos', 'Saos', 30),
  (4, 'sambal', 'Sambal', 40),
  (5, 'sendok_plastik', 'Sendok Plastik', 50),
  (6, 'box_hampers', 'Box Hampers', 60);
ALTER TABLE categories AUTO_INCREMENT = 7;

-- 20 produk dari daftar hardcoded lama (id dipertahankan agar API publik identik).
INSERT INTO products (id, category_id, name, description, price, image_path) VALUES
  (1,  1, 'Kerupuk Finna Udang', 'Kerupuk udang asli dengan rasa gurih', 45000, 'kerupuk1.jpg'),
  (2,  1, 'Kerupuk Finna Bawang', 'Kerupuk bawang renyah', 55000, 'kerupuk2.jpg'),
  (3,  1, 'Kerupuk Gondang', 'Kerupuk ketela tradisional', 35000, 'kerupuk3.jpg'),
  (4,  1, 'Kerupuk Aci', 'Kerupuk aci warna-warni', 25000, 'kerupuk4.jpg'),
  (5,  2, 'Tepung Terigu Protein Tinggi', 'Tepung terigu 1kg protein tinggi', 65000, 'tepung1.jpg'),
  (6,  2, 'Tepung Maizena', 'Tepung maizena 500gr', 28000, 'tepung2.jpg'),
  (7,  2, 'Tepung Beras', 'Tepung beras premium 500gr', 32000, 'tepung3.jpg'),
  (8,  2, 'Tepung Gandum', 'Tepung gandum organik 1kg', 48000, 'tepung4.jpg'),
  (9,  3, 'Saos Tomat Murni', 'Saos tomat 400ml tanpa pengawet', 18000, 'saos1.jpg'),
  (10, 3, 'Saos Cabe Merah', 'Saos cabe merah pedas 300ml', 22000, 'saos2.jpg'),
  (11, 4, 'Sambal Bajak Pedas', 'Sambal bajak tradisional 350gr', 35000, 'sambal1.jpg'),
  (12, 4, 'Sambal Matah Segar', 'Sambal matah segar 250gr', 28000, 'sambal2.jpg'),
  (13, 5, 'Sendok Plastik Putih', '1 pak isi 50 pcs sendok plastik', 5000, 'sendok1.jpg'),
  (14, 5, 'Sendok Plastik Warna', '1 pak isi 50 pcs warna-warni', 7000, 'sendok2.jpg'),
  (15, 6, 'Box Hampers Kecil', 'Box hampers isi produk pilihan', 75000, 'hampers1.jpg'),
  (16, 6, 'Box Hampers Sedang', 'Box hampers isi 8 item premium', 150000, 'hampers2.jpg'),
  (17, 6, 'Box Hampers Besar', 'Box hampers lengkap 15 item', 300000, 'hampers3.jpg'),
  (18, 6, 'Box Hampers VIP', 'Box hampers eksklusif 20 item mewah', 500000, 'hampers4.jpg'),
  (19, 3, 'Saos Kecap Manis', 'Kecap manis premium 420ml', 16000, 'saos3.jpg'),
  (20, 4, 'Sambal Tomat Pedas', 'Sambal tomat ulek 300gr', 32000, 'sambal3.jpg');
ALTER TABLE products AUTO_INCREMENT = 21;
