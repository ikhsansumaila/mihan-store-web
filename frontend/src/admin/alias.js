// Alias pelanggan (khusus admin). Aturan sama dengan backend NormalizeAlias (customers.go):
// karakter kontrol, bidi, dan tak terlihat (zero-width, BOM) dibuang; tab/baris baru jadi spasi;
// spasi dirapikan; maks. 100 karakter (code point) SETELAH dirapikan. Kosong = hapus alias.

export const ALIAS_MAX = 100;

// eslint-disable-next-line no-control-regex
const DROP_RE = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F-\u009F\u200B-\u200F\u202A-\u202E\u2060-\u2064\u2066-\u2069\uFEFF]/g;

export const normalizeAlias = (s) =>
  String(s ?? '')
    .replace(/[\t\n\r]/g, ' ')
    .replace(DROP_RE, '')
    .split(/\s+/)
    .filter(Boolean)
    .join(' ');

export const aliasLength = (s) => [...normalizeAlias(s)].length;
