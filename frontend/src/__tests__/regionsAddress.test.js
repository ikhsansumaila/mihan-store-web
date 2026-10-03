// Tes alamat tersusun: formatFullAddress, teks WhatsApp admin -> pelanggan, invoice PDF pesanan, dan detail
// pesanan (pelanggan & admin). Pesanan lama tanpa wilayah tetap memakai alamat + kota.
import { buildAdminSummaryText, buildCustomerConfirmText, flattenAddress, formatFullAddress } from '../shop/format';

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

const REGION = {
  province: { code: '31', name: 'DKI Jakarta' },
  regency: { code: '31.74', name: 'Kota Administrasi Jakarta Selatan' },
  district: { code: '31.74.06', name: 'Cilandak' },
  village: { code: '31.74.06.1004', name: 'Lebak Bulus' },
};
const FULL = 'Jl. Mawar No. 5, RT 01/RW 02, Lebak Bulus, Kec. Cilandak, Kota Administrasi Jakarta Selatan, DKI Jakarta 12440';

const newOrder = {
  orderNo: 'MS-261003-0007',
  status: 'pending_payment',
  subtotal: 90000,
  discount: 0,
  shippingFee: 0,
  total: 90000,
  createdAt: '2026-10-03T03:00:00Z',
  recipient: {
    name: 'Siti Penerima',
    phone: '+6281311112222',
    address: 'Jl. Mawar No. 5\nRT 01/RW 02',
    city: 'Kota Administrasi Jakarta Selatan',
    postalCode: '12440',
    region: REGION,
    fullAddress: FULL,
  },
  customer: { name: 'Nama Akun Google' },
  items: [{ name: 'Kerupuk Udang', unitPrice: 45000, qty: 2, lineTotal: 90000 }],
};
const oldOrder = {
  ...newOrder,
  orderNo: 'MS-261001-0900',
  recipient: { name: 'Pembeli Lama', phone: '+6281200000000', address: 'Jl. Lama No. 1', city: 'Tangerang', postalCode: null, region: null },
};

test('formatFullAddress: pesanan baru (wilayah) dan lama (alamat + kota)', () => {
  expect(formatFullAddress(newOrder.recipient)).toBe(FULL);
  expect(formatFullAddress(oldOrder.recipient)).toBe('Jl. Lama No. 1, Tangerang');
  expect(formatFullAddress({ address: 'Jl. A', city: 'Bogor', postalCode: '16111' })).toBe('Jl. A, Bogor 16111');
  expect(formatFullAddress(null)).toBe('');
  expect(flattenAddress(' Jl. A ,\r\n\r\n  Blok   B \n')).toBe('Jl. A, Blok B');
});

test('teks WhatsApp admin -> pelanggan memuat alamat tersusun (tanpa nama akun); teks konfirmasi pelanggan tidak berubah', () => {
  const t = buildAdminSummaryText(newOrder, {});
  expect(t).toContain(`Alamat pengiriman:\n${FULL}`);
  expect(t).not.toContain('Nama Akun Google');
  expect(buildAdminSummaryText(oldOrder, {})).toContain('Alamat pengiriman:\nJl. Lama No. 1, Tangerang');
  const c = buildCustomerConfirmText(newOrder, 'Nama Akun Google');
  expect(c).not.toContain('Mawar');
  expect(c).not.toContain('Lebak Bulus');
  expect(c).toContain('Nama: Siti Penerima');
});

test('invoice PDF pesanan: KIRIM KE memakai alamat tersusun; pesanan lama tetap alamat + kota', async () => {
  mockDocs.length = 0;
  await generateInvoicePdf(orderToInvoice(newOrder, {}), { images: {} });
  let texts = mockDocs[0].texts;
  expect(texts).toContain('KIRIM KE');
  expect(texts).toContain(FULL);
  expect(texts).toContain('Siti Penerima');
  await generateInvoicePdf(orderToInvoice(oldOrder, {}), { images: {} });
  texts = mockDocs[1].texts;
  expect(texts).toContain('Jl. Lama No. 1, Tangerang');
});
