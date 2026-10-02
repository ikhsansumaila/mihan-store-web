// Tes unit pembentuk teks WhatsApp dan generator invoice (jsPDF ditiru).
import { buildAdminSummaryText, buildCustomerConfirmText, newIdempotencyKey, waLink, waNumber } from '../shop/format';

const mockDocs = [];
jest.mock('jspdf', () => ({
  __esModule: true,
  default: function MockPdf() {
    const doc = {
      texts: [],
      images: [],
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
      addImage(...a) {
        doc.images.push(a);
      },
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

const order = {
  orderNo: 'MS-261002-0001',
  status: 'paid',
  subtotal: 145000,
  discount: 5000,
  discountNote: 'Promo',
  shippingFee: 12000,
  total: 152000,
  createdAt: '2026-10-02T03:00:00Z',
  recipient: { name: 'Budi *Penerima*', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang', postalCode: '15111' },
  items: [
    { name: 'Kerupuk Udang', unitPrice: 45000, qty: 2, lineTotal: 90000 },
    { name: 'Kerupuk Bawang', unitPrice: 55000, qty: 1, lineTotal: 55000 },
  ],
};

test('waNumber/waLink: normalisasi ke 62xxx dan menolak nomor tidak valid', () => {
  expect(waNumber('+62 812-3456-7890')).toBe('6281234567890');
  expect(waNumber('081234567890')).toBe('6281234567890');
  expect(waNumber('12345')).toBe('');
  expect(waNumber('')).toBe('');
  expect(waLink('', 'x')).toBe('');
  expect(waLink('+6281234567890', 'a b&c')).toBe('https://wa.me/6281234567890?text=a%20b%26c');
});

test('teks konfirmasi pelanggan memuat nomor pesanan, item, total, nama; tanpa alamat/telepon', () => {
  const t = buildCustomerConfirmText(order, 'Akun Google Berbeda');
  expect(t).toContain('MS-261002-0001');
  expect(t).toContain('Kerupuk Udang x2 = Rp 90.000');
  expect(t).toContain('Total: Rp 152.000');
  expect(t).toContain(`Nama: ${order.recipient.name}`);
  expect(t).not.toContain('Akun Google Berbeda'); // nama penerima diutamakan, bukan nama akun login
  expect(t).not.toContain('Melati');
  expect(t).not.toContain('6281311112222');
});

test('teks konfirmasi: nama akun hanya cadangan bila nama penerima kosong, lalu "-"', () => {
  const noRecipient = { ...order, recipient: { ...order.recipient, name: '  ' } };
  expect(buildCustomerConfirmText(noRecipient, 'Akun Google')).toContain('Nama: Akun Google');
  expect(buildCustomerConfirmText({ ...order, recipient: undefined }, 'Akun Google')).toContain('Nama: Akun Google');
  expect(buildCustomerConfirmText({ ...order, recipient: undefined }, '')).toContain('Nama: -');
  expect(buildCustomerConfirmText({ ...order, recipient: undefined })).toContain('Nama: -');
});

test('ringkasan admin ke pelanggan memuat diskon, ongkir, total, dan rekening bila menunggu pembayaran', () => {
  const store = { bank_name: 'BCA', bank_account_number: '123', bank_account_holder: 'Toko' };
  const t = buildAdminSummaryText({ ...order, status: 'pending_payment' }, store);
  expect(t).toContain('Diskon (Promo): -Rp 5.000');
  expect(t).toContain('Ongkir: Rp 12.000');
  expect(t).toContain('*Total: Rp 152.000*');
  expect(t).toContain('BCA 123');
  expect(buildAdminSummaryText(order, store)).not.toContain('BCA 123'); // sudah dibayar
});

test('newIdempotencyKey menghasilkan UUID v4 unik', () => {
  const a = newIdempotencyKey();
  expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(newIdempotencyKey()).not.toBe(a);
});

test('invoice pesanan: nomor pesanan, penerima, diskon, ongkir, total, rekening Pengaturan Toko, LUNAS', async () => {
  window.open = () => null;
  const inv = orderToInvoice(order, { bank_name: 'Mandiri', bank_account_number: '999', bank_account_holder: 'Toko Uji' });
  expect(inv.isLunas).toBe(true);
  const doc = await generateInvoicePdf(inv, { images: {} });
  const all = doc.texts.join('|');
  for (const s of ['NO. PESANAN', ': MS-261002-0001', 'KIRIM KE', 'Budi *Penerima*', 'SUBTOTAL', 'DISKON (PROMO)', 'ONGKIR', 'TOTAL', 'MANDIRI', '999', 'TOKO UJI', 'LUNAS']) {
    expect(all).toContain(s);
  }
  expect(all).toMatch(/-Rp\s?5\.000/);
  expect(all).toMatch(/Rp\s?152\.000/);
  // Belum dibayar -> tanpa LUNAS; rekening kosong -> rekening default invoice manual.
  const doc2 = await generateInvoicePdf(orderToInvoice({ ...order, status: 'pending_payment' }, {}), { images: {}, open: false });
  expect(doc2.texts.join('|')).not.toContain('LUNAS');
  expect(doc2.texts.join('|')).toContain('3452271335');
});

test('invoice manual tetap sama: tanpa nomor pesanan/diskon/ongkir, total = jumlah item', async () => {
  const doc = await generateInvoicePdf(
    { customerName: 'Pelanggan', items: [{ name: 'Tepung', qty: 2, price: 10000, total: 20000 }], isLunas: false },
    { images: {}, open: false }
  );
  const all = doc.texts.join('|');
  expect(all).toContain(': PELANGGAN');
  expect(all).not.toContain('NO. PESANAN');
  expect(all).not.toContain('ONGKIR');
  expect(all).toContain('BCA');
  expect(all).toContain('QOMARIAH AKMALA');
  expect(all).toMatch(/TOTAL\|Rp\s?20\.000/);
});
