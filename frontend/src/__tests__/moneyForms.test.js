// Tes integrasi kolom uang (MoneyInput) di form: produk admin (harga eceran + jenjang Rp), diskon/ongkir pesanan
// admin, dan Buat Invoice manual. Memastikan tampilan berformat "1.250.000" dan payload tetap angka bulat yang sama.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { pasteInto, setRaw, typeInto } from '../testUtils/moneyKeys';

const mockState = { calls: [], admin: {}, pdf: [] };

jest.mock('axios', () => ({
  __esModule: true,
  default: {
    get: (url) => Promise.resolve({ data: url.endsWith('/auth/me') ? { user: { name: 'Admin', role: 'admin' } } : [] }),
    post: () => Promise.resolve({ data: {} }),
  },
}));
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });
jest.mock('../invoicePdf', () => {
  const actual = jest.requireActual('../invoicePdf');
  return { ...actual, generateInvoicePdf: (args) => mockState.pdf.push(args) };
});
jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path, opts) => {
      mockState.calls.push({ method: opts?.method || 'GET', url: `admin${path}`, body: opts?.body });
      if (path === '/me') return Promise.resolve({ user: { email: 'admin@example.com' } });
      if (mockState.pricingHold && (path.endsWith('/pricing') || path.endsWith('/confirm'))) return new Promise((resolve, reject) => mockState.pricingHold.push({ resolve, reject }));
      if (path.endsWith('/shipping-default')) {
        mockState.defaultCalls = (mockState.defaultCalls || []).concat([{ path, body: opts?.body }]);
        if (mockState.defaultFail) return Promise.reject(Object.assign(new Error(mockState.defaultFail), { status: 409 }));
        if (mockState.defaultHold) return new Promise((resolve) => mockState.defaultHold.push(resolve));
        return Promise.resolve({ villageCode: '36.71.01.1001', villageName: 'Sukarasa', districtName: 'Tangerang', shippingFee: 18000, previousFee: null });
      }
      if (path.endsWith('/shipping-suggestions')) {
        mockState.suggestCalls = (mockState.suggestCalls || 0) + 1;
        if (mockState.suggest === 'fail') return Promise.reject(new Error('Layanan sedang tidak tersedia'));
        if (mockState.suggest === 'hang') return new Promise(() => {});
        if (mockState.suggest === 'late') return new Promise((r) => setTimeout(() => r(mockState.lateSugg), 3200));
        return Promise.resolve(mockState.suggest || { suggestions: [], canSetDefault: false, region: {}, currentDefault: null });
      }
      if (mockState.pricingError && (path.endsWith('/pricing') || path.endsWith('/confirm'))) return Promise.reject(Object.assign(new Error(mockState.pricingError), { status: 409 }));
      const key = Object.keys(mockState.admin)
        .filter((k) => path.startsWith(k))
        .sort((a, b) => b.length - a.length)[0];
      return Promise.resolve(key ? mockState.admin[key] : {});
    },
  };
});

const App = require('../App').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

let container;
let root;
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};
const renderAt = async (path) => {
  localStorage.clear();
  window.history.replaceState({}, '', path);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
  await act(async () => {
    root.render(<App />);
  });
  await flush();
  await flush();
};
const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};
const inputInLabel = (text) => [...container.querySelectorAll('label')].find((l) => l.textContent.trim().startsWith(text)).querySelector('input');
const setNative = (el, value) => {
  Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
};

