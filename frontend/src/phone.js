// Nomor telepon/WhatsApp: aturan SAMA dengan backend (NormalizePhone di backend/validate.go).
// Menerima 08xx, 628xx, +628xx; spasi, titik, tanda hubung, dan kurung diabaikan. Karakter dari
// keyboard/kontak HP diseragamkan dulu (tanda hubung & spasi Unicode, angka lebar, angka
// Arab-Indic; karakter tak terlihat dibuang), "+62 0812..." dirapikan menjadi "+62812...".
// Hasil: +628xx dengan 8-15 digit (tanpa '+').

export const PHONE_ERRORS = {
  chars: 'Nomor telepon ada karakter yang tidak valid. Gunakan angka saja (boleh diawali +62).',
  prefix: 'Nomor telepon harus diawali 08, 62, atau +62 (nomor HP, mis. 0812...).',
  short: 'Nomor telepon terlalu pendek, minimal 8 digit.',
  long: 'Nomor telepon terlalu panjang, maksimal 15 digit.',
};

// Sama dengan unicode.IsSpace di Go.
const isSpace = (c) =>
  (c >= 0x09 && c <= 0x0d) || c === 0x20 || c === 0x85 || c === 0xa0 || c === 0x1680 ||
  (c >= 0x2000 && c <= 0x200a) || c === 0x2028 || c === 0x2029 || c === 0x202f || c === 0x205f || c === 0x3000;

const isBidiControl = (c) => (c >= 0x202a && c <= 0x202e) || (c >= 0x2066 && c <= 0x2069) || c === 0x200e || c === 0x200f;

// Kembalikan karakter ASCII pengganti, '' untuk dibuang, atau karakter aslinya.
const foldChar = (ch) => {
  const c = ch.codePointAt(0);
  if ((c >= 0x2010 && c <= 0x2015) || c === 0x2212 || c === 0xfe58 || c === 0xfe63 || c === 0xff0d) return '-';
  if (isSpace(c)) return ' ';
  if (c >= 0xff10 && c <= 0xff19) return String(c - 0xff10);
  if (c >= 0x0660 && c <= 0x0669) return String(c - 0x0660);
  if (c >= 0x06f0 && c <= 0x06f9) return String(c - 0x06f0);
  if (c === 0xff0b) return '+';
  if (c === 0xff08) return '(';
  if (c === 0xff09) return ')';
  if (c === 0xff0e) return '.';
  if ((c >= 0x200b && c <= 0x200f) || (c >= 0x2060 && c <= 0x2064) || c === 0xfeff || isBidiControl(c)) return '';
  return ch;
};

// normalizePhone(s) -> { phone, error }. phone '' + error '' = kosong (tidak diisi).
export const normalizePhone = (s) => {
  let p = '';
  for (const ch of String(s ?? '')) {
    const f = foldChar(ch);
    if (f !== ' ' && f !== '-' && f !== '.' && f !== '(' && f !== ')') p += f;
  }
  if (!p) return { phone: '', error: '' };
  if (!/^\+?[0-9]*$/.test(p)) return { phone: '', error: PHONE_ERRORS.chars };
  if (p.startsWith('+62')) p = p.slice(1);
  else if (p.startsWith('62')) {
    // tetap
  } else if (p.startsWith('0')) p = `62${p.slice(1)}`;
  else return { phone: '', error: PHONE_ERRORS.prefix };
  if (p.startsWith('620')) p = `62${p.slice(3)}`; // "+62 0812..." -> "62812..."
  if (!p.startsWith('628')) return { phone: '', error: PHONE_ERRORS.prefix };
  if (p.length < 8) return { phone: '', error: PHONE_ERRORS.short };
  if (p.length > 15) return { phone: '', error: PHONE_ERRORS.long };
  return { phone: `+${p}`, error: '' };
};

// Pesan galat untuk nomor yang diisi ('' bila valid atau kosong).
export const phoneError = (s) => normalizePhone(s).error;
