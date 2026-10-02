// Logika murni pricelist (tanpa DOM/canvas) agar bisa diuji unit: format rupiah & tanggal, nomor WhatsApp,
// pengelompokan + urutan produk per kategori, pembungkusan teks, pemecahan halaman gambar, teks pendamping.
import { waNumber } from '../shop/format';

export const PLACEHOLDER = 'BELUM DIISI';
export const NOTE_MAX = 200;
export const TITLE_MAX = 80;
export const DATE_MAX = 80;
export const DEFAULT_TITLE = 'Daftar Harga Mihan Store';
export const STORE_NAME = 'Mihan Store';

export const THEMES = {
  ungu: { label: 'Ungu', primary: '#6b21a8', accent: '#7e22ce', light: '#f3e8ff', stripe: '#faf5ff', line: '#e9d5ff', soft: '#ede9fe' },
  hijau: { label: 'Hijau', primary: '#065f46', accent: '#047857', light: '#d1fae5', stripe: '#f0fdf4', line: '#bbf7d0', soft: '#dcfce7' },
  biru: { label: 'Biru', primary: '#1e3a8a', accent: '#1d4ed8', light: '#dbeafe', stripe: '#eff6ff', line: '#bfdbfe', soft: '#dbeafe' },
};
export const DEFAULT_THEME = 'ungu';

// "Rp 45.000" (pemisah ribuan titik, id-ID) tanpa bergantung pada data ICU lingkungan.
export const formatRupiah = (n) => {
  const v = Math.round(Number(n) || 0);
  const s = String(Math.abs(v)).replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  return `${v < 0 ? '-' : ''}Rp ${s}`;
};

const MONTHS = ['Januari', 'Februari', 'Maret', 'April', 'Mei', 'Juni', 'Juli', 'Agustus', 'September', 'Oktober', 'November', 'Desember'];

