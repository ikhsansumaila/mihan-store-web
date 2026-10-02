// Penggambaran pricelist ke Canvas 2D (di browser, tanpa dependensi/font eksternal).
// Lebar tetap 1080 px, tinggi menyesuaikan isi (maks MAX_HEIGHT per gambar), skala 1:1 (tanpa devicePixelRatio)
// agar hasil sama di semua perangkat. Logika urutan/pemecahan halaman ada di ./layout.
import { THEMES, DEFAULT_THEME, STORE_NAME, formatRupiah, normalizeOrderUrl, wrapText, ellipsize, toRows, paginate } from './layout';

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

const defaultCreateCanvas = () => document.createElement('canvas');

const measurer = (ctx, f) => (s) => {
  ctx.font = f;
  return ctx.measureText(s).width;
};

// Hitung tata letak semua halaman. groups: hasil groupProducts().
export const layoutPricelist = ({ groups, columns = 1, title, dateText, note, orderUrl }, mctx) => {
  const cols = columns === 2 ? 2 : 1;
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
  const mName = measurer(mctx, font(500, nameSize));
  const mPrice = measurer(mctx, font(800, priceSize));
  const prepared = (groups || []).map((g) => {
    const items = g.items.map((it) => {
      const price = formatRupiah(it.price);
      const priceW = mPrice(price);
      const nameW = Math.max(80, cellW - 2 * G.cellPadX - priceW - 24);
      const lines = wrapText(it.name, nameW, mName, 2);
      return { ...it, priceText: price, lines, h: lines.length * G.nameLH + 2 * G.rowPadY };
    });
    return { key: g.key, name: g.name, rows: toRows(items, cols, (c) => c.h) };
  });
  const budget = MAX_HEIGHT - headerH - G.contentTop - G.contentBottom - footerH;
  const pages = paginate(prepared, { budget, catHeaderH: G.catH, groupGap: G.groupGap }).map((p) => ({
    ...p,
    canvasHeight: Math.ceil(headerH + G.contentTop + p.height + G.contentBottom + footerH),
  }));
  return { cols, cellW, nameSize, priceSize, titleLines, dateLine, noteLines, bandH, headerH, footerH, url, pages, textX };
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
export const drawPage = (ctx, L, index, { theme = DEFAULT_THEME, logo = null } = {}) => {
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
        // Nama (kiri, maks 2 baris) dan harga (kanan, rata kanan), keduanya di tengah vertikal baris.
        ctx.fillStyle = '#111827';
        ctx.textAlign = 'left';
        ctx.font = font(500, L.nameSize);
        const startY = y + (row.h - cell.lines.length * G.nameLH) / 2 + G.nameLH / 2;
        cell.lines.forEach((line, li) => ctx.fillText(line, x + G.cellPadX, startY + li * G.nameLH));
        ctx.fillStyle = T.primary;
        ctx.textAlign = 'right';
        ctx.font = font(800, L.priceSize);
        ctx.fillText(cell.priceText, x + L.cellW - G.cellPadX, y + row.h / 2);
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
export const renderPricelist = (opts, { createCanvas = defaultCreateCanvas, logo = null } = {}) => {
  const probe = createCanvas();
  const mctx = probe.getContext && probe.getContext('2d');
  if (!mctx) throw new Error('Browser ini tidak mendukung pembuatan gambar (Canvas).');
  const L = layoutPricelist(opts, mctx);
  return L.pages.map((p, i) => {
    const canvas = i === 0 ? probe : createCanvas();
    canvas.width = WIDTH;
    canvas.height = p.canvasHeight;
    const ctx = canvas.getContext('2d');
    drawPage(ctx, L, i, { theme: opts.theme, logo });
    return { canvas, width: WIDTH, height: p.canvasHeight };
  });
};

export const canvasToBlob = (canvas) =>
  new Promise((resolve, reject) => {
    if (!canvas.toBlob) return reject(new Error('Browser ini tidak bisa menyimpan gambar PNG.'));
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('Gagal membuat file PNG.'))), 'image/png');
    return undefined;
  });
