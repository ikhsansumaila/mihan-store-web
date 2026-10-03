// Logika murni pricelist (tanpa DOM/canvas) agar bisa diuji unit: format rupiah & tanggal, alamat web
// pemesanan, pengelompokan + urutan produk per kategori, pembungkusan teks, pemecahan halaman gambar, teks pendamping.

export const NOTE_MAX = 200;
export const TITLE_MAX = 80;
export const DATE_MAX = 80;
export const URL_MAX = 120;
export const DEFAULT_TITLE = 'Daftar Harga Mihan Store';
export const STORE_NAME = 'Mihan Store';

export const THEMES = {
  ungu: { label: 'Ungu', primary: '#6b21a8', accent: '#7e22ce', light: '#f3e8ff', stripe: '#faf5ff', line: '#e9d5ff', soft: '#ede9fe' },
  hijau: { label: 'Hijau', primary: '#065f46', accent: '#047857', light: '#d1fae5', stripe: '#f0fdf4', line: '#bbf7d0', soft: '#dcfce7' },
  biru: { label: 'Biru', primary: '#1e3a8a', accent: '#1d4ed8', light: '#dbeafe', stripe: '#eff6ff', line: '#bfdbfe', soft: '#dbeafe' },
};
export const DEFAULT_THEME = 'ungu';

// Mode tampilan harga di gambar. Bawaan: eceran + grosir (produk tanpa jenjang tetap tampil seperti biasa).
export const PRICE_MODES = {
  both: 'Eceran + grosir',
  retail: 'Eceran saja',
  wholesale: 'Grosir saja',
};
export const DEFAULT_PRICE_MODE = 'both';
export const TIER_SEP = ' · ';

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

// Alamat web pemesanan (halaman toko untuk pelanggan) -> { href: 'https://store.mihan.web.id', display:
// 'store.mihan.web.id' } atau null bila kosong/tidak valid. Hanya http/https, tanpa spasi, tanpa user:password.
// Tanpa skema dianggap https (mis. "store.mihan.web.id"). display = tanpa skema dan tanpa "/" di akhir.
export const normalizeOrderUrl = (raw) => {
  if (raw === null || raw === undefined) return null;
  const s = String(raw).trim();
  if (!s || s.length > URL_MAX || /\s/.test(s)) return null;
  const hasScheme = /^[a-z][a-z0-9+.-]*:/i.test(s);
  if (hasScheme && !/^https?:\/\//i.test(s)) return null; // ftp:, javascript:, mailto:, dll.
  let u;
  try {
    u = new URL(hasScheme ? s : `https://${s}`);
  } catch {
    return null;
  }
  if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.hostname || u.username || u.password) return null;
  const href = `${u.protocol}//${u.host}${u.pathname === '/' ? '' : u.pathname.replace(/\/+$/, '')}${u.search}${u.hash}`;
  return { href, display: href.replace(/^https?:\/\//i, '') };
};

// Alamat bawaan: asal (origin) halaman admin yang sedang dibuka, mis. https://store.mihan.web.id.
export const defaultOrderUrl = () => (typeof window !== 'undefined' && window.location && window.location.origin) || '';

const collator = typeof Intl !== 'undefined' ? new Intl.Collator('id', { sensitivity: 'base', numeric: true }) : null;
const cmpText = (a, b) => (collator ? collator.compare(a, b) : a < b ? -1 : a > b ? 1 : 0);

// Jenjang dari API (unitPrice = harga efektif dari server) -> [{minQty, unitPrice}] urut minQty naik.
export const toTiers = (tiers) =>
  (Array.isArray(tiers) ? tiers : [])
    .map((t) => ({ minQty: Number(t.minQty), unitPrice: Number(t.unitPrice) }))
    .filter((t) => Number.isInteger(t.minQty) && t.minQty >= 2 && Number.isFinite(t.unitPrice) && t.unitPrice > 0)
    .sort((a, b) => a.minQty - b.minQty);

// Produk admin (GET /api/admin/products) -> bentuk ringkas.
export const toItem = (p) => ({
  id: p.id,
  name: String(p.name || '').trim(),
  price: Number(p.price) || 0,
  unit: String(p.unit || 'pcs').trim() || 'pcs',
  tiers: toTiers(p.tiers),
  categoryId: p.categoryId ?? null,
  categoryName: p.categoryName || '',
  active: p.isActive !== false,
});

// Teks per jenjang: "10+ : Rp 42.000".
export const tierSegments = (tiers) => (tiers || []).map((t) => `${t.minQty}+ : ${formatRupiah(t.unitPrice)}`);

// Apa yang tampil untuk satu produk pada mode tertentu.
// showBase: harga eceran di kanan; showTiers: baris jenjang di bawah nama.
export const priceParts = (item, mode = DEFAULT_PRICE_MODE) => {
  const hasTiers = !!(item.tiers && item.tiers.length);
  const showTiers = hasTiers && mode !== 'retail';
  const showBase = !hasTiers || mode !== 'wholesale';
  return { showBase, showTiers };
};

// Bungkus potongan teks ATOMIK (mis. "10+ : Rp 42.000") dengan pemisah, maksimal maxLines baris.
// Potongan tidak pernah dipatah di tengah; potongan yang lebih lebar dari satu baris dipotong elipsis.
// Bila masih tersisa setelah maxLines, baris terakhir diakhiri "…".
export const wrapSegments = (segments, maxWidth, measure, maxLines = 8, sep = TIER_SEP) => {
  const segs = (segments || []).filter(Boolean);
  if (!segs.length) return [];
  const lines = [];
  let cur = '';
  for (const seg of segs) {
    const piece = measure(seg) > maxWidth ? ellipsize(seg, maxWidth, measure) : seg;
    const candidate = cur ? `${cur}${sep}${piece}` : piece;
    if (!cur || measure(candidate) <= maxWidth) cur = candidate;
    else {
      lines.push(cur);
      cur = piece;
    }
  }
  if (cur) lines.push(cur);
  if (lines.length <= maxLines) return lines;
  const kept = lines.slice(0, maxLines);
  // Potongan berikutnya memang tidak muat di baris ini, jadi hasilnya pasti berakhir "…".
  kept[maxLines - 1] = ellipsize(`${kept[maxLines - 1]}${sep}${lines[maxLines]}`, maxWidth, measure);
  return kept;
};

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

// Teks pendamping untuk WhatsApp (tanpa nomor WA; pemesanan lewat web toko).
export const buildShareText = ({ title, dateText, note, orderUrl }) => {
  const lines = [];
  const t = String(title || '').trim();
  if (t) lines.push(`*${t}*`);
  const d = String(dateText || '').trim();
  if (d) lines.push(d);
  const n = String(note || '').trim();
  if (n) lines.push('', n);
  const u = normalizeOrderUrl(orderUrl);
  if (u) lines.push('', `Pesan online di ${u.href}`);
  return lines.join('\n');
};

export const waShareUrl = (text) => `https://wa.me/?text=${encodeURIComponent(text || '')}`;
