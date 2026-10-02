// Tes logika murni pricelist (src/pricelist/layout.js).
import {
  formatRupiah,
  formatDateId,
  defaultDateText,
  fileNameFor,
  normalizeWhatsapp,
  groupProducts,
  defaultSelection,
  catalogForSelection,
  wrapText,
  ellipsize,
  toRows,
  paginate,
  buildShareText,
  waShareUrl,
} from '../pricelist/layout';

const measure = (s) => s.length * 10; // 10 px per karakter

const cats = [
  { id: 2, name: 'Tepung', sortOrder: 20 },
  { id: 1, name: 'Kerupuk', sortOrder: 10 },
  { id: 3, name: 'Saos', sortOrder: 20 }, // sortOrder sama dengan Tepung -> urut nama
];
const prod = (id, name, categoryId, price, isActive = true, extra = {}) => ({ id, name, categoryId, price, isActive, categoryName: '', ...extra });
const products = [
  prod(1, 'kerupuk udang', 1, 45000),
  prod(2, 'Kerupuk Aci', 1, 25000),
  prod(3, 'Tepung Beras', 2, 32000),
  prod(4, 'Tepung Maizena', 2, 28000, false),
  prod(5, 'Saos Tomat', 3, 18000),
  prod(6, 'Barang Lama', 99, 1000, true, { categoryName: 'Kategori Terhapus', categoryDeleted: true }),
];

test('formatRupiah: pemisah ribuan id-ID', () => {
  expect(formatRupiah(45000)).toBe('Rp 45.000');
  expect(formatRupiah(0)).toBe('Rp 0');
  expect(formatRupiah(500)).toBe('Rp 500');
  expect(formatRupiah(1250000)).toBe('Rp 1.250.000');
  expect(formatRupiah('1000000000')).toBe('Rp 1.000.000.000');
  expect(formatRupiah(null)).toBe('Rp 0');
});

test('tanggal id-ID, teks default, dan nama file', () => {
  const d = new Date(2026, 9, 2);
  expect(formatDateId(d)).toBe('2 Oktober 2026');
  expect(defaultDateText(d)).toBe('Berlaku per 2 Oktober 2026');
  expect(formatDateId(new Date(2027, 0, 15))).toBe('15 Januari 2027');
  expect(fileNameFor(d, 0)).toBe('pricelist-mihan-store-20261002-1.png');
  expect(fileNameFor(d, 1)).toBe('pricelist-mihan-store-20261002-2.png');
});

test('normalizeWhatsapp: format +62 rapi; kosong/placeholder/tidak valid -> null', () => {
  expect(normalizeWhatsapp('081234567890')).toEqual({ digits: '6281234567890', display: '+62 812-3456-7890' });
  expect(normalizeWhatsapp('+62 812-3456-7890')).toEqual({ digits: '6281234567890', display: '+62 812-3456-7890' });
  expect(normalizeWhatsapp('6281311112222').display).toBe('+62 813-1111-2222');
  expect(normalizeWhatsapp('0812345678').display).toBe('+62 812-3456-78');
  expect(normalizeWhatsapp('08123456789').display).toBe('+62 812-3456-789');
  expect(normalizeWhatsapp('081234567890123').display).toBe('+62 812-3456-7890-123');
  expect(normalizeWhatsapp('BELUM DIISI')).toBeNull();
  expect(normalizeWhatsapp(' belum diisi ')).toBeNull();
  expect(normalizeWhatsapp('')).toBeNull();
  expect(normalizeWhatsapp(null)).toBeNull();
  expect(normalizeWhatsapp(undefined)).toBeNull();
  expect(normalizeWhatsapp('12345')).toBeNull();
  expect(normalizeWhatsapp('021-5551234')).toBeNull(); // bukan nomor HP
  expect(normalizeWhatsapp('abc')).toBeNull();
});

test('groupProducts: default hanya produk aktif, urut kategori (sortOrder, nama) lalu nama produk', () => {
  const g = groupProducts(products, cats);
  expect(g.map((x) => x.name)).toEqual(['Kerupuk', 'Saos', 'Tepung', 'Kategori Terhapus']);
  expect(g[0].items.map((i) => i.name)).toEqual(['Kerupuk Aci', 'kerupuk udang']); // tanpa beda huruf besar/kecil
  expect(g[2].items.map((i) => i.name)).toEqual(['Tepung Beras']); // Maizena nonaktif tidak ikut
  expect(g[3].known).toBe(false);
});

test('groupProducts: produk nonaktif ikut bila dipilih manual dan ditandai', () => {
  const sel = new Set([3, 4]);
  const g = groupProducts(products, cats, sel);
  expect(g).toHaveLength(1);
  expect(g[0].items.map((i) => [i.name, i.active])).toEqual([
    ['Tepung Beras', true],
    ['Tepung Maizena', false],
  ]);
  expect(groupProducts(products, cats, new Set())).toEqual([]);
});

test('defaultSelection & catalogForSelection', () => {
  expect([...defaultSelection(products)].sort()).toEqual([1, 2, 3, 5, 6]);
  const all = catalogForSelection(products, cats);
  expect(all.reduce((s, g) => s + g.items.length, 0)).toBe(6);
  expect(all.find((g) => g.name === 'Tepung').items.map((i) => i.active)).toEqual([true, false]);
});

test('groupProducts: id sama urut stabil', () => {
  const g = groupProducts([prod(9, 'Sama', 1, 1), prod(8, 'Sama', 1, 2)], cats);
  expect(g[0].items.map((i) => i.id)).toEqual([8, 9]);
});

