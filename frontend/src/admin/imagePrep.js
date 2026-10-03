// Perkecil foto di browser SEBELUM diunggah (foto kamera HP 3–6 MB -> umumnya < 500 KB).
// Canvas 2D tanpa dependensi: sisi terpanjang maks. 1600 px, JPEG kualitas ±0,85, orientasi EXIF diterapkan
// (createImageBitmap dengan imageOrientation "from-image"; cadangan: elemen <img>, yang di browser modern juga
// mengikuti orientasi EXIF). Hasil canvas tidak membawa metadata (lokasi GPS dsb. ikut terbuang).
// Server tetap memeriksa dan memproses ulang (batas 2 MB, 1200 px).

export const MAX_UPLOAD_BYTES = 2 * 1024 * 1024;
export const CLIENT_MAX_SIDE = 1600;
export const CLIENT_QUALITY = 0.85;
export const ACCEPTED_TYPES = ['image/jpeg', 'image/png', 'image/webp'];
export const ACCEPT_ATTR = ACCEPTED_TYPES.join(',');
// Batas berkas MENTAH yang mau dicoba dibaca browser (sebelum diperkecil).
export const MAX_SOURCE_BYTES = 40 * 1024 * 1024;

export const MSG_TYPE = 'Format foto tidak didukung. Pilih foto JPG, PNG, atau WebP.';
export const MSG_DECODE = 'Foto tidak bisa dibaca di browser ini. Coba foto lain, atau simpan ulang sebagai JPG.';
export const MSG_TOO_BIG = 'Foto masih lebih dari 2 MB setelah diperkecil. Coba foto lain dengan resolusi lebih kecil.';
export const MSG_SOURCE_TOO_BIG = 'Berkas foto terlalu besar (lebih dari 40 MB). Pilih foto lain.';

export class ImagePrepError extends Error {}

// Ukuran target: sisi terpanjang <= maxSide, rasio terjaga, tidak diperbesar.
export const fitSize = (w, h, maxSide = CLIENT_MAX_SIDE) => {
  if (!(w > 0) || !(h > 0)) return { width: 0, height: 0 };
  if (w <= maxSide && h <= maxSide) return { width: Math.round(w), height: Math.round(h) };
  const s = maxSide / Math.max(w, h);
  return { width: Math.max(1, Math.round(w * s)), height: Math.max(1, Math.round(h * s)) };
};

const loadViaImgElement = (file) =>
  new Promise((resolve, reject) => {
    if (typeof Image === 'undefined' || typeof URL === 'undefined' || !URL.createObjectURL) {
      reject(new ImagePrepError(MSG_DECODE));
      return;
    }
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.decoding = 'async';
    img.onload = () => {
      const width = img.naturalWidth || img.width;
      const height = img.naturalHeight || img.height;
      resolve({ source: img, width, height, close: () => URL.revokeObjectURL(url) });
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new ImagePrepError(MSG_DECODE));
    };
    img.src = url;
  });

// Buka berkas sebagai sumber gambar yang sudah memperhatikan orientasi EXIF.
export const decodeImage = async (file) => {
  if (typeof createImageBitmap === 'function') {
    try {
      const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
      return { source: bmp, width: bmp.width, height: bmp.height, close: () => bmp.close && bmp.close() };
    } catch {
      // Browser lama (opsi tidak dikenal) atau format tidak bisa didekode: coba lewat <img>.
    }
  }
  return loadViaImgElement(file);
};

const canvasToBlob = (canvas, quality) =>
  new Promise((resolve) => {
    if (!canvas.toBlob) {
      resolve(null);
      return;
    }
    canvas.toBlob((b) => resolve(b || null), 'image/jpeg', quality);
  });

const defaultCreateCanvas = () => document.createElement('canvas');

/**
 * Siapkan foto untuk diunggah.
 * @returns {Promise<{blob: Blob, width: number, height: number, quality: number}>}
 * @throws {ImagePrepError} pesan ramah berbahasa Indonesia.
 */
export const prepareImage = async (
  file,
  { maxSide = CLIENT_MAX_SIDE, quality = CLIENT_QUALITY, maxBytes = MAX_UPLOAD_BYTES, createCanvas = defaultCreateCanvas } = {}
) => {
  if (!file) throw new ImagePrepError('Pilih foto terlebih dahulu.');
  const type = String(file.type || '').toLowerCase();
  // HEIC/AVIF/GIF/SVG dsb. ditolak (iPhone otomatis mengubah HEIC ke JPEG bila input menerima JPEG saja).
  if (!ACCEPTED_TYPES.includes(type)) throw new ImagePrepError(MSG_TYPE);
  if (file.size > MAX_SOURCE_BYTES) throw new ImagePrepError(MSG_SOURCE_TOO_BIG);
  const decoded = await decodeImage(file);
  try {
    if (!(decoded.width > 0) || !(decoded.height > 0)) throw new ImagePrepError(MSG_DECODE);
    let side = Math.max(decoded.width, decoded.height) > maxSide ? maxSide : Math.max(decoded.width, decoded.height);
    let q = quality;
    // Kualitas diturunkan bertahap; bila masih terlalu besar, ukuran diperkecil lagi.
    for (let attempt = 0; attempt < 12; attempt += 1) {
      const { width, height } = fitSize(decoded.width, decoded.height, side);
      const canvas = createCanvas();
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext && canvas.getContext('2d');
      if (!ctx) throw new ImagePrepError('Browser ini tidak mendukung pengolahan foto (Canvas).');
      ctx.fillStyle = '#ffffff'; // PNG/WebP transparan -> latar putih (JPEG tidak punya transparansi)
      ctx.fillRect(0, 0, width, height);
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = 'high';
      ctx.drawImage(decoded.source, 0, 0, width, height);
      // eslint-disable-next-line no-await-in-loop
      const blob = await canvasToBlob(canvas, q);
      if (!blob) throw new ImagePrepError(MSG_DECODE);
      if (blob.size <= maxBytes) return { blob, width, height, quality: q };
      if (q > 0.55) q = Math.round((q - 0.1) * 100) / 100;
      else side = Math.max(320, Math.round(side * 0.8));
    }
    throw new ImagePrepError(MSG_TOO_BIG);
  } finally {
    decoded.close();
  }
};

export const prepErrorMessage = (err) => (err instanceof ImagePrepError ? err.message : MSG_DECODE);
