// Tes logika murni harga grosir di frontend: pratinjau/validasi form (src/pricing.js), teks WhatsApp,
// pricelist (mode Eceran / Grosir / Eceran + grosir), dan invoice PDF dari pesanan.
import { analyzeTiers, normalizeUnit, parseX100, tierPrice, tiersToBody, tiersToRows, MAX_TIERS } from '../pricing';
import { buildAdminSummaryText, buildCustomerConfirmText, perUnit } from '../shop/format';
import { groupProducts, priceParts, tierSegments, toItem, wrapSegments, PRICE_MODES, DEFAULT_PRICE_MODE } from '../pricelist/layout';
import { layoutPricelist, MAX_HEIGHT } from '../pricelist/render';

const mockDocs = [];
jest.mock('jspdf', () => ({
  __esModule: true,
  default: function MockPdf() {
    const doc = {
      texts: [],
      internal: { pageSize: { height: 297 } },
      setFontSize() {},
      setFont() {},
      setTextColor() {},
      setDrawColor() {},
      setLineWidth() {},
      setFillColor() {},
      line() {},
      rect() {},
      addPage() {},
      addImage() {},
      splitTextToSize: (t) => [String(t)],
      text(t) {
        doc.texts.push(Array.isArray(t) ? t.join(' ') : String(t));
      },
      output: () => 'blob:x',
    };
    mockDocs.push(doc);
    return doc;
  },
}));
const { generateInvoicePdf, orderToInvoice } = require('../invoicePdf');

const row = (minQty, type, value) => ({ key: `${minQty}`, minQty: String(minQty), type, value: String(value) });

describe('pricing.js (pratinjau & validasi form, aturan sama dengan server)', () => {
  test('harga jenjang: fixed, percent dibulatkan half up, min(harga jenjang, dasar), minimal 1', () => {
    expect(tierPrice(45000, 'fixed', 4200000)).toBe(42000);
    expect(tierPrice(1001, 'percent', 5000)).toBe(501); // 500,5 -> 501
    expect(tierPrice(10, 'percent', 3500)).toBe(7); // 6,5 -> 7 (half up)
    expect(tierPrice(999, 'percent', 1500)).toBe(849); // 849,15 -> 849
    expect(tierPrice(40000, 'fixed', 4200000)).toBe(40000); // lebih mahal dari dasar -> dasar
    expect(tierPrice(3, 'percent', 9999)).toBe(1); // minimal 1
  });

  test('analyzeTiers: valid -> harga efektif, hemat per unit & persen', () => {
    const a = analyzeTiers(45000, [row(50, 'percent', '10'), row(10, 'fixed', 42000)]);
    expect(a.valid).toBe(true);
    expect(a.rows[0]).toMatchObject({ minQty: 50, price: 40500, savePerUnit: 4500, savePct: 10 });
    expect(a.rows[1]).toMatchObject({ minQty: 10, price: 42000, savePerUnit: 3000 });
    expect(analyzeTiers(45000, []).valid).toBe(true);
  });

  test('analyzeTiers: pesan per baris (monoton, < harga dasar, duplikat, persen, jumlah minimal)', () => {
    const msgs = (base, rows) => analyzeTiers(base, rows).rows.map((r) => r.errors.join(' '));
    expect(msgs(1000, [row(5, 'fixed', 900), row(10, 'percent', 10)])[1]).toContain('harus lebih murah dari jenjang min. 5 (Rp 900)');
    expect(msgs(1000, [row(5, 'fixed', 1000)])[0]).toContain('harus lebih murah dari harga dasar Rp 1.000');
    expect(msgs(1000, [row(5, 'fixed', 900), row(5, 'fixed', 800)])[1]).toContain('sudah dipakai jenjang 1');
    expect(msgs(1000, [row(5, 'percent', 0)])[0]).toContain('0,01 sampai 99,99');
    expect(msgs(1000, [row(5, 'percent', 100)])[0]).toContain('0,01 sampai 99,99');
    expect(msgs(1000, [row(5, 'percent', '1,234')])[0]).toContain('maksimal 2 angka desimal');
    expect(msgs(1000, [row(1, 'fixed', 900)])[0]).toContain('2 sampai 1.000.000');
    expect(msgs(1000, [row(5, 'fixed', '9,5')])[0]).toContain('bilangan bulat');
    expect(msgs(1000, [row(5, 'gratis', 1)])[0]).toContain('jenis potongan');
    // Harga dasar turun (fixed tidak lagi lebih murah) -> ditandai.
    expect(analyzeTiers(42000, [row(10, 'fixed', 42000)]).valid).toBe(false);
    // Lebih dari 20 jenjang tidak valid.
    const many = Array.from({ length: MAX_TIERS + 1 }, (_, i) => row(i + 2, 'fixed', 50000 - i));
    expect(analyzeTiers(60000, many).valid).toBe(false);
  });

  test('parseX100, normalizeUnit, konversi baris <-> body API', () => {
    expect(parseX100('12,5')).toBe(1250);
    expect(parseX100('42000')).toBe(4200000);
    expect(parseX100('1.234')).toBeNull();
    expect(parseX100('-1')).toBeNull();
    expect(normalizeUnit('  PAK ')).toBe('pak');
    expect(normalizeUnit('')).toBe('pcs');
    expect(normalizeUnit('box  isi 12')).toBe('box isi 12');
    expect(normalizeUnit('dus-besar')).toBeNull();
    expect(normalizeUnit('a'.repeat(21))).toBeNull();
    expect(tiersToBody([row(10, 'percent', '12,5'), row(20, 'fixed', '42000')])).toEqual([
      { minQty: 10, type: 'percent', value: 12.5 },
      { minQty: 20, type: 'fixed', value: 42000 },
    ]);
    expect(tiersToRows([{ minQty: 10, type: 'percent', value: 12.5, unitPrice: 1 }])[0]).toMatchObject({ minQty: '10', type: 'percent', value: '12,5' });
  });
});