test('wrapText: maksimal 2 baris, elipsis, kata panjang dipatah', () => {
  expect(wrapText('Kerupuk Aci', 200, measure)).toEqual(['Kerupuk Aci']);
  expect(wrapText('Tepung Terigu Protein Tinggi', 150, measure)).toEqual(['Tepung Terigu', 'Protein Tinggi']);
  const long = wrapText('Kerupuk Finna Rasa Udang Galah Asli Kemasan Keluarga', 150, measure);
  expect(long).toHaveLength(2);
  expect(long[1].endsWith('…')).toBe(true);
  long.forEach((l) => expect(measure(l)).toBeLessThanOrEqual(150));
  const word = wrapText('Supercalifragilisticexpialidocious', 100, measure, 2);
  expect(word).toHaveLength(2);
  word.forEach((l) => expect(measure(l)).toBeLessThanOrEqual(100));
  expect(wrapText('', 100, measure)).toEqual(['']);
  expect(ellipsize('Halo dunia', 1000, measure)).toBe('Halo dunia');
  expect(ellipsize('Halo dunia panjang', 60, measure)).toBe('Halo…');
});

test('toRows: 1 kolom dan 2 kolom (tinggi baris = sel tertinggi)', () => {
  const items = [{ h: 80 }, { h: 120 }, { h: 80 }];
  expect(toRows(items, 1, (c) => c.h).map((r) => r.h)).toEqual([80, 120, 80]);
  const two = toRows(items, 2, (c) => c.h);
  expect(two.map((r) => [r.cells.length, r.h])).toEqual([
    [2, 120],
    [1, 80],
  ]);
});

const mk = (key, n, h = 100) => ({ key, name: key, rows: Array.from({ length: n }, () => ({ h })) });
const opt = { budget: 1000, catHeaderH: 50, groupGap: 20 };

test('paginate: semua muat di satu halaman', () => {
  const pages = paginate([mk('A', 3), mk('B', 3)], opt);
  expect(pages).toHaveLength(1);
  expect(pages[0].height).toBe(50 + 300 + 20 + 50 + 300);
});

test('paginate: kategori yang muat utuh tidak dipotong, pindah ke halaman berikutnya', () => {
  // A = 50 + 600 = 650; B = 50 + 400 = 450 -> tidak muat di sisa (1000 - 650 - 20) -> halaman 2.
  const pages = paginate([mk('A', 6), mk('B', 4), mk('C', 2)], opt);
  expect(pages.map((p) => p.blocks.map((b) => `${b.key}:${b.rows.length}${b.continued ? '+' : ''}`))).toEqual([['A:6'], ['B:4', 'C:2']]);
  pages.forEach((p) => expect(p.height).toBeLessThanOrEqual(1000));
  // Setiap kategori muncul tepat sekali (tidak terpotong).
  const keys = pages.flatMap((p) => p.blocks.map((b) => b.key));
  expect(keys).toEqual(['A', 'B', 'C']);
});

test('paginate: kategori lebih tinggi dari satu halaman dipecah, judul diulang (lanjutan)', () => {
  const pages = paginate([mk('A', 2), mk('BESAR', 20)], opt);
  const blocks = pages.flatMap((p) => p.blocks);
  const besar = blocks.filter((b) => b.key === 'BESAR');
  expect(besar.length).toBeGreaterThan(1);
  expect(besar[0].continued).toBe(false);
  besar.slice(1).forEach((b) => expect(b.continued).toBe(true));
  expect(besar.reduce((s, b) => s + b.rows.length, 0)).toBe(20);
  pages.forEach((p) => expect(p.height).toBeLessThanOrEqual(1000));
  besar.forEach((b) => expect(b.rows.length).toBeGreaterThanOrEqual(2)); // tanpa judul yatim
});

test('paginate: tidak meninggalkan judul kategori dengan < 2 baris di akhir halaman', () => {
  // A memakai 50 + 800 = 850; sisa 130 hanya cukup judul + 0 baris -> BESAR mulai di halaman baru.
  const pages = paginate([mk('A', 8), mk('BESAR', 12)], opt);
  expect(pages[0].blocks.map((b) => b.key)).toEqual(['A']);
  expect(pages[1].blocks[0].key).toBe('BESAR');
  expect(pages[1].blocks[0].continued).toBe(false);
});

test('paginate: input kosong -> tanpa halaman', () => {
  expect(paginate([], opt)).toEqual([]);
  expect(paginate([mk('A', 0)], opt)).toEqual([]);
});

test('buildShareText: judul, tanggal, catatan, nomor; nomor disembunyikan bila belum diisi', () => {
  const t = buildShareText({ title: 'Daftar Harga Mihan Store', dateText: 'Berlaku per 2 Oktober 2026', note: 'Promo ongkir', whatsapp: '081234567890' });
  expect(t).toBe('*Daftar Harga Mihan Store*\nBerlaku per 2 Oktober 2026\n\nPromo ongkir\n\nInfo dan pemesanan: wa.me/6281234567890');
  const noWa = buildShareText({ title: 'Judul', dateText: 'Tgl', note: '', whatsapp: 'BELUM DIISI' });
  expect(noWa).not.toMatch(/wa\.me\/\d/);
  expect(noWa).toContain('Info dan pemesanan');
  expect(noWa).not.toContain('\n\n\n');
  expect(buildShareText({ title: 'X', whatsapp: '123' })).not.toMatch(/wa\.me\/\d/);
  expect(waShareUrl('a b&c')).toBe('https://wa.me/?text=a%20b%26c');
});
