// Tes render (jsdom) harga grosir: form produk admin (jenjang, validasi, pratinjau, 422 per baris), daftar produk
// admin, kartu produk toko (lencana Grosir), keranjang (harga efektif, hemat, petunjuk, penanda perubahan harga +
// "Mengerti"), dan checkout (expectedTotal, 409 price_changed, konfirmasi ulang).
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { fillCheckout } from '../testUtils/regionFixtures';

const mockState = { calls: [], cart: null, products: [], admin: {}, adminError: null, orderReplies: [], ackCart: null };

jest.mock('axios', () => {
  const respond = (method, url, body) => {
    mockState.calls.push({ method, url, body });
    if (url.endsWith('/auth/me')) return { data: { user: { name: 'Budi', role: 'customer' } } };
    if (url.endsWith('/api/products')) return { data: mockState.products };
    if (url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart/ack-prices')) return { data: mockState.ackCart || mockState.cart };
    if (url.endsWith('/api/cart') || url.includes('/api/cart/items')) return { data: mockState.cart };
    if (url.endsWith('/api/store-info')) return { data: {} };
    if (method === 'post' && url.endsWith('/api/orders')) {
      const next = mockState.orderReplies.shift();
      if (next && next.status !== 201) {
        const err = new Error('gagal');
        err.response = { status: next.status, data: next.data };
        throw err;
      }
      return { data: { orderNo: 'MS-261003-0001' } };
    }
    if (url.includes('/api/orders/')) return { data: null };
    return { data: {} };
  };
  const wrap = (fn) => (...a) => {
    try {
      return Promise.resolve(fn(...a));
    } catch (e) {
      return Promise.reject(e);
    }
  };
  return {
    __esModule: true,
    default: {
      get: (url) => require('../testUtils/regionFixtures').mockRegionGet(url) || wrap((u) => respond('get', u))(url),
      post: wrap((url, body) => respond('post', url, body)),
      put: wrap((url, body) => respond('put', url, body)),
      delete: wrap((url) => respond('delete', url)),
    },
  };
});
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });
jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path, opts) => {
      mockState.calls.push({ method: opts?.method || 'GET', url: `admin${path}`, body: opts?.body });
      if (path === '/me') return Promise.resolve({ user: { email: 'admin@example.com' } });
      if ((opts?.method === 'PUT' || opts?.method === 'POST') && mockState.adminError) return Promise.reject(mockState.adminError);
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
const renderAt = async (path, user = null) => {
  localStorage.clear();
  sessionStorage.clear();
  if (user) {
    localStorage.setItem('token', 'tes');
    localStorage.setItem('user', JSON.stringify(user));
  }
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
const USER = { name: 'Budi', role: 'customer', phone: '+6281234567890' };
const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const btnStarts = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith(text));
const setInput = (el, value) => {
  const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
};
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};

