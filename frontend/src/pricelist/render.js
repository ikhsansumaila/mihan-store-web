// Penggambaran pricelist ke Canvas 2D (di browser, tanpa dependensi/font eksternal).
// Lebar tetap 1080 px, tinggi menyesuaikan isi (maks MAX_HEIGHT per gambar), skala 1:1 (tanpa devicePixelRatio)
// agar hasil sama di semua perangkat. Logika urutan/pemecahan halaman ada di ./layout.
import {
  THEMES,
  DEFAULT_THEME,
  DEFAULT_PRICE_MODE,
  STORE_NAME,
  formatRupiah,
  normalizeOrderUrl,
  wrapText,
  wrapSegments,
  tierSegments,
  priceParts,
  ellipsize,
  toRows,
  paginate,
} from './layout';

export const WIDTH = 1080;
export const MAX_HEIGHT = 2400;
export const LOGO_SRC = '/mihan-store-logo.png';
const M = 56; // margin kiri/kanan
const CW = WIDTH - 2 * M; // lebar konten
const FONT = 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", "Liberation Sans", sans-serif';
const font = (weight, size) => `${weight} ${size}px ${FONT}`;

const G = {
  bandPad: 44,
  logo: 140,
  titleSize: 54,
  titleLH: 64,
  dateSize: 30,
  dateLH: 40,
  noteGap: 32,
  noteSize: 30,
  noteLH: 42,
  notePad: 22,
  contentTop: 36,
  contentBottom: 40,
  catH: 68,
  catSize: 34,
  groupGap: 32,
  rowPadY: 20,
  cellPadX: 20,
  colGap: 16,
  nameLH: 40,
  tierGap: 6,
  tierMaxLines: 20, // = maksimal jenjang per produk: satu jenjang per baris pun tidak pernah terpotong
  footerUrl: 150,
  footerPlain: 104,
};

// Gambar logo same-origin (tanpa crossOrigin agar canvas tidak tercemar). null bila gagal dimuat.
export const loadLogo = (src = LOGO_SRC) =>
  new Promise((resolve) => {
    if (typeof Image === 'undefined') return resolve(null);
    const img = new Image();
    img.onload = () => resolve(img.naturalWidth ? img : null);
    img.onerror = () => resolve(null);
    img.src = src;
    return undefined;
  });

// Foto produk (opsi "Tampilkan foto produk"): thumbnail persegi dipotong tengah di kiri baris.
export const PHOTO_SIZE = { 1: 96, 2: 84 };
export const PHOTO_GAP = 18;
export const PHOTO_TIMEOUT_MS = 8000;

// Muat satu thumbnail same-origin (tanpa crossOrigin -> canvas tidak tercemar). null bila gagal/terlalu lama.
const loadOnePhoto = (src, timeoutMs) =>
  new Promise((resolve) => {
    if (!src || typeof Image === 'undefined') return resolve(null);
    let done = false;
    let timer = null;
    const finish = (v) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve(v);
    };
    const img = new Image();
    timer = setTimeout(() => finish(null), timeoutMs);
    img.onload = () => finish(img.naturalWidth && img.naturalHeight ? img : null);
    img.onerror = () => finish(null);
    img.decoding = 'async';
    img.src = src;
    return undefined;
  });

// Muat thumbnail semua produk terpilih SEBELUM menggambar: Map(id -> Image | null). Gagal/lewat batas waktu ->
// null (digambar sebagai kotak placeholder). Hanya path same-origin ("/uploads/...") yang dimuat.
export const loadPhotos = async (items, { timeoutMs = PHOTO_TIMEOUT_MS, cache } = {}) => {
  const out = new Map();
  await Promise.all(
    (items || []).map(async (it) => {
      const src = /^\/[^/\s]\S*$/.test(String(it.thumb || '')) ? it.thumb : '';
      if (!src) return out.set(it.id, null);
      if (cache && cache.has(src)) return out.set(it.id, cache.get(src));
      const img = await loadOnePhoto(src, timeoutMs);
      if (cache) cache.set(src, img);
      return out.set(it.id, img);
    })
  );
  return out;
};

const defaultCreateCanvas = () => document.createElement('canvas');

const measurer = (ctx, f) => (s) => {
  ctx.font = f;
  return ctx.measureText(s).width;
};