// "2 Oktober 2026"
export const formatDateId = (d = new Date()) => `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
export const defaultDateText = (d = new Date()) => `Berlaku per ${formatDateId(d)}`;

// YYYYMMDD untuk nama file.
export const dateStamp = (d = new Date()) =>
  `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`;

export const fileNameFor = (d, index) => `pricelist-mihan-store-${dateStamp(d)}-${index + 1}.png`;

// Nomor WhatsApp toko -> { digits: '6281234567890', display: '+62 812-3456-7890' } atau null bila kosong,
// placeholder "BELUM DIISI", atau tidak valid.
export const normalizeWhatsapp = (raw) => {
  if (raw === null || raw === undefined) return null;
  const s = String(raw).trim();
  if (!s || s.toUpperCase() === PLACEHOLDER) return null;
  const digits = waNumber(s);
  if (!digits) return null;
  const rest = digits.slice(2);
  const parts = [rest.slice(0, 3)];
  for (let i = 3; i < rest.length; i += 4) parts.push(rest.slice(i, i + 4));
  // Hindari kelompok terakhir 1 digit (mis. 812-3456-7890-1 -> 812-3456-78901).
  if (parts.length > 2 && parts[parts.length - 1].length === 1) {
    const last = parts.pop();
    parts[parts.length - 1] += last;
  }
  return { digits, display: `+62 ${parts.join('-')}` };
};

const collator = typeof Intl !== 'undefined' ? new Intl.Collator('id', { sensitivity: 'base', numeric: true }) : null;
const cmpText = (a, b) => (collator ? collator.compare(a, b) : a < b ? -1 : a > b ? 1 : 0);

// Produk admin (GET /api/admin/products) -> bentuk ringkas.
export const toItem = (p) => ({
  id: p.id,
  name: String(p.name || '').trim(),
  price: Number(p.price) || 0,
  categoryId: p.categoryId ?? null,
  categoryName: p.categoryName || '',
  active: p.isActive !== false,
});

// Kelompokkan produk per kategori. Urutan kategori: sortOrder, lalu nama, lalu id; kategori yang tidak
// dikenal (mis. sudah dihapus) di akhir. Urutan produk: nama (id-ID, tanpa beda huruf besar/kecil), lalu id.
// selected: Set id produk yang ikut; bila tidak diberikan, semua produk aktif.
export const groupProducts = (products, categories, selected) => {
  const items = (products || []).map(toItem);
  const isSel = selected ? (it) => selected.has(it.id) : (it) => it.active;
  const cats = new Map((categories || []).map((c) => [c.id, c]));
  const groups = new Map();
  items.forEach((it) => {
    if (!isSel(it)) return;
    const c = cats.get(it.categoryId);
    const key = c ? `c${c.id}` : `x${it.categoryId ?? ''}`;
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        id: c ? c.id : it.categoryId,
        name: c ? c.name : it.categoryName || 'Lainnya',
        sortOrder: c ? Number(c.sortOrder) || 0 : Number.POSITIVE_INFINITY,
        known: !!c,
        items: [],
      });
    }
    groups.get(key).items.push(it);
  });
  const out = [...groups.values()];
  out.forEach((g) => g.items.sort((a, b) => cmpText(a.name, b.name) || a.id - b.id));
  out.sort((a, b) => (a.sortOrder === b.sortOrder ? 0 : a.sortOrder < b.sortOrder ? -1 : 1) || cmpText(a.name, b.name) || String(a.key).localeCompare(String(b.key)));
  return out;
};

// Semua kategori + produknya (aktif dan nonaktif) untuk daftar centang di UI, urutan sama seperti gambar.
export const catalogForSelection = (products, categories) => {
  const all = new Set((products || []).map((p) => p.id));
  const groups = groupProducts(products, categories, all);
  // Kategori tanpa produk tidak ditampilkan (tidak ada yang bisa dicentang).
  return groups;
};

export const defaultSelection = (products) => new Set((products || []).filter((p) => p.isActive !== false).map((p) => p.id));

// Bungkus teks maksimal maxLines baris dengan lebar maxWidth (measure(text) -> lebar piksel).
// Baris terakhir dipotong dengan elipsis bila masih ada sisa. Kata yang lebih panjang dari satu baris dipatah.
export const wrapText = (text, maxWidth, measure, maxLines = 2) => {
  const words = String(text || '').trim().split(/\s+/).filter(Boolean);
  if (!words.length) return [''];
  const lines = [];
  let cur = '';
  const pushWordChars = (word) => {
    // Patah kata terlalu panjang per karakter.
    let chunk = '';
    for (const ch of word) {
      if (measure(chunk + ch) > maxWidth && chunk) {
        lines.push(chunk);
        chunk = ch;
      } else chunk += ch;
    }
    return chunk;
  };
  for (const w of words) {
    const candidate = cur ? `${cur} ${w}` : w;
    if (measure(candidate) <= maxWidth) {
      cur = candidate;
    } else if (!cur) {
      cur = pushWordChars(w);
    } else {
      lines.push(cur);
      cur = measure(w) <= maxWidth ? w : pushWordChars(w);
    }
  }
  if (cur) lines.push(cur);
  if (lines.length <= maxLines) return lines;
  const kept = lines.slice(0, maxLines);
  const last = `${kept[maxLines - 1]} ${lines.slice(maxLines).join(' ')}`;
  return [...kept.slice(0, maxLines - 1), ellipsize(last, maxWidth, measure)];
};

// Potong teks satu baris dengan "…" agar muat maxWidth.
export const ellipsize = (text, maxWidth, measure) => {
  const s = String(text || '');
  if (measure(s) <= maxWidth) return s;
  const chars = [...s];
  let lo = 0;
  let hi = chars.length;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    if (measure(`${chars.slice(0, mid).join('').trimEnd()}…`) <= maxWidth) lo = mid;
    else hi = mid - 1;
  }
  return `${chars.slice(0, lo).join('').trimEnd()}…`;
};

// Susun item kategori menjadi baris (1 atau 2 kolom). heightOf(item) -> tinggi sel; tinggi baris = sel tertinggi.
export const toRows = (items, columns, heightOf) => {
  const rows = [];
  const n = columns === 2 ? 2 : 1;
  for (let i = 0; i < items.length; i += n) {
    const cells = items.slice(i, i + n);
    rows.push({ cells, h: Math.max(...cells.map(heightOf)) });
  }
  return rows;
};

// Pecah kategori (dengan baris bertinggi h) menjadi halaman gambar dengan tinggi konten maksimum `budget`.
// - Kategori yang muat utuh tidak pernah dipotong: bila tidak muat di sisa halaman, pindah ke halaman baru.
// - Kategori yang lebih tinggi dari satu halaman dipecah per baris; judul kategori diulang di halaman
//   berikutnya (continued: true). Minimal 2 baris (atau semua bila kurang) agar tidak ada judul yatim.
// groups: [{ key, name, rows: [{ h, ... }] }]  ->  pages: [{ blocks: [{ key, name, continued, rows }], height }]
export const paginate = (groups, { budget, catHeaderH, groupGap }) => {
  const pages = [];
  let cur = { blocks: [], height: 0 };
  const newPage = () => {
    if (cur.blocks.length) pages.push(cur);
    cur = { blocks: [], height: 0 };
  };
  const sum = (rows) => rows.reduce((s, r) => s + r.h, 0);
  const place = (g, rows, continued) => {
    const gap = cur.blocks.length ? groupGap : 0;
    cur.blocks.push({ key: g.key, name: g.name, continued, rows });
    cur.height += gap + catHeaderH + sum(rows);
  };
  (groups || []).forEach((g) => {
    if (!g.rows || !g.rows.length) return;
    const full = catHeaderH + sum(g.rows);
    const gap = cur.blocks.length ? groupGap : 0;
    if (cur.height + gap + full <= budget) return place(g, g.rows, false);
    if (cur.blocks.length && full <= budget) {
      newPage();
      return place(g, g.rows, false);
    }
    // Kategori lebih tinggi dari satu halaman: pecah per baris.
    const queue = [...g.rows];
    let continued = false;
    while (queue.length) {
      const gp = cur.blocks.length ? groupGap : 0;
      let avail = budget - cur.height - gp - catHeaderH;
      const take = [];
      while (queue.length && queue[0].h <= avail) {
        avail -= queue[0].h;
        take.push(queue.shift());
      }
      const minRows = Math.min(2, take.length + queue.length);
      if (take.length < minRows && cur.blocks.length) {
        queue.unshift(...take);
        newPage();
        continue; // eslint-disable-line no-continue
      }
      if (!take.length) take.push(queue.shift()); // baris tunggal lebih tinggi dari halaman (tidak terjadi normalnya)
      place(g, take, continued);
      continued = true;
      if (queue.length) newPage();
    }
    return undefined;
  });
  newPage();
  return pages;
};

// Teks pendamping untuk WhatsApp.
export const buildShareText = ({ title, dateText, note, whatsapp }) => {
  const lines = [];
  const t = String(title || '').trim();
  if (t) lines.push(`*${t}*`);
  const d = String(dateText || '').trim();
  if (d) lines.push(d);
  const n = String(note || '').trim();
  if (n) lines.push('', n);
  const wa = normalizeWhatsapp(whatsapp);
  lines.push('', wa ? `Info dan pemesanan: wa.me/${wa.digits}` : 'Info dan pemesanan: hubungi kami via WhatsApp.');
  return lines.join('\n');
};

export const waShareUrl = (text) => `https://wa.me/?text=${encodeURIComponent(text || '')}`;