describe('teks WhatsApp & invoice memakai satuan dan harga efektif', () => {
  const order = {
    orderNo: 'MS-261003-0007',
    status: 'pending_payment',
    subtotal: 549000,
    discount: 0,
    shippingFee: 0,
    total: 549000,
    createdAt: '2026-10-03T03:00:00Z',
    recipient: { name: 'Budi', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang' },
    items: [
      { name: 'Kerupuk Grosir', unitPrice: 42000, baseUnitPrice: 45000, tierMinQty: 10, unit: 'pak', qty: 12, lineTotal: 504000 },
      { name: 'Saos', unitPrice: 45000, baseUnitPrice: 45000, tierMinQty: null, unit: 'botol', qty: 1, lineTotal: 45000 },
    ],
  };

  test('ringkasan admin -> pelanggan: "12 x Rp 42.000 / pak = ..." + catatan harga grosir', () => {
    const t = buildAdminSummaryText(order, {});
    expect(t).toContain('12 x Rp 42.000 / pak = Rp 504.000 (harga grosir min. 10)');
    expect(t).toContain('1 x Rp 45.000 / botol = Rp 45.000');
    expect(t).not.toContain('botol = Rp 45.000 (harga grosir');
    // Pesanan lama (tanpa satuan) tetap seperti dulu.
    const old = buildAdminSummaryText({ ...order, items: [{ name: 'Lama', unitPrice: 45000, qty: 2, lineTotal: 90000 }] }, {});
    expect(old).toContain('2 x Rp 45.000 = Rp 90.000');
    expect(perUnit(42000, 'pak')).toBe('Rp 42.000 / pak');
  });

  test('konfirmasi pelanggan memuat satuan dan tanda harga grosir', () => {
    const t = buildCustomerConfirmText(order, 'X');
    expect(t).toContain('- Kerupuk Grosir x12 pak (harga grosir)');
    expect(t).toContain('- Saos x1 botol\n');
    expect(t).not.toContain('Rp');
  });

  test('invoice pesanan: satuan, harga efektif per satuan, catatan HARGA GROSIR (MIN N); pesanan lama tetap PCS', async () => {
    const doc = await generateInvoicePdf(orderToInvoice(order, {}), { images: {}, open: false });
    const all = doc.texts.join('|');
    expect(all).toContain('12 PAK');
    expect(all).toMatch(/Rp\s?42\.000 \/ PAK/);
    expect(all).toContain('HARGA GROSIR (MIN 10)');
    expect(all).toContain('1 BOTOL');
    expect(all.match(/HARGA GROSIR/g)).toHaveLength(1);
    const lama = await generateInvoicePdf(orderToInvoice({ ...order, items: [{ name: 'Lama', unitPrice: 45000, qty: 2, lineTotal: 90000 }] }, {}), {
      images: {},
      open: false,
    });
    const allLama = lama.texts.join('|');
    expect(allLama).toContain('2 PCS');
    expect(allLama).not.toContain('/ PCS');
    expect(allLama).not.toContain('GROSIR');
  });
});

describe('pricelist: tiga mode tampilan harga', () => {
  // Konteks ukur tiruan: 16 px per karakter (lebar tidak bergantung font).
  const mctx = { font: '', measureText: (s) => ({ width: String(s).length * 16 }) };
  const products = [
    { id: 1, name: 'Kerupuk Udang', categoryId: 1, price: 45000, unit: 'pak', isActive: true, tiers: [
      { minQty: 10, type: 'fixed', value: 42000, unitPrice: 42000 },
      { minQty: 50, type: 'percent', value: 10, unitPrice: 40500 },
    ] },
    { id: 2, name: 'Kerupuk Aci', categoryId: 1, price: 25000, unit: 'pcs', isActive: true, tiers: [] },
  ];
  const cats = [{ id: 1, name: 'Kerupuk', sortOrder: 1 }];
  const opts = (priceMode, columns = 1) => ({ groups: groupProducts(products, cats), columns, title: 'Daftar', dateText: 'Hari ini', note: '', orderUrl: 'https://store.mihan.web.id', priceMode });
  const cellsOf = (L) => L.pages.flatMap((p) => p.blocks.flatMap((b) => b.rows.flatMap((r) => r.cells)));

  test('mode tersedia, default eceran + grosir', () => {
    expect(Object.keys(PRICE_MODES)).toEqual(['both', 'retail', 'wholesale']);
    expect(DEFAULT_PRICE_MODE).toBe('both');
    expect(PRICE_MODES.retail).toBe('Eceran saja');
    expect(PRICE_MODES.wholesale).toBe('Grosir saja');
    expect(PRICE_MODES.both).toBe('Eceran + grosir');
  });

  test('toItem membawa satuan & jenjang; segmen "10+ : Rp 42.000"', () => {
    const it = toItem(products[0]);
    expect(it.unit).toBe('pak');
    expect(tierSegments(it.tiers)).toEqual(['10+ : Rp 42.000', '50+ : Rp 40.500']);
    expect(toItem({ id: 3, name: 'x', price: 1 }).unit).toBe('pcs');
    expect(priceParts(it, 'retail')).toEqual({ showBase: true, showTiers: false });
    expect(priceParts(it, 'wholesale')).toEqual({ showBase: false, showTiers: true });
    expect(priceParts(it, 'both')).toEqual({ showBase: true, showTiers: true });
    expect(priceParts(toItem(products[1]), 'wholesale')).toEqual({ showBase: true, showTiers: false }); // tanpa jenjang: seperti biasa
  });

  test('Eceran saja: tanpa baris jenjang, harga + "/ satuan"', () => {
    const cells = cellsOf(layoutPricelist(opts('retail'), mctx));
    expect(cells.every((c) => c.tierLines.length === 0)).toBe(true);
    expect(cells.find((c) => c.id === 1)).toMatchObject({ priceText: 'Rp 45.000', unitText: ' / pak' });
  });

  test('Eceran + grosir: jenjang sebagai teks kecil di bawah nama; produk tanpa jenjang tetap', () => {
    const L = layoutPricelist(opts('both'), mctx);
    const [udang, aci] = [1, 2].map((id) => cellsOf(L).find((c) => c.id === id));
    expect(udang.tierLines.join(' ')).toContain('10+ : Rp 42.000 · 50+ : Rp 40.500');
    expect(udang.priceText).toBe('Rp 45.000');
    expect(aci.tierLines).toEqual([]);
    expect(udang.h).toBeGreaterThan(aci.h); // baris dengan jenjang lebih tinggi (tidak terpotong)
    expect(aci.h).toBe(cellsOf(layoutPricelist(opts('retail'), mctx)).find((c) => c.id === 2).h); // tanpa jenjang: tinggi sama
  });

  test('Grosir saja: harga eceran produk berjenjang disembunyikan, diganti "per satuan" + jenjang', () => {
    const cells = cellsOf(layoutPricelist(opts('wholesale'), mctx));
    expect(cells.find((c) => c.id === 1)).toMatchObject({ priceText: '', unitText: 'per pak' });
    expect(cells.find((c) => c.id === 1).tierLines.length).toBeGreaterThan(0);
    expect(cells.find((c) => c.id === 2)).toMatchObject({ priceText: 'Rp 25.000', unitText: ' / pcs' });
  });

  test('20 jenjang x banyak produk: halaman tetap <= tinggi maksimum, setiap produk muncul sekali, teks tidak melebihi lebar', () => {
    const tiers = Array.from({ length: 20 }, (_, i) => ({ minQty: (i + 1) * 10, type: 'fixed', value: 100000 - i * 1000, unitPrice: 100000 - i * 1000 }));
    const many = Array.from({ length: 30 }, (_, i) => ({ id: 100 + i, name: `Produk Grosir ${i}`, categoryId: 1, price: 200000, unit: 'karton', isActive: true, tiers }));
    for (const cols of [1, 2]) {
      const L = layoutPricelist({ ...opts('both', cols), groups: groupProducts(many, cats) }, mctx);
      expect(L.pages.length).toBeGreaterThan(1);
      L.pages.forEach((p) => expect(p.canvasHeight).toBeLessThanOrEqual(MAX_HEIGHT));
      const ids = cellsOf(L).map((c) => c.id);
      expect(new Set(ids).size).toBe(30);
      expect(ids).toHaveLength(30);
      const maxW = L.cellW - 40;
      cellsOf(L).forEach((c) => c.tierLines.forEach((line) => expect(line.length * 16).toBeLessThanOrEqual(maxW)));
      // 20 jenjang tetap tampil semua (tanpa elipsis) dalam batas baris.
      expect(cellsOf(L)[0].tierLines.join(' ')).toContain('200+ : Rp 81.000');
    }
  });

  test('wrapSegments: segmen tidak dipatah, elipsis bila melebihi batas baris', () => {
    const measure = (s) => s.length * 10;
    const segs = ['10+ : Rp 1', '20+ : Rp 2', '30+ : Rp 3'];
    expect(wrapSegments(segs, 1000, measure)).toEqual(['10+ : Rp 1 · 20+ : Rp 2 · 30+ : Rp 3']);
    expect(wrapSegments(segs, 120, measure)).toEqual(['10+ : Rp 1', '20+ : Rp 2', '30+ : Rp 3']);
    const cut = wrapSegments(segs, 120, measure, 2);
    expect(cut).toHaveLength(2);
    expect(cut[1].endsWith('…')).toBe(true);
    expect(wrapSegments([], 100, measure)).toEqual([]);
  });
});