beforeEach(() => {
  mockState.calls = [];
  mockState.cart = { items: [], subtotal: 0, itemCount: 0, lineCount: 0, hasUnavailable: false, maxQty: 999, maxLines: 50, savings: 0 };
  mockState.products = [];
  mockState.admin = {};
  mockState.adminError = null;
  mockState.orderReplies = [];
  mockState.ackCart = null;
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const tieredProduct = {
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

describe('admin produk', () => {
  const adminData = () => ({
    '/products': { items: [tieredProduct, { ...tieredProduct, id: 8, name: 'Saos', unit: 'botol', tiers: [] }], total: 2, page: 1, perPage: 20 },
    '/categories': { items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] },
  });

  test('daftar: satuan dan lencana "Grosir (N jenjang)"', async () => {
    mockState.admin = adminData();
    await renderAt('/admin/products');
    const rows = [...container.querySelectorAll('tbody tr')];
    expect(rows[0].textContent).toContain('/ pak');
    expect(rows[0].textContent).toContain('Grosir (2 jenjang)');
    expect(rows[1].textContent).toContain('/ botol');
    expect(rows[1].textContent).not.toContain('Grosir (');
  });

  test('form: satuan dengan saran, baris jenjang, pratinjau, validasi langsung, kirim unit + tiers', async () => {
    mockState.admin = adminData();
    await renderAt('/admin/products');
    await click(btn('Ubah'));
    const dialog = container.querySelector('[role="dialog"]');
    const unit = dialog.querySelector('#pf-unit');
    expect(unit.value).toBe('pak');
    expect([...dialog.querySelectorAll('#pf-unit-list option')].map((o) => o.value)).toEqual(['pcs', 'pak', 'dus', 'box', 'karton', 'bungkus', 'botol', 'kg', 'gram', 'lusin']);
    expect(dialog.querySelectorAll('[data-testid="tier-row"]')).toHaveLength(2);
    const preview = dialog.querySelector('[data-testid="tier-preview"]');
    expect(preview.textContent).toContain('10+');
    expect(preview.textContent).toContain('Rp 42.000');
    expect(preview.textContent).toContain('Rp 40.500');
    expect(preview.textContent).toContain('Rp 4.500 (10%)');
    expect(dialog.textContent).toContain('2/20 jenjang');

    // Tambah jenjang 100 dengan Rp 41.000 (lebih mahal dari jenjang 50) -> peringatan merah, simpan diblokir.
    await click(btnStarts('+ Tambah jenjang'));
    const rows = dialog.querySelectorAll('[data-testid="tier-row"]');
    expect(rows).toHaveLength(3);
    await act(async () => {
      setInput(rows[2].querySelector('input[aria-label="Jumlah minimal jenjang 3"]'), '100');
      setInput(rows[2].querySelector('input[aria-label="Nilai jenjang 3"]'), '41000');
    });
    // Kolom jenjang "Rp" memakai MoneyInput: tampil berformat ribuan.
    expect(rows[2].querySelector('input[aria-label="Nilai jenjang 3"]').value).toBe('41.000');
    expect(rows[2].textContent).toContain('harus lebih murah dari jenjang min. 50 (Rp 40.500)');
    await click(btn('Simpan'));
    expect(mockState.calls.some((c) => c.method === 'PUT')).toBe(false);
    expect(dialog.textContent).toContain('Perbaiki jenjang harga grosir yang ditandai merah');

    // Ganti ke persen 12,5 -> valid; satuan diubah; simpan mengirim unit + tiers.
    const pctBtn = [...rows[2].querySelectorAll('button')].find((b) => b.textContent === '%');
    await click(pctBtn);
    await act(async () => setInput(rows[2].querySelector('input[aria-label="Nilai jenjang 3"]'), '12,5'));
    expect(rows[2].textContent).not.toContain('harus lebih murah');
    await act(async () => setInput(unit, ' Dus '));
    await click(btn('Simpan'));
    const put = mockState.calls.find((c) => c.method === 'PUT' && c.url === 'admin/products/7');
    expect(put.body.unit).toBe('dus');
    expect(put.body.tiers).toEqual([
      { minQty: 10, type: 'fixed', value: 42000 },
      { minQty: 50, type: 'percent', value: 10 },
      { minQty: 100, type: 'percent', value: 12.5 },
    ]);
  });

  test('form: kesalahan server 422 ditampilkan per jenjang; batas 20 jenjang', async () => {
    mockState.admin = adminData();
    const err = new Error('Harga dasar baru Rp 40.000 membuat jenjang grosir tidak valid; ubah atau hapus jenjang yang bermasalah. Jenjang 1 (min. 10): …');
    err.status = 422;
    err.data = { tierErrors: [{ index: 0, minQty: 10, message: 'Jenjang 1 (min. 10): dari server' }] };
    mockState.adminError = err;
    await renderAt('/admin/products');
    await click(btn('Ubah'));
    const dialog = container.querySelector('[role="dialog"]');
    await click(btn('Simpan'));
    expect(dialog.textContent).toContain('Harga dasar baru Rp 40.000');
    expect(dialog.querySelectorAll('[data-testid="tier-row"]')[0].textContent).toContain('Jenjang 1 (min. 10): dari server');
    // Batas 20.
    for (let i = 0; i < 18; i += 1) {
      // eslint-disable-next-line no-await-in-loop
      await click(btnStarts('+ Tambah jenjang'));
    }
    expect(dialog.querySelectorAll('[data-testid="tier-row"]')).toHaveLength(20);
    expect(btnStarts('+ Tambah jenjang').disabled).toBe(true);
    expect(dialog.textContent).toContain('20/20 jenjang (batas maksimal tercapai)');
  });
});

test('kartu produk: harga "/ satuan" dan lencana Grosir yang membuka daftar jenjang', async () => {
  mockState.products = [
    { id: 7, name: 'Kerupuk Grosir', category: 'kerupuk', price: 45000, description: 'Gurih', image: '', unit: 'pak', tiers: tieredProduct.tiers },
    { id: 8, name: 'Saos', category: 'saos', price: 15000, description: 'Pedas', image: '', unit: 'botol', tiers: [] },
    { id: 9, name: 'Lama', category: 'saos', price: 1000, description: 'API lama', image: '' },
  ];
  await renderAt('/');
  expect(container.textContent).toContain('/ pak');
  expect(container.textContent).toContain('/ botol');
  expect(container.textContent).toContain('/ pcs'); // tanpa field unit (API lama) -> pcs
  const badge = btnStarts('Grosir');
  expect(badge.textContent).toContain('mulai Rp 40.500 / pak');
  expect(badge.getAttribute('aria-expanded')).toBe('false');
  await click(badge);
  const list = container.querySelector('[data-testid="tier-list"]');
  expect(list.textContent).toContain('Beli 10+ pak');
  expect(list.textContent).toContain('Rp 42.000 / pak');
  expect(list.textContent).toContain('Beli 50+ pak');
  expect([...container.querySelectorAll('button')].filter((b) => b.textContent.startsWith('Grosir'))).toHaveLength(1);
});

const line = (over) => ({
  productId: 7,
  name: 'Kerupuk Grosir',
  price: 45000,
  baseUnitPrice: 45000,
  unitPrice: 42000,
  unit: 'pak',
  qty: 12,
  lineTotal: 504000,
  available: true,
  tierMinQty: 10,
  savings: 36000,
  nextTier: { minQty: 50, unitPrice: 40500, moreQty: 38 },
  priceChanged: false,
  previousUnitPrice: null,
  ...over,
});
const cartOf = (items) => ({
  items,
  subtotal: items.reduce((s, i) => s + i.lineTotal, 0),
  itemCount: items.reduce((s, i) => s + i.qty, 0),
  lineCount: items.length,
  hasUnavailable: false,
  hasPriceChanges: items.some((i) => i.priceChanged),
  savings: items.reduce((s, i) => s + i.savings, 0),
  maxQty: 999,
  maxLines: 50,
});

test('keranjang: harga efektif, hemat, petunjuk jenjang berikutnya, penanda perubahan harga + Mengerti', async () => {
  mockState.cart = cartOf([line({ priceChanged: true, previousUnitPrice: 43000 }), line({ productId: 8, name: 'Saos', unit: 'botol', price: 15000, baseUnitPrice: 15000, unitPrice: 15000, qty: 1, lineTotal: 15000, tierMinQty: null, savings: 0, nextTier: { minQty: 6, unitPrice: 14000, moreQty: 5 } })]);
  mockState.ackCart = cartOf([line(), line({ productId: 8, name: 'Saos', unit: 'botol', price: 15000, baseUnitPrice: 15000, unitPrice: 15000, qty: 1, lineTotal: 15000, tierMinQty: null, savings: 0, nextTier: null })]);
  await renderAt('/keranjang', USER);
  const t = container.textContent;
  expect(t).toContain('Rp 42.000 / pak');
  expect(t).toContain('Rp 45.000 / pak'); // harga dasar dicoret
  expect(t).toContain('harga grosir (min. 10)');
  expect(t).toContain('hemat Rp 36.000');
  expect(t).toContain('Tambah 38 lagi untuk harga Rp 40.500 / pak');
  expect(t).toContain('Tambah 5 lagi untuk harga Rp 14.000 / botol');
  expect(container.querySelector('[data-testid="price-change-banner"]').textContent).toContain('Harga berubah untuk 1 produk');
  const marker = container.querySelector('[data-testid="price-changed"]');
  expect(marker.textContent).toContain('Harga berubah: dari Rp 43.000 menjadi Rp 42.000 / pak');
  expect(t).toContain('Hemat harga grosir');
  await click([...marker.querySelectorAll('button')].find((b) => b.textContent === 'Mengerti'));
  expect(mockState.calls.some((c) => c.method === 'post' && c.url.endsWith('/api/cart/ack-prices'))).toBe(true);
  expect(container.querySelector('[data-testid="price-changed"]')).toBeNull();
  expect(container.querySelector('[data-testid="price-change-banner"]')).toBeNull();
});

test('checkout: kirim expectedTotal; 409 price_changed -> keranjang terkini + konfirmasi ulang dengan total baru', async () => {
  mockState.cart = cartOf([line({ nextTier: null })]); // 504.000 di layar
  const fresh = cartOf([line({ unitPrice: 43000, lineTotal: 516000, savings: 24000, priceChanged: true, previousUnitPrice: 42000, nextTier: null })]);
  mockState.orderReplies = [{ status: 409, data: { error: 'price_changed', message: 'Harga berubah', cart: fresh } }, { status: 201 }];
  mockState.ackCart = { ...fresh, items: fresh.items.map((i) => ({ ...i, priceChanged: false, previousUnitPrice: null })), hasPriceChanges: false };
  await renderAt('/checkout', USER);
  expect(container.textContent).toContain('Rp 42.000 / pak');
  expect(container.textContent).toContain('harga grosir (min. 10)');
  await fillCheckout(container);
  await click(btn('Buat pesanan'));
  const posts = () => mockState.calls.filter((c) => c.method === 'post' && c.url.endsWith('/api/orders'));
  expect(posts()).toHaveLength(1);
  expect(posts()[0].body.expectedTotal).toBe(504000);
  // 409: tidak pindah halaman; pesan jelas + keranjang terkini + tombol konfirmasi.
  expect(window.location.pathname).toBe('/checkout');
  const warn = container.querySelector('[data-testid="checkout-price-changed"]');
  expect(warn.textContent).toContain('Harga berubah');
  expect(warn.textContent).toContain('Rp 516.000');
  expect(container.textContent).toContain('Harga berubah dari Rp 42.000');
  expect(btn('Buat pesanan')).toBeUndefined();
  await click(btn('Konfirmasi & buat pesanan'));
  await flush();
  expect(mockState.calls.some((c) => c.method === 'post' && c.url.endsWith('/api/cart/ack-prices'))).toBe(true);
  expect(posts()).toHaveLength(2);
  expect(posts()[1].body.expectedTotal).toBe(516000);
  expect(posts()[1].body.idempotencyKey).toBe(posts()[0].body.idempotencyKey);
  expect(window.location.pathname).toBe('/pesanan/MS-261003-0001');
});
