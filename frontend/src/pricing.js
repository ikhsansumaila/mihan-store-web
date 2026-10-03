// Harga grosir di sisi browser — HANYA untuk membantu admin (pratinjau & peringatan langsung di form
// produk) dan tampilan. Sumber kebenaran harga tetap server (backend/pricing.go: effectiveUnitPrice &
// validateTiers); aturan di sini sengaja dibuat sama agar peringatan di form cocok dengan penolakan server.
import { rupiah } from './shop/format';

export const MAX_TIERS = 20;
export const MIN_TIER_QTY = 2;
export const MAX_TIER_QTY = 1000000;
export const UNIT_MAX = 20;
export const DEFAULT_UNIT = 'pcs';
export const UNIT_SUGGESTIONS = ['pcs', 'pak', 'dus', 'box', 'karton', 'bungkus', 'botol', 'kg', 'gram', 'lusin'];

// Satuan: trim, huruf kecil, spasi dirapikan; kosong -> "pcs". null bila tidak valid.
export const normalizeUnit = (raw) => {
  const s = String(raw ?? '')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
  if (!s) return DEFAULT_UNIT;
  return /^[a-z0-9 ./]{1,20}$/.test(s) ? s : null;
};

// "12,5" / "12.5" / "42000" -> bilangan bulat x100 (12,5 -> 1250). null bila tidak valid (maks. 2 desimal).
export const parseX100 = (raw) => {
  const s = String(raw ?? '').trim().replace(',', '.');
  if (!/^[0-9]+(\.[0-9]{1,2})?$/.test(s) || s.length > 16) return null;
  const [w, f = ''] = s.split('.');
  return Number(w) * 100 + Number((f + '00').slice(0, 2));
};

// Harga efektif satu jenjang: fixed = value; percent = round half up(base*(100-p)/100); min(…, base); minimal 1.
export const tierPrice = (base, type, valueX100) => {
  let p = type === 'percent' ? Math.floor((base * (10000 - valueX100) + 5000) / 10000) : Math.floor(valueX100 / 100);
  if (p > base) p = base;
  if (p < 1) p = 1;
  return p;
};

// Format jenjang ringkas: "10+ : Rp 42.000".
export const tierLabel = (t, unit) => `${t.minQty}+${unit ? ` ${unit}` : ''}: ${rupiah(t.unitPrice)}`;

const fmtPct = (x100) => `${(x100 / 100).toLocaleString('id-ID', { maximumFractionDigits: 2 })}%`;

// Analisis baris form jenjang terhadap harga dasar -> { rows, valid, errorCount }.
// rows[i] = { index, minQty, type, valueX100, price, savePerUnit, savePct, errors: [] } (urutan sama dengan input).
export const analyzeTiers = (baseRaw, inputRows) => {
  const base = Number(baseRaw);
  const baseOk = Number.isInteger(base) && base >= 0;
  const rows = (inputRows || []).map((r, index) => ({ index, errors: [], price: null, savePerUnit: null, savePct: null, ...parseRow(r) }));
  const seen = new Map();
  rows.forEach((r) => {
    const label = `Jenjang ${r.index + 1}`;
    if (r.minQty === null) {
      r.errors.push(`${label}: jumlah minimal harus bilangan bulat 2 sampai 1.000.000`);
      return;
    }
    if (seen.has(r.minQty)) {
      r.errors.push(`${label}: jumlah minimal ${r.minQty} sudah dipakai jenjang ${seen.get(r.minQty) + 1}`);
      return;
    }
    seen.set(r.minQty, r.index);
    if (r.type === 'fixed' && (r.valueX100 === null || r.valueX100 % 100 !== 0 || r.valueX100 < 100)) {
      r.errors.push(`${label}: harga (Rp) harus bilangan bulat minimal 1`);
    } else if (r.type === 'percent' && (r.valueX100 === null || r.valueX100 < 1 || r.valueX100 > 9999)) {
      r.errors.push(`${label}: diskon persen harus 0,01 sampai 99,99 (maksimal 2 angka desimal)`);
    } else if (r.type !== 'fixed' && r.type !== 'percent') {
      r.errors.push(`${label}: jenis potongan tidak dikenal`);
    }
  });
  if (baseOk) {
    const ok = rows.filter((r) => !r.errors.length).sort((a, b) => a.minQty - b.minQty);
    let prev = null;
    ok.forEach((r) => {
      r.price = tierPrice(base, r.type, r.valueX100);
      r.savePerUnit = base - r.price;
      r.savePct = base > 0 ? Math.round(((base - r.price) / base) * 1000) / 10 : 0;
      const label = `Jenjang ${r.index + 1} (min. ${r.minQty})`;
      if (r.price >= base) {
        r.errors.push(`${label}: harga efektif ${rupiah(r.price)} harus lebih murah dari harga dasar ${rupiah(base)}`);
      } else if (prev && r.price >= prev.price) {
        r.errors.push(`${label}: harga efektif ${rupiah(r.price)} harus lebih murah dari jenjang min. ${prev.minQty} (${rupiah(prev.price)})`);
      }
      prev = r;
    });
  }
  const errorCount = rows.reduce((n, r) => n + r.errors.length, 0) + (rows.length > MAX_TIERS ? 1 : 0);
  return { rows, valid: baseOk && errorCount === 0, errorCount };
};

function parseRow(r) {
  const q = String(r?.minQty ?? '').trim();
  const minQty = /^[0-9]+$/.test(q) && Number(q) >= MIN_TIER_QTY && Number(q) <= MAX_TIER_QTY ? Number(q) : null;
  return { minQty, type: r?.type, valueX100: parseX100(r?.value) };
}

// Ringkasan nilai jenjang untuk tampilan: "Rp 42.000" atau "10%".
export const tierValueText = (t) => (t.type === 'percent' ? `${fmtPct(Math.round(Number(t.value) * 100))}` : rupiah(t.value));

// Baris form -> body API ({minQty, type, value} angka).
export const tiersToBody = (rows) =>
  (rows || []).map((r) => ({ minQty: Number(String(r.minQty).trim()), type: r.type, value: Number(String(r.value).trim().replace(',', '.')) }));

// Jenjang dari API -> baris form.
export const tiersToRows = (tiers) =>
  (tiers || []).map((t, i) => ({
    key: `t${i}-${t.minQty}`,
    minQty: String(t.minQty),
    type: t.type === 'percent' ? 'percent' : 'fixed',
    value: t.type === 'percent' ? String(t.value).replace('.', ',') : String(t.value),
  }));
