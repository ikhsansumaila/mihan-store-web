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
      if (mockState.pricingHold && path.endsWith('/pricing')) return new Promise((resolve, reject) => mockState.pricingHold.push({ resolve, reject }));
      if (mockState.pricingError && path.endsWith('/pricing')) return Promise.reject(Object.assign(new Error(mockState.pricingError), { status: 409 }));
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

describe('form diskon & ongkir pesanan', () => {
  test('tampil berformat; tanpa perubahan payload sama; ketik & tempel -> total baru dan payload angka bulat', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const discount = inputInLabel('Diskon (Rp)');
    const shipping = inputInLabel('Ongkir (Rp)');
    expect(discount.value).toBe('5.000');
    expect(shipping.value).toBe('12.000');
    await click(btn('Simpan diskon & ongkir'));
    expect(lastPricing().body).toEqual({ discount: 5000, discountNote: 'Promo', shippingFee: 12000 });

    typeInto(discount, '150000');
    pasteInto(shipping, 'Rp 25.000');
    expect(discount.value).toBe('150.000');
    expect(shipping.value).toBe('25.000');
    expect(container.textContent).toContain('Total baru: Rp 1.325.000');
    await click(btn('Simpan diskon & ongkir'));
    expect(lastPricing().body).toEqual({ discount: 5000, discountNote: 'Promo', shippingFee: 12000 }); // belum terkirim
    await click(btn('Ya, simpan'));
    expect(lastPricing().body).toEqual({ discount: 150000, discountNote: 'Promo', shippingFee: 25000 });

    // Dikosongkan -> dikirim 0 seperti sebelumnya.
    typeInto(discount, '');
    setRaw(discount, '', 0, 'deleteContentBackward');
    expect(discount.value).toBe('');
    await click(btn('Simpan diskon & ongkir'));
    await click(btn('Ya, simpan'));
    expect(lastPricing().body).toEqual({ discount: 0, discountNote: 'Promo', shippingFee: 25000 });
  });

  test('validasi lama: diskon > subtotal dan ongkir > Rp 10.000.000 ditolak tanpa PATCH', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const discount = inputInLabel('Diskon (Rp)');
    const shipping = inputInLabel('Ongkir (Rp)');
    typeInto(discount, '1450001');
    expect(discount.value).toBe('1.450.001');
    expect(discount.getAttribute('aria-invalid')).toBe('true');
    await click(btn('Simpan diskon & ongkir'));
    expect(container.textContent).toContain('Diskon tidak boleh melebihi subtotal');
    typeInto(discount, '0');
    typeInto(shipping, '10000001');
    expect(shipping.value).toBe('10.000.001');
    await click(btn('Simpan diskon & ongkir'));
    expect(container.textContent).toContain('Ongkir maksimal Rp 10.000.000');
    expect(lastPricing()).toBeUndefined();
  });
});

const confirmSheet = () => container.querySelector('[data-testid="pricing-confirm"]');
const pricingCount = () => mockState.calls.filter((c) => c.method === 'PATCH' && c.url === 'admin/orders/5/pricing').length;