// Hitung tata letak semua halaman. groups: hasil groupProducts().
export const layoutPricelist = (
  { groups, columns = 1, title, dateText, note, orderUrl, priceMode = DEFAULT_PRICE_MODE, showPhotos = false },
  mctx
) => {
  const cols = columns === 2 ? 2 : 1;
  // Opsi foto: kolom foto persegi di kiri sel; teks bergeser, tinggi baris minimal setinggi foto.
  const photo = showPhotos ? PHOTO_SIZE[cols] : 0;
  const photoW = showPhotos ? photo + PHOTO_GAP : 0;
  const textX = M + G.logo + 36;
  const textW = WIDTH - textX - M;
  const titleLines = wrapText(title || '', textW, measurer(mctx, font(800, G.titleSize)), 2);
  const dateLine = ellipsize(String(dateText || '').trim(), textW, measurer(mctx, font(400, G.dateSize)));
  const textBlockH = titleLines.length * G.titleLH + (dateLine ? 14 + G.dateLH : 0);
  const bandH = G.bandPad * 2 + Math.max(G.logo, textBlockH);
  const noteText = String(note || '').trim();
  const noteLines = noteText ? wrapText(noteText, CW - 2 * G.notePad - 10, measurer(mctx, font(600, G.noteSize)), 4) : [];
  const noteH = noteLines.length ? noteLines.length * G.noteLH + 2 * G.notePad : 0;
  const headerH = bandH + (noteH ? G.noteGap + noteH : 0);
  const url = normalizeOrderUrl(orderUrl);
  const footerH = url ? G.footerUrl : G.footerPlain;

  const cellW = cols === 2 ? (CW - G.colGap) / 2 : CW;
  const nameSize = cols === 2 ? 30 : 32;
  const priceSize = cols === 2 ? 30 : 32;
  // Satuan di samping harga ("/ pak") dan baris jenjang grosir di bawah nama ("10+ : Rp 42.000 · 50+ : …").
  const unitSize = cols === 2 ? 22 : 24;
  const tierSize = priceMode === 'wholesale' ? (cols === 2 ? 25 : 27) : cols === 2 ? 22 : 24;
  const tierLH = tierSize + 10;
  const mName = measurer(mctx, font(500, nameSize));
  const mPrice = measurer(mctx, font(800, priceSize));
  const mUnit = measurer(mctx, font(500, unitSize));
  const mTier = measurer(mctx, font(600, tierSize));
  const prepared = (groups || []).map((g) => {
    const items = g.items.map((it) => {
      const unit = it.unit || 'pcs';
      const { showBase, showTiers } = priceParts(it, priceMode);
      const price = showBase ? formatRupiah(it.price) : '';
      const unitText = showBase ? ` / ${unit}` : `per ${unit}`;
      const unitW = mUnit(unitText);
      const priceW = (price ? mPrice(price) : 0) + unitW;
      const nameW = Math.max(80, cellW - 2 * G.cellPadX - photoW - priceW - 24);
      const lines = wrapText(it.name, nameW, mName, 2);
      const tierLines = showTiers ? wrapSegments(tierSegments(it.tiers), cellW - 2 * G.cellPadX - photoW, mTier, G.tierMaxLines) : [];
      const contentH = lines.length * G.nameLH + (tierLines.length ? G.tierGap + tierLines.length * tierLH : 0);
      return { ...it, priceText: price, unitText, unitW, lines, tierLines, contentH, h: Math.max(contentH, photo) + 2 * G.rowPadY };
    });
    return { key: g.key, name: g.name, rows: toRows(items, cols, (c) => c.h) };
  });
  const budget = MAX_HEIGHT - headerH - G.contentTop - G.contentBottom - footerH;
  const pages = paginate(prepared, { budget, catHeaderH: G.catH, groupGap: G.groupGap }).map((p) => ({
    ...p,
    canvasHeight: Math.ceil(headerH + G.contentTop + p.height + G.contentBottom + footerH),
  }));
  return {
    cols,
    cellW,
    nameSize,
    priceSize,
    unitSize,
    tierSize,
    tierLH,
    priceMode,
    titleLines,
    dateLine,
    noteLines,
    bandH,
    headerH,
    footerH,
    url,
    pages,
    textX,
    showPhotos: !!showPhotos,
    photo,
    photoW,
  };
};

// Gambar foto (dipotong tengah, sudut membulat) atau kotak placeholder netral bila tidak ada/gagal dimuat.
const drawPhoto = (ctx, img, x, y, size) => {
  const call = (m, ...a) => typeof ctx[m] === 'function' && ctx[m](...a);
  call('save');
  roundRect(ctx, x, y, size, size, 12);
  ctx.fillStyle = '#f3f4f6';
  ctx.fill();
  if (img && img.naturalWidth && img.naturalHeight) {
    call('clip');
    const s = Math.max(size / img.naturalWidth, size / img.naturalHeight);
    const sw = size / s;
    const sh = size / s;
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(img, (img.naturalWidth - sw) / 2, (img.naturalHeight - sh) / 2, sw, sh, x, y, size, size);
  } else {
    // Placeholder: bingkai gambar sederhana abu-abu.
    ctx.fillStyle = '#d1d5db';
    const m = size * 0.28;
    ctx.fillRect(x + m, y + size * 0.62, size - 2 * m, size * 0.06);
    roundRect(ctx, x + size * 0.36, y + size * 0.3, size * 0.16, size * 0.16, size * 0.08);
    ctx.fill();
  }
  call('restore');
};