beforeEach(() => {
  mockState.calls = [];
  mockState.admin = {};
  mockState.pdf = [];
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const product = {
  id: 7,
  name: 'Kerupuk Grosir',
  description: 'Gurih',
  categoryId: 1,
  categoryName: 'Kerupuk',
  price: 45000,
  unit: 'pak',
  imagePath: '',
  isActive: true,
  tiers: [
    { minQty: 10, type: 'fixed', value: 42000, unitPrice: 42000 },
    { minQty: 50, type: 'percent', value: 10, unitPrice: 40500 },
  ],
};
const productAdmin = () => ({
  '/products': { items: [product], total: 1, page: 1, perPage: 20 },
  '/categories': { items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] },
});
const lastPut = () => mockState.calls.filter((c) => c.method === 'PUT' && c.url === 'admin/products/7').pop();

describe('form produk', () => {
  test('tanpa perubahan: tampil berformat, payload sama seperti sebelumnya (angka bulat)', async () => {
    mockState.admin = productAdmin();
    await renderAt('/admin/products');
    await click(btn('Ubah'));
    const dialog = container.querySelector('[role="dialog"]');
    const price = dialog.querySelector('#pf-price');
    expect(price.value).toBe('45.000');
    expect(price.getAttribute('inputmode')).toBe('numeric');
    expect(price.placeholder).toBe('15.000');
    expect(dialog.querySelector('input[aria-label="Nilai jenjang 1"]').value).toBe('42.000');
    // Nilai persen tetap kolom desimal biasa (bukan uang).
    const pct = dialog.querySelector('input[aria-label="Nilai jenjang 2"]');
    expect(pct.value).toBe('10');
    expect(pct.getAttribute('inputmode')).toBe('decimal');
    await click(btn('Simpan'));
    expect(lastPut().body).toMatchObject({
      price: 45000,
      tiers: [
        { minQty: 10, type: 'fixed', value: 42000 },
        { minQty: 50, type: 'percent', value: 10 },
      ],
    });
  });

  test('ketik harga & jenjang Rp, tempel "Rp 1.100.000,00": pratinjau benar, payload angka bulat', async () => {
    mockState.admin = productAdmin();
    await renderAt('/admin/products');
    await click(btn('Ubah'));
    const dialog = container.querySelector('[role="dialog"]');
    const price = dialog.querySelector('#pf-price');
    typeInto(price, '1250000');
    expect(price.value).toBe('1.250.000');
    const t1 = dialog.querySelector('input[aria-label="Nilai jenjang 1"]');
    typeInto(t1, '1200000');
    expect(t1.value).toBe('1.200.000');
    await click([...container.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith('+ Tambah jenjang')));
    const row3 = dialog.querySelectorAll('[data-testid="tier-row"]')[2];
    await act(async () => setNative(row3.querySelector('input[aria-label="Jumlah minimal jenjang 3"]'), '100'));
    // Persen 10% dari 1.250.000 = 1.125.000; jenjang 100 harus lebih murah -> 1.100.000.
    pasteInto(row3.querySelector('input[aria-label="Nilai jenjang 3"]'), 'Rp 1.100.000,00');
    expect(row3.querySelector('input[aria-label="Nilai jenjang 3"]').value).toBe('1.100.000');
    const preview = dialog.querySelector('[data-testid="tier-preview"]').textContent;
    expect(preview).toContain('Rp 1.250.000');
    expect(preview).toContain('Rp 1.200.000');
    expect(preview).toContain('Rp 1.125.000');
    expect(preview).toContain('Rp 1.100.000');
    expect(dialog.querySelectorAll('[role="alert"]')).toHaveLength(0);
    await click(btn('Simpan'));
    const body = lastPut().body;
    expect(body.price).toBe(1250000);
    expect(body.tiers).toEqual([
      { minQty: 10, type: 'fixed', value: 1200000 },
      { minQty: 50, type: 'percent', value: 10 },
      { minQty: 100, type: 'fixed', value: 1100000 },
    ]);
    [body.price, ...body.tiers.map((t) => t.value)].forEach((v) => expect(Number.isInteger(v)).toBe(true));
  });

  test('validasi lama tetap: harga kosong ditolak, maks. 10 digit, % berdesimal -> Rp dikosongkan', async () => {
    mockState.admin = productAdmin();
    await renderAt('/admin/products');
    await click(btn('Ubah'));
    const dialog = container.querySelector('[role="dialog"]');
    const price = dialog.querySelector('#pf-price');
    // Batas 10 digit (server maks. Rp 1.000.000.000): digit ke-11 ditolak.
    typeInto(price, '12345678901');
    expect(price.value).toBe('1.234.567.890');
    expect(price.getAttribute('aria-invalid')).toBe('true');
    await act(async () => dialog.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await flush();
    expect(dialog.textContent).toContain('Harga harus bilangan bulat 0 sampai 1.000.000.000');
    // Kosong -> pesan sama, tidak ada PUT.
    act(() => price.focus());
    price.setSelectionRange(0, price.value.length);
    setRaw(price, '', 0, 'deleteContentBackward');
    expect(price.value).toBe('');
    await act(async () => dialog.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await flush();
    expect(mockState.calls.some((c) => c.method === 'PUT')).toBe(false);
    // Jenjang 2 persen "12,5" lalu diganti ke Rp: nilai dikosongkan dan ditandai wajib diisi.
    const row2 = dialog.querySelectorAll('[data-testid="tier-row"]')[1];
    await act(async () => setNative(row2.querySelector('input[aria-label="Nilai jenjang 2"]'), '12,5'));
    await click([...row2.querySelectorAll('button')].find((b) => b.textContent === 'Rp'));
    expect(row2.querySelector('input[aria-label="Nilai jenjang 2"]').value).toBe('');
    expect(row2.textContent).toContain('harga (Rp) harus bilangan bulat minimal 1');
  });
});

const order = {
  id: 5,
  orderNo: 'MS-261002-0001',
  status: 'pending_payment',
  subtotal: 1450000,
  discount: 5000,
  discountNote: 'Promo',
  shippingFee: 12000,
  total: 1457000,
  recipient: { name: 'Budi Penerima', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang', postalCode: '15111' },
  items: [{ productId: 1, name: 'Kerupuk Finna Udang', unitPrice: 725000, qty: 2, lineTotal: 1450000 }],
  history: [{ from: null, to: 'pending_payment', toLabel: 'Menunggu pembayaran', actor: 'pelanggan:budi', createdAt: '2026-10-02T03:00:00Z' }],
  itemCount: 2,
  createdAt: '2026-10-02T03:00:00Z',
  updatedAt: '2026-10-02T03:00:00Z',
  pricingLocked: false,
  allowedNext: ['paid', 'cancelled'],
  customer: { name: 'Budi', username: 'budi', email: 'budi@example.com' },
};
const lastPricing = () => mockState.calls.filter((c) => c.method === 'PATCH' && c.url === 'admin/orders/5/pricing').pop();

describe('Buat Invoice manual', () => {
  test('harga satuan berformat; item, total, WhatsApp, dan PDF memakai angka bulat', async () => {
    const opened = [];
    const alerts = [];
    const origOpen = window.open;
    const origAlert = window.alert;
    window.open = (url) => opened.push(url);
    window.alert = (m) => alerts.push(m);
    try {
      await renderAt('/admin/invoice');
      const name = container.querySelector('input[placeholder="Nama Pelanggan"]');
      await act(async () => setNative(name, 'Bu Ani'));
      await act(async () => setNative(container.querySelector('input[placeholder="Misal: Tepung Terigu"]'), 'Kerupuk'));
      await act(async () => setNative(container.querySelector('input[placeholder="1"]'), '3'));
      const price = container.querySelector('input[aria-label="Harga Satuan (Rp)"]');
      expect(price.getAttribute('inputmode')).toBe('numeric');
      typeInto(price, '1250000');
      expect(price.value).toBe('1.250.000');
      await click(btn('Tambah'));
      expect(alerts).toEqual([]);
      expect(price.value).toBe('');
      const row = container.querySelector('tbody tr');
      expect(row.textContent.replace(/\s/g, ' ')).toMatch(/Rp\s1\.250\.000/);
      expect(row.textContent.replace(/\s/g, ' ')).toMatch(/Rp\s3\.750\.000/);
      // Tempel dengan desimal.
      await act(async () => setNative(container.querySelector('input[placeholder="Misal: Tepung Terigu"]'), 'Saos'));
      await act(async () => setNative(container.querySelector('input[placeholder="1"]'), '1'));
      pasteInto(price, '15.000,00');
      expect(price.value).toBe('15.000');
      await click(btn('Tambah'));
      await click([...container.querySelectorAll('button')].find((b) => b.textContent.includes('Preview PDF')));
      expect(mockState.pdf).toHaveLength(1);
      expect(mockState.pdf[0].items).toEqual([
        { name: 'Kerupuk', qty: 3, price: 1250000, total: 3750000 },
        { name: 'Saos', qty: 1, price: 15000, total: 15000 },
      ]);
      await click([...container.querySelectorAll('button')].find((b) => b.textContent.includes('Kirim WhatsApp')));
      const text = decodeURIComponent(opened[0].split('text=')[1]).replace(/ /g, ' ');
      expect(text).toContain('3 x Rp 1.250.000 = Rp 3.750.000');
      expect(text).toContain('*TOTAL:* Rp 3.765.000');
    } finally {
      window.open = origOpen;
      window.alert = origAlert;
    }
  });
});

// ---------- Konfirmasi pesanan (pending_confirmation, POST /confirm) ----------

// ---------- Kartu "Diskon & ongkir" + alur dua langkah (isi -> konfirmasi) ----------
const awaiting = {
  ...order,
  status: 'pending_confirmation',
  discount: 0,
  discountNote: null,
  shippingFee: 0,
  total: 1450000,
  canConfirm: true,
  pricingLocked: false,
  allowedNext: ['cancelled'],
};
const confirmCalls = () => mockState.calls.filter((c) => c.method === 'POST' && c.url === 'admin/orders/5/confirm');
const pricingCalls = () => mockState.calls.filter((c) => c.method === 'PATCH' && c.url === 'admin/orders/5/pricing');
const sheet = () => container.querySelector('[data-testid="price-sheet"]');
const review = () => container.querySelector('[data-testid="price-review"]');
const anySheet = () => sheet() || review();
const inSheet = (label) => [...sheet().querySelectorAll('label')].find((l) => l.textContent.trim().startsWith(label)).querySelector('input');
const sBtn = (label) => [...anySheet().querySelectorAll('button')].find((b) => b.textContent.trim() === label);
const card = () => container.querySelector('[data-testid="pricing-summary"]');
const SUGG = {
  region: { villageCode: '36.71.01.1001', villageName: 'Sukarasa', districtName: 'Tangerang', regencyName: 'Kota Tangerang' },
  canSetDefault: true,
  currentDefault: null,
  suggestions: [
    { source: 'default', sources: ['default'], fee: 12000, orderNo: null, date: new Date().toISOString(), regionLabel: 'Sukarasa' },
    { source: 'village', sources: ['village', 'customer'], fee: 15000, orderNo: 'MS-261005-0003', date: new Date(Date.now() - 3 * 86400000).toISOString(), regionLabel: 'Sukarasa' },
    { source: 'district', sources: ['district'], fee: 17000, orderNo: 'MS-261005-0003', date: new Date(Date.now() - 3 * 86400000).toISOString(), regionLabel: 'Tangerang' },
    { source: 'regency', sources: ['regency'], fee: 20000, orderNo: 'MS-261001-0001', date: new Date().toISOString(), regionLabel: 'Kota Tangerang' },
  ],
};
const settle = async (ms = 80) => {
  await act(async () => new Promise((r) => setTimeout(r, ms)));
  await flush();
};
const openSheet = async (label) => {
  await click(btn(label));
  await settle();
  expect(sheet()).not.toBeNull();
};
const lanjut = async () => {
  await click(sBtn('Lanjut'));
  await settle();
  expect(review()).not.toBeNull();
};
const titleOf = (el) => document.getElementById(el.querySelector('[role="dialog"]').getAttribute('aria-labelledby')).textContent;
const offerSheet = () => container.querySelector('[data-testid="default-offer"]');
const confirmedReply = (fee) => ({ ...awaiting, status: 'pending_payment', canConfirm: false, shippingFee: fee, total: 1450000 + fee, allowedNext: ['paid', 'cancelled'], updatedAt: `2026-10-08T0${fee % 7}:00:00Z` });
const escape = async () => {
  await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
  await flush();
};

beforeEach(() => {
  mockState.pricingError = null;
  mockState.pricingHold = null;
  mockState.suggest = null;
  mockState.suggestCalls = 0;
  mockState.defaultCalls = [];
  mockState.defaultFail = null;
});

describe('kartu Diskon & ongkir (ringkasan)', () => {
  test('ringkasan baca-saja tanpa input inline; tombol per status', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const c = card();
    expect(c.parentElement.querySelectorAll('input')).toHaveLength(0);
    expect(c.textContent).toContain('Diskon (Promo)');
    expect(c.textContent).toContain('-Rp 5.000');
    expect(container.querySelector('[data-testid="pricing-shipping"]').textContent).toBe('Rp 12.000');
    expect(c.textContent).toContain('Rp 1.457.000');
    expect([...container.querySelectorAll('label')].some((l) => l.textContent.trim().startsWith('Ongkir (Rp)'))).toBe(false);
    expect(btn('Ubah diskon & ongkir')).toBeTruthy();
    expect(btn('Konfirmasi pesanan')).toBeUndefined();
    expect(anySheet()).toBeNull();
    act(() => root.unmount());
    container.remove();
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    expect(container.querySelector('[data-testid="confirm-hint"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="pricing-shipping"]').textContent).toBe('belum diisi');
    expect(btn('Konfirmasi pesanan')).toBeTruthy();
    expect(btn('Tandai Dibayar')).toBeUndefined();
    act(() => root.unmount());
    container.remove();
    mockState.admin = { '/orders/5': { ...order, status: 'paid', pricingLocked: true, allowedNext: ['completed', 'cancelled'] }, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    expect(btn('Ubah diskon & ongkir')).toBeUndefined();
    expect(container.textContent).toContain('Terkunci');
  });
});

describe('ubah diskon & ongkir (dua langkah)', () => {
  test('sheet 1: nilai awal, fokus diskon, hanya input + total sementara; Lanjut nonaktif tanpa perubahan', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    expect(titleOf(sheet())).toBe('Ubah diskon & ongkir');
    expect(inSheet('Diskon (Rp)').value).toBe('5.000');
    expect(inSheet('Ongkir (Rp)').value).toBe('12.000');
    expect(inSheet('Keterangan diskon').value).toBe('Promo');
    expect(document.activeElement).toBe(inSheet('Diskon (Rp)'));
    expect(mockState.suggestCalls).toBe(0); // tanpa saran di mode ubah
    expect(sheet().querySelector('[data-testid="price-summary"]')).toBeNull(); // lama -> baru hanya di sheet 2
    expect(sheet().querySelector('[data-testid="confirm-notify"]')).toBeNull();
    expect(sBtn('Lanjut').disabled).toBe(true);
    expect(sheet().querySelector('[data-testid="price-nochange"]').textContent).toBe('Belum ada perubahan.');
    typeInto(inSheet('Diskon (Rp)'), '150000');
    pasteInto(inSheet('Ongkir (Rp)'), 'Rp 25.000');
    expect(sheet().querySelector('[data-testid="price-draft-total"]').textContent).toContain('Total sementaraRp 1.325.000');
    expect(sBtn('Lanjut').disabled).toBe(false);
  });

  test('Lanjut -> sheet 2 ringkasan lama -> baru; Kembali menjaga isian; Ya, simpan mengirim PATCH', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    typeInto(inSheet('Diskon (Rp)'), '150000');
    pasteInto(inSheet('Ongkir (Rp)'), 'Rp 25.000');
    await lanjut();
    expect(sheet()).toBeNull(); // hanya satu sheet
    expect(titleOf(review())).toBe('Konfirmasi perubahan harga');
    expect(document.activeElement).toBe(document.getElementById('price-review-title'));
    expect(review().querySelectorAll('input')).toHaveLength(0);
    expect(review().querySelector('[data-testid="confirm-shipping"]').textContent).toMatch(/Rp 12\.000.*→.*Rp 25\.000/);
    expect(review().querySelector('[data-testid="confirm-discount"]').textContent).toMatch(/Rp 5\.000.*→.*Rp 150\.000/);
    expect(review().querySelector('[data-testid="confirm-total"]').textContent).toContain('Rp 1.325.000');
    expect(review().querySelector('[data-testid="confirm-notify"]').textContent).toBe('Pelanggan akan menerima notifikasi.');
    // Kembali: isian tetap, fokus ke kolom ongkir.
    await click(sBtn('Kembali'));
    await settle();
    expect(review()).toBeNull();
    expect(inSheet('Diskon (Rp)').value).toBe('150.000');
    expect(inSheet('Ongkir (Rp)').value).toBe('25.000');
    expect(document.activeElement).toBe(inSheet('Ongkir (Rp)'));
    await lanjut();
    await click(sBtn('Ya, simpan'));
    expect(pricingCalls()).toHaveLength(1);
    expect(lastPricing().body).toEqual({ discount: 150000, discountNote: 'Promo', shippingFee: 25000 });
    expect(anySheet()).toBeNull();
  });

  test('perubahan ke nol tertulis jelas; hanya keterangan berubah: tanpa kalimat notifikasi', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    const ship = inSheet('Ongkir (Rp)');
    typeInto(ship, '');
    setRaw(ship, '', 0, 'deleteContentBackward');
    await lanjut();
    expect(review().querySelector('[data-testid="confirm-shipping"]').textContent).toMatch(/Rp 12\.000.*→.*Rp 0$/);
    await click(sBtn('Kembali'));
    await settle();
    typeInto(inSheet('Ongkir (Rp)'), '12000');
    await act(async () => setNative(inSheet('Keterangan diskon'), 'Promo baru'));
    await lanjut();
    expect(review().querySelector('[data-testid="confirm-note"]').textContent).toContain('Promo baru');
    expect(review().querySelector('[data-testid="confirm-notify"]')).toBeNull();
  });

  test('validasi inline: Lanjut nonaktif, tanpa kirim', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    typeInto(inSheet('Diskon (Rp)'), '1450001');
    expect(sheet().querySelector('[data-testid="price-invalid"]').textContent).toBe('Diskon tidak boleh melebihi subtotal');
    expect(sBtn('Lanjut').disabled).toBe(true);
    typeInto(inSheet('Diskon (Rp)'), '0');
    typeInto(inSheet('Ongkir (Rp)'), '10000001');
    expect(sheet().querySelector('[data-testid="price-invalid"]').textContent).toBe('Ongkir maksimal Rp 10.000.000');
    await act(async () => sheet().querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(review()).toBeNull();
    expect(pricingCalls()).toHaveLength(0);
  });

  test('menutup: Batal/X/latar/Escape di sheet 1 dan X/latar/Escape di sheet 2 membuang draf', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const edits = [
      () => click(sBtn('Batal')),
      () => click(sheet().querySelector('button[aria-label="Tutup"]')),
      () => click(sheet().querySelector('[data-testid="price-sheet-backdrop"]')),
      escape,
    ];
    for (const close of edits) {
      // eslint-disable-next-line no-await-in-loop
      await openSheet('Ubah diskon & ongkir');
      typeInto(inSheet('Ongkir (Rp)'), '99000');
      // eslint-disable-next-line no-await-in-loop
      await close();
      expect(anySheet()).toBeNull();
    }
    const reviews = [
      () => click(review().querySelector('button[aria-label="Tutup"]')),
      () => click(review().querySelector('[data-testid="price-review-backdrop"]')),
      escape,
    ];
    for (const close of reviews) {
      // eslint-disable-next-line no-await-in-loop
      await openSheet('Ubah diskon & ongkir');
      typeInto(inSheet('Ongkir (Rp)'), '99000');
      // eslint-disable-next-line no-await-in-loop
      await lanjut();
      // eslint-disable-next-line no-await-in-loop
      await close();
      expect(anySheet()).toBeNull();
    }
    await openSheet('Ubah diskon & ongkir');
    expect(inSheet('Ongkir (Rp)').value).toBe('12.000'); // draf dibuang
    expect(pricingCalls()).toHaveLength(0);
  });

  test('klik ganda hanya satu kiriman; galat server di sheet 2 lalu Kembali untuk mengedit', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    mockState.pricingHold = [];
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    typeInto(inSheet('Ongkir (Rp)'), '15000');
    await lanjut();
    const yes = sBtn('Ya, simpan');
    await act(async () => {
      yes.click();
      yes.click();
    });
    await flush();
    expect(pricingCalls()).toHaveLength(1);
    expect(sBtn('Menyimpan...').disabled).toBe(true);
    expect(sBtn('Kembali').disabled).toBe(true);
    await act(async () => mockState.pricingHold[0].reject(Object.assign(new Error('Status pesanan sudah berubah'), { status: 409 })));
    await flush();
    expect(review().textContent).toContain('Status pesanan sudah berubah');
    await click(sBtn('Kembali'));
    await settle();
    expect(inSheet('Ongkir (Rp)').value).toBe('15.000');
  });
});