describe('konfirmasi simpan diskon & ongkir', () => {
  beforeEach(() => {
    mockState.pricingError = null;
    mockState.pricingHold = null;
  });

  test('perubahan ongkir: dialog berisi lama -> baru, total baru, kalimat notifikasi; Ya, simpan mengirim', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    typeInto(inputInLabel('Ongkir (Rp)'), '15000');
    await click(btn('Simpan diskon & ongkir'));
    expect(pricingCount()).toBe(0);
    const sh = confirmSheet();
    const dlg = sh.querySelector('[role="dialog"]');
    expect(dlg.getAttribute('aria-modal')).toBe('true');
    expect(document.getElementById(dlg.getAttribute('aria-labelledby')).textContent).toBe('Konfirmasi perubahan harga');
    expect(sh.textContent).toContain('Anda akan mengubah harga pesanan MS-261002-0001:');
    expect(sh.querySelector('[data-testid="confirm-shipping"]').textContent).toMatch(/Rp 12\.000.*→.*Rp 15\.000/);
    // Diskon tidak berubah: nilai saja tanpa panah.
    expect(sh.querySelector('[data-testid="confirm-discount"]').textContent).toBe('DiskonRp 5.000');
    expect(sh.querySelector('[data-testid="confirm-note"]').textContent).toContain('Promo');
    expect(sh.querySelector('[data-testid="confirm-total"]').textContent).toContain('Rp 1.460.000');
    expect(sh.querySelector('[data-testid="confirm-total"] strong')).not.toBeNull();
    expect(sh.querySelector('[data-testid="confirm-notify"]').textContent).toBe('Pelanggan akan menerima notifikasi.');
    expect(document.activeElement).toBe(dlg);
    await click(btn('Ya, simpan'));
    expect(pricingCount()).toBe(1);
    expect(lastPricing().body).toEqual({ discount: 5000, discountNote: 'Promo', shippingFee: 15000 });
    expect(confirmSheet()).toBeNull();
  });

  test('ongkir dikosongkan ke Rp 0: tertulis jelas "Rp 12.000 → Rp 0"', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const shipping = inputInLabel('Ongkir (Rp)');
    typeInto(shipping, '');
    setRaw(shipping, '', 0, 'deleteContentBackward');
    await click(btn('Simpan diskon & ongkir'));
    expect(confirmSheet().querySelector('[data-testid="confirm-shipping"]').textContent).toMatch(/Rp 12\.000.*→.*Rp 0$/);
    expect(confirmSheet().querySelector('[data-testid="confirm-notify"]')).not.toBeNull();
  });

  test('hanya keterangan berubah: konfirmasi tanpa kalimat notifikasi', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const note = inputInLabel('Keterangan diskon (opsional)');
    await act(async () => setNative(note, 'Promo baru'));
    await click(btn('Simpan diskon & ongkir'));
    const sh = confirmSheet();
    expect(sh).not.toBeNull();
    expect(sh.querySelector('[data-testid="confirm-note"]').textContent).toContain('Promo baru');
    expect(sh.querySelector('[data-testid="confirm-notify"]')).toBeNull();
    expect(sh.querySelector('[data-testid="confirm-shipping"]').textContent).not.toContain('→');
  });

  test('tanpa perubahan: langsung simpan tanpa konfirmasi (perilaku lama)', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    await click(btn('Simpan diskon & ongkir'));
    expect(confirmSheet()).toBeNull();
    expect(pricingCount()).toBe(1);
  });

  test('Batal, X, latar, Escape menutup tanpa menyimpan', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    typeInto(inputInLabel('Ongkir (Rp)'), '15000');
    const closers = [
      () => click(btn('Batal')),
      () => click(confirmSheet().querySelector('button[aria-label="Tutup"]')),
      () => click(confirmSheet().querySelector('[data-testid="pricing-confirm-backdrop"]')),
      async () => {
        await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
        await flush();
      },
    ];
    for (const close of closers) {
      // eslint-disable-next-line no-await-in-loop
      await click(btn('Simpan diskon & ongkir'));
      expect(confirmSheet()).not.toBeNull();
      // eslint-disable-next-line no-await-in-loop
      await close();
      expect(confirmSheet()).toBeNull();
    }
    expect(pricingCount()).toBe(0);
  });

  test('klik ganda: hanya satu PATCH, tombol nonaktif saat menyimpan', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    mockState.pricingHold = [];
    await renderAt('/admin/orders/5');
    typeInto(inputInLabel('Ongkir (Rp)'), '15000');
    await click(btn('Simpan diskon & ongkir'));
    const yes = btn('Ya, simpan');
    await act(async () => {
      yes.click();
      yes.click();
    });
    await flush();
    expect(pricingCount()).toBe(1);
    expect(btn('Menyimpan...', confirmSheet())).toBeDefined();
    expect([...confirmSheet().querySelectorAll('button')].find((b) => b.textContent.trim() === 'Menyimpan...').disabled).toBe(true);
    await act(async () => mockState.pricingHold[0].resolve(order));
    await flush();
    expect(confirmSheet()).toBeNull();
  });

  test('galat server tampil di dalam dialog; dialog tetap terbuka', async () => {
    mockState.admin = { '/orders/5': order, '/settings': { settings: {} } };
    mockState.pricingError = 'Status pesanan sudah berubah';
    await renderAt('/admin/orders/5');
    typeInto(inputInLabel('Ongkir (Rp)'), '15000');
    await click(btn('Simpan diskon & ongkir'));
    await click(btn('Ya, simpan'));
    expect(confirmSheet()).not.toBeNull();
    expect(confirmSheet().textContent).toContain('Status pesanan sudah berubah');
  });
});

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