const roundRect = (ctx, x, y, w, h, r) => {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
};

// Gambar satu halaman ke ctx (ukuran kanvas sudah WIDTH x page.canvasHeight).
export const drawPage = (ctx, L, index, { theme = DEFAULT_THEME, logo = null, photos = null } = {}) => {
  const T = THEMES[theme] || THEMES[DEFAULT_THEME];
  const page = L.pages[index];
  const H = page.canvasHeight;
  const total = L.pages.length;
  ctx.textBaseline = 'middle';
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, WIDTH, H);

  // Header berwarna: logo dalam kotak putih, judul, tanggal.
  ctx.fillStyle = T.primary;
  ctx.fillRect(0, 0, WIDTH, L.bandH);
  ctx.fillStyle = T.accent;
  ctx.fillRect(0, L.bandH - 10, WIDTH, 10);
  const logoY = G.bandPad + (L.bandH - 2 * G.bandPad - G.logo) / 2;
  ctx.fillStyle = '#ffffff';
  roundRect(ctx, M, logoY, G.logo, G.logo, 24);
  ctx.fill();
  if (logo && logo.naturalWidth) {
    const box = G.logo - 20;
    const s = Math.min(box / logo.naturalWidth, box / logo.naturalHeight);
    const w = logo.naturalWidth * s;
    const h = logo.naturalHeight * s;
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(logo, M + (G.logo - w) / 2, logoY + (G.logo - h) / 2, w, h);
  }
  const textBlockH = L.titleLines.length * G.titleLH + (L.dateLine ? 14 + G.dateLH : 0);
  let ty = G.bandPad + (L.bandH - 2 * G.bandPad - textBlockH) / 2;
  ctx.textAlign = 'left';
  ctx.fillStyle = '#ffffff';
  ctx.font = font(800, G.titleSize);
  L.titleLines.forEach((line) => {
    ctx.fillText(line, L.textX, ty + G.titleLH / 2);
    ty += G.titleLH;
  });
  if (L.dateLine) {
    ctx.fillStyle = 'rgba(255,255,255,0.88)';
    ctx.font = font(400, G.dateSize);
    ctx.fillText(L.dateLine, L.textX, ty + 14 + G.dateLH / 2);
  }

  // Catatan (promo) dalam kotak berwarna muda dengan garis aksen di kiri.
  if (L.noteLines.length) {
    const ny = L.bandH + G.noteGap;
    const nh = L.noteLines.length * G.noteLH + 2 * G.notePad;
    ctx.fillStyle = T.light;
    roundRect(ctx, M, ny, CW, nh, 16);
    ctx.fill();
    ctx.fillStyle = T.accent;
    roundRect(ctx, M, ny, 10, nh, 5);
    ctx.fill();
    ctx.fillStyle = T.primary;
    ctx.font = font(600, G.noteSize);
    L.noteLines.forEach((line, i) => ctx.fillText(line, M + 10 + G.notePad, ny + G.notePad + i * G.noteLH + G.noteLH / 2));
  }

  // Kategori dan baris produk.
  let y = L.headerH + G.contentTop;
  page.blocks.forEach((b, bi) => {
    if (bi > 0) y += G.groupGap;
    ctx.fillStyle = T.accent;
    roundRect(ctx, M, y, CW, G.catH, 14);
    ctx.fill();
    ctx.fillStyle = '#ffffff';
    ctx.textAlign = 'left';
    ctx.font = font(800, G.catSize);
    const label = b.continued ? `${b.name} (lanjutan)` : b.name;
    ctx.fillText(ellipsize(label, CW - 48, measurer(ctx, font(800, G.catSize))), M + 24, y + G.catH / 2);
    ctx.font = font(800, G.catSize); // measurer mengubah font; kembalikan
    y += G.catH;
    b.rows.forEach((row, ri) => {
      row.cells.forEach((cell, ci) => {
        const x = M + ci * (L.cellW + G.colGap);
        ctx.fillStyle = ri % 2 === 0 ? '#ffffff' : T.stripe;
        ctx.fillRect(x, y, L.cellW, row.h);
        ctx.fillStyle = '#e5e7eb';
        ctx.fillRect(x, y + row.h - 1, L.cellW, 1);
        // Isi sel (nama maks 2 baris + baris jenjang) di tengah vertikal baris; harga + satuan rata kanan,
        // sejajar tengah blok nama. Tanpa jenjang: sama seperti sebelumnya (nama & harga di tengah baris).
        const top = y + (row.h - cell.contentH) / 2;
        // Opsi foto: kotak foto di kiri (tengah vertikal), teks mulai setelahnya.
        const tx = x + G.cellPadX + (L.showPhotos ? L.photoW : 0);
        if (L.showPhotos) drawPhoto(ctx, photos ? photos.get(cell.id) : null, x + G.cellPadX, y + (row.h - L.photo) / 2, L.photo);
        ctx.fillStyle = '#111827';
        ctx.textAlign = 'left';
        ctx.font = font(500, L.nameSize);
        cell.lines.forEach((line, li) => ctx.fillText(line, tx, top + G.nameLH / 2 + li * G.nameLH));
        const midY = top + (cell.lines.length * G.nameLH) / 2;
        const right = x + L.cellW - G.cellPadX;
        ctx.textAlign = 'right';
        ctx.fillStyle = '#6b7280';
        ctx.font = font(500, L.unitSize);
        ctx.fillText(cell.unitText, right, midY);
        if (cell.priceText) {
          ctx.fillStyle = T.primary;
          ctx.font = font(800, L.priceSize);
          ctx.fillText(cell.priceText, right - cell.unitW, midY);
        }
        if (cell.tierLines.length) {
          ctx.textAlign = 'left';
          ctx.fillStyle = L.priceMode === 'wholesale' ? T.primary : T.accent;
          ctx.font = font(600, L.tierSize);
          const ty0 = top + cell.lines.length * G.nameLH + G.tierGap + L.tierLH / 2;
          cell.tierLines.forEach((line, ti) => ctx.fillText(line, tx, ty0 + ti * L.tierLH));
        }
      });
      y += row.h;
    });
  });

  // Footer: alamat web pemesanan (tanpa skema), nama toko, nomor halaman.
  const fy = H - L.footerH;
  ctx.fillStyle = T.primary;
  ctx.fillRect(0, fy, WIDTH, L.footerH);
  ctx.fillStyle = T.accent;
  ctx.fillRect(0, fy, WIDTH, 8);
  ctx.fillStyle = '#ffffff';
  let lineY = fy + 8 + (L.footerH - 8) / 2;
  if (L.url) {
    ctx.textAlign = 'center';
    ctx.font = font(800, 36);
    const line = ellipsize(`Pesan online: ${L.url.display}`, CW, measurer(ctx, font(800, 36)));
    ctx.font = font(800, 36); // measurer mengubah font; kembalikan
    ctx.fillText(line, WIDTH / 2, fy + 56);
    lineY = fy + 112;
  }
  ctx.font = font(600, 28);
  ctx.fillStyle = 'rgba(255,255,255,0.92)';
  if (total > 1) {
    ctx.textAlign = 'left';
    ctx.fillText(STORE_NAME, M, lineY);
    ctx.textAlign = 'right';
    ctx.fillText(`Halaman ${index + 1}/${total}`, WIDTH - M, lineY);
  } else {
    ctx.textAlign = 'center';
    ctx.fillText(STORE_NAME, WIDTH / 2, lineY);
  }
  ctx.textAlign = 'left';
};

// Buat semua halaman sebagai kanvas. Melempar Error bila Canvas 2D tidak tersedia.
export const renderPricelist = (opts, { createCanvas = defaultCreateCanvas, logo = null, photos = null } = {}) => {
  const probe = createCanvas();
  const mctx = probe.getContext && probe.getContext('2d');
  if (!mctx) throw new Error('Browser ini tidak mendukung pembuatan gambar (Canvas).');
  const L = layoutPricelist(opts, mctx);
  return L.pages.map((p, i) => {
    const canvas = i === 0 ? probe : createCanvas();
    canvas.width = WIDTH;
    canvas.height = p.canvasHeight;
    const ctx = canvas.getContext('2d');
    drawPage(ctx, L, i, { theme: opts.theme, logo, photos });
    return { canvas, width: WIDTH, height: p.canvasHeight };
  });
};

export const canvasToBlob = (canvas) =>
  new Promise((resolve, reject) => {
    if (!canvas.toBlob) return reject(new Error('Browser ini tidak bisa menyimpan gambar PNG.'));
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('Gagal membuat file PNG.'))), 'image/png');
    return undefined;
  });