describe('konfirmasi pesanan (dua langkah)', () => {
  test('sheet 1 "Isi ongkir & diskon" fokus ongkir; Rp 0 boleh; sheet 2 selalu tampil; body /confirm', async () => {
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Konfirmasi pesanan');
    expect(titleOf(sheet())).toBe('Isi ongkir & diskon');
    expect(document.activeElement).toBe(inSheet('Ongkir (Rp)'));
    expect(sBtn('Lanjut').disabled).toBe(false);
    await lanjut();
    expect(titleOf(review())).toBe('Konfirmasi pesanan MS-261002-0001');
    expect(review().querySelector('[data-testid="confirm-shipping"]').textContent).toBe('OngkirRp 0');
    expect(review().querySelector('[data-testid="confirm-total"]').textContent).toContain('Rp 1.450.000');
    expect(review().querySelector('[data-testid="confirm-notify"]').textContent).toBe('Pelanggan akan dinotifikasi dan diminta membayar.');
    mockState.admin['/orders/5/confirm'] = confirmedReply(0);
    await click(sBtn('Ya, konfirmasi'));
    expect(confirmCalls()).toHaveLength(1);
    expect(confirmCalls()[0].body).toEqual({ discount: 0, discountNote: '', shippingFee: 0 });
    expect(mockState.calls.some((c) => c.method === 'PATCH')).toBe(false);
    expect(anySheet()).toBeNull();
  });

  test('saran inline di sheet 1 mengisi ongkir & total sementara', async () => {
    mockState.suggest = SUGG;
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Konfirmasi pesanan');
    const opts = sheet().querySelector('[data-testid="shipping-options"]');
    expect(opts.textContent).toContain('Default kelurahan Sukarasa');
    expect(opts.textContent).toContain('Terakhir ke kelurahan Sukarasa');
    expect(opts.textContent).toContain('Terakhir ke kecamatan Tangerang');
    expect(opts.textContent).toContain('MS-261005-0003 · 3 hari lalu · sama dengan terakhir pelanggan ini');
    expect(opts.textContent).toContain('Rp 0 (kurir dipesan pembeli)');
    const shipLabel = inSheet('Ongkir (Rp)').closest('label');
    expect(shipLabel.compareDocumentPosition(opts) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    await click(opts.querySelector('[data-option="regency"]'));
    expect(inSheet('Ongkir (Rp)').value).toBe('20.000');
    expect(sheet().querySelector('[data-testid="price-draft-total"]').textContent).toContain('Rp 1.470.000');
    expect(confirmCalls()).toHaveLength(0);
    await lanjut();
    expect(review().querySelector('[data-testid="confirm-shipping"]').textContent).toMatch(/Rp 0.*→.*Rp 20\.000/);
    mockState.admin['/orders/5/confirm'] = confirmedReply(20000);
    await click(sBtn('Ya, konfirmasi'));
    expect(confirmCalls()[0].body).toEqual({ discount: 0, discountNote: '', shippingFee: 20000 });
  });

  test('saran gagal / menggantung / terlambat: tanpa saran, tanpa galat', async () => {
    mockState.lateSugg = SUGG;
    for (const sg of ['fail', 'hang']) {
      mockState.suggest = sg;
      mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
      // eslint-disable-next-line no-await-in-loop
      await renderAt('/admin/orders/5');
      // eslint-disable-next-line no-await-in-loop
      await openSheet('Konfirmasi pesanan');
      expect(sheet().querySelector('[data-testid="shipping-options"]')).toBeNull();
      expect(sheet().querySelector('[role="alert"]')).toBeNull();
      act(() => root.unmount());
      container.remove();
    }
    mockState.suggest = 'late';
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Konfirmasi pesanan');
    await settle(3400);
    expect(sheet().querySelector('[data-testid="shipping-options"]')).toBeNull();
  }, 10000);

  test('klik ganda konfirmasi: satu POST; galat 409 di sheet 2', async () => {
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    mockState.pricingHold = [];
    await renderAt('/admin/orders/5');
    await openSheet('Konfirmasi pesanan');
    await lanjut();
    const yes = sBtn('Ya, konfirmasi');
    await act(async () => {
      yes.click();
      yes.click();
    });
    await flush();
    expect(confirmCalls()).toHaveLength(1);
    await act(async () => mockState.pricingHold[0].reject(Object.assign(new Error('Pesanan ini tidak sedang menunggu konfirmasi. Muat ulang halaman.'), { status: 409 })));
    await flush();
    expect(review().textContent).toContain('tidak sedang menunggu konfirmasi');
    expect(sBtn('Ya, konfirmasi').disabled).toBe(false);
  });
});

describe('popup default ongkir wilayah setelah konfirmasi', () => {
  const confirmWith = async (fee) => {
    await openSheet('Konfirmasi pesanan');
    typeInto(inSheet('Ongkir (Rp)'), String(fee));
    await lanjut();
    mockState.admin['/orders/5/confirm'] = confirmedReply(fee);
    await click(sBtn('Ya, konfirmasi'));
  };

  test('muncul setelah sukses; "Ya" memanggil endpoint tanpa nominal; pesan sukses', async () => {
    mockState.suggest = SUGG;
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await confirmWith(18000);
    expect(anySheet()).toBeNull();
    expect(btn('Ubah diskon & ongkir')).toBeTruthy();
    const sh = offerSheet();
    expect(titleOf(sh)).toBe('Jadikan default ongkir wilayah?');
    expect(sh.textContent).toContain('Ongkir Rp 18.000 untuk Kel./Desa Sukarasa (Kec. Tangerang) baru saja dikonfirmasi');
    expect(sh.textContent).toContain('Jadikan Rp 18.000 default ongkir untuk kelurahan/desa ini?');
    expect(sh.textContent).not.toMatch(/untuk kecamatan ini/);
    expect(sh.querySelector('[data-testid="default-offer-replace"]')).toBeNull();
    await click(btn('Ya, jadikan default'));
    expect(mockState.defaultCalls).toHaveLength(1);
    expect(mockState.defaultCalls[0].body).toEqual({});
    expect(offerSheet()).toBeNull();
    expect(container.querySelector('[data-testid="default-offer-notice"]').textContent).toContain('Default ongkir Kel./Desa Sukarasa disimpan: Rp 18.000');
  });

  test('default lain ada: teks mengganti; "Tidak" hanya menutup; sekali per konfirmasi', async () => {
    mockState.suggest = { ...SUGG, currentDefault: { fee: 15000 } };
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await confirmWith(18000);
    expect(offerSheet().querySelector('[data-testid="default-offer-replace"]').textContent).toBe('Ini akan mengganti default sekarang (Rp 15.000).');
    await click(btn('Tidak'));
    expect(offerSheet()).toBeNull();
    expect(mockState.defaultCalls).toHaveLength(0);
    act(() => root.unmount());
    container.remove();
    mockState.admin = { '/orders/5': confirmedReply(18000), '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    expect(offerSheet()).toBeNull();
  });

  test.each([
    ['ongkir sama dengan default', () => { mockState.suggest = { ...SUGG, currentDefault: { fee: 18000 } }; }, 18000],
    ['tanpa kelurahan', () => { mockState.suggest = { ...SUGG, canSetDefault: false, region: { villageCode: null, districtName: 'Tangerang' } }; }, 18000],
    ['info saran gagal', () => { mockState.suggest = 'fail'; }, 18000],
    ['ongkir Rp 0', () => { mockState.suggest = SUGG; }, 0],
  ])('popup tidak muncul: %s', async (_, arrange, fee) => {
    arrange();
    mockState.admin = { '/orders/5': awaiting, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await confirmWith(fee);
    expect(confirmCalls()).toHaveLength(1);
    expect(offerSheet()).toBeNull();
  });

  test('tidak muncul di "Ubah diskon & ongkir"', async () => {
    mockState.suggest = SUGG;
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await openSheet('Ubah diskon & ongkir');
    typeInto(inSheet('Ongkir (Rp)'), '19000');
    await lanjut();
    await click(sBtn('Ya, simpan'));
    expect(pricingCalls()).toHaveLength(1);
    expect(offerSheet()).toBeNull();
  });
});
