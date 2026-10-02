// Tes render ringan (jsdom) fitur pesanan: keranjang, checkout, pesanan saya, admin Pesanan & Pengaturan.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

const mockState = {
  calls: [],
  cart: null,
  orders: { items: [], total: 0, page: 1, perPage: 10 },
  order: null,
  storeInfo: null,
  products: [],
  admin: {},
  postDelay: 0,
};

const mockCart = (items) => {
  const avail = items.filter((i) => i.available);
  return {
    items,
    subtotal: avail.reduce((s, i) => s + i.lineTotal, 0),
    itemCount: avail.reduce((s, i) => s + i.qty, 0),
    lineCount: items.length,
    hasUnavailable: items.some((i) => !i.available),
    maxQty: 999,
    maxLines: 50,
  };
};

jest.mock('axios', () => {
  const respond = (method, url, body) => {
    mockState.calls.push({ method, url, body });
    if (url.endsWith('/auth/me')) return { data: { user: { name: 'Budi', role: 'customer', phone: '+6281234567890' } } };
    if (url.endsWith('/api/products')) return { data: mockState.products };
    if (url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart') || url.includes('/api/cart/items')) return { data: mockState.cart };
    if (url.endsWith('/api/store-info')) return { data: mockState.storeInfo };
    if (method === 'post' && url.endsWith('/api/orders')) return { data: { orderNo: 'MS-261002-0001' } };
    if (url.includes('/api/orders?')) return { data: mockState.orders };
    if (url.includes('/api/orders/')) return { data: mockState.order };
    return { data: {} };
  };
  const later = (v) => new Promise((r) => setTimeout(() => r(v), mockState.postDelay));
  return {
    __esModule: true,
    default: {
      get: (url) => Promise.resolve(respond('get', url)),
      post: (url, body) => later(respond('post', url, body)),
      put: (url, body) => Promise.resolve(respond('put', url, body)),
      delete: (url) => Promise.resolve(respond('delete', url)),
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

beforeEach(() => {
  mockState.calls = [];
  mockState.cart = mockCart([]);
  mockState.postDelay = 0;
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const USER = { name: 'Budi', role: 'customer', phone: '+6281234567890' };
const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const setInput = (el, value) => {
  const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
};

const sampleOrder = {
  orderNo: 'MS-261002-0001',
  status: 'pending_payment',
  subtotal: 145000,
  discount: 5000,
  discountNote: 'Promo',
  shippingFee: 12000,
  total: 152000,
  recipient: { name: 'Budi Penerima', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang', postalCode: '15111' },
  items: [
    { productId: 1, name: 'Kerupuk Finna Udang', unitPrice: 45000, qty: 2, lineTotal: 90000 },
    { productId: 2, name: 'Kerupuk Finna Bawang', unitPrice: 55000, qty: 1, lineTotal: 55000 },
  ],
  history: [{ from: null, to: 'pending_payment', toLabel: 'Menunggu pembayaran', actor: 'Anda', createdAt: '2026-10-02T03:00:00Z' }],
  canCancel: true,
  itemCount: 3,
  createdAt: '2026-10-02T03:00:00Z',
};

test('navbar saat login: Pesanan Saya + ikon keranjang dengan lencana jumlah item', async () => {
  mockState.cart = mockCart([{ productId: 1, name: 'A', price: 1000, qty: 3, lineTotal: 3000, available: true }]);
  await renderAt('/', USER);
  const nav = container.querySelector('nav');
  expect([...nav.querySelectorAll('a')].map((a) => a.getAttribute('href'))).toEqual(expect.arrayContaining(['/pesanan', '/keranjang']));
  expect(nav.querySelector('a[href="/keranjang"]').textContent).toContain('3');
});

test('belum login: tidak ada ikon keranjang; tombol Tambah ke keranjang mengarah ke login lalu kembali ke toko', async () => {
  mockState.products = [{ id: 1, name: 'Kerupuk', category: 'kerupuk', price: 45000, description: 'Gurih', image: '' }];
  await renderAt('/');
  expect(container.querySelector('a[href="/keranjang"]')).toBeNull();
  expect(container.querySelector('a[href="/pesanan"]')).toBeNull();
  await act(async () => btn('Tambah ke keranjang').click());
  expect(window.location.pathname).toBe('/login');
  expect(sessionStorage.getItem('returnTo')).toBe('/');
  expect(mockState.calls.some((c) => c.url.includes('/cart/items'))).toBe(false);
});

test('login: Tambah ke keranjang memanggil API (tanpa harga) dan memberi umpan balik', async () => {
  mockState.products = [{ id: 7, name: 'Saos', category: 'saos', price: 15000, description: 'Pedas', image: '' }];
  await renderAt('/', USER);
  mockState.cart = mockCart([{ productId: 7, name: 'Saos', price: 15000, qty: 1, lineTotal: 15000, available: true }]);
  await act(async () => btn('Tambah ke keranjang').click());
  await flush();
  const post = mockState.calls.find((c) => c.method === 'post' && c.url.endsWith('/cart/items'));
  expect(post.body).toEqual({ productId: 7, qty: 1 });
  expect(container.textContent).toContain('Ditambahkan ✓');
  expect(container.querySelector('a[href="/keranjang"]').textContent).toContain('1');
});

test('/keranjang tanpa login dialihkan ke /login dan kembali ke /keranjang', async () => {
  await renderAt('/keranjang');
  expect(window.location.pathname).toBe('/login');
  expect(sessionStorage.getItem('returnTo')).toBe('/keranjang');
});

test('/keranjang: item, subtotal, produk nonaktif ditandai, checkout dinonaktifkan', async () => {
  mockState.cart = mockCart([
    { productId: 1, name: 'Kerupuk Udang', price: 45000, qty: 2, lineTotal: 90000, available: true },
    { productId: 2, name: 'Produk Lama', price: 10000, qty: 1, lineTotal: 10000, available: false },
  ]);
  await renderAt('/keranjang', USER);
  expect(container.querySelector('h1').textContent).toBe('Keranjang');
  expect(container.textContent).toContain('Kerupuk Udang');
  expect(container.textContent).toContain('Tidak tersedia');
  expect(container.textContent).toContain('Rp 90.000');
  expect(btn('Lanjut ke checkout').disabled).toBe(true);
  // Ubah qty -> PUT dengan qty baru.
  await act(async () => container.querySelector('button[aria-label="Tambah Kerupuk Udang"]').click());
  const put = mockState.calls.find((c) => c.method === 'put');
  expect(put.body).toEqual({ productId: 1, qty: 3 });
});

test('/checkout: hanya data penerima + idempotencyKey, klik ganda hanya satu permintaan, lalu halaman sukses', async () => {
  mockState.cart = mockCart([{ productId: 1, name: 'Kerupuk Udang', price: 45000, qty: 2, lineTotal: 90000, available: true }]);
  mockState.order = sampleOrder;
  mockState.storeInfo = { storeWhatsapp: '+6281299998888', bankName: 'BCA', bankAccountNumber: '123', bankAccountHolder: 'Toko', paymentConfigured: true };
  await renderAt('/checkout', USER);
  expect(container.querySelector('h1').textContent).toBe('Checkout');
  const [addr] = container.querySelectorAll('textarea');
  const inputs = [...container.querySelectorAll('form input')];
  await act(async () => {
    setInput(addr, 'Jl. Melati No. 9');
    setInput(inputs[2], 'Tangerang');
  });
  mockState.postDelay = 20;
  const submit = btn('Buat pesanan');
  await act(async () => {
    submit.click();
    submit.click();
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 40));
  });
  await flush();
  const posts = mockState.calls.filter((c) => c.method === 'post' && c.url.endsWith('/api/orders'));
  expect(posts).toHaveLength(1);
  const body = posts[0].body;
  expect(Object.keys(body).sort()).toEqual(['address', 'city', 'idempotencyKey', 'note', 'postalCode', 'recipientName', 'recipientPhone']);
  expect(body.recipientName).toBe('Budi');
  expect(body.idempotencyKey).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(window.location.pathname).toBe('/pesanan/MS-261002-0001');
  expect(container.textContent).toContain('Pesanan berhasil dibuat');
  expect(container.textContent).toContain('Transfer ke rekening berikut');
});

test('/pesanan menampilkan daftar pesanan', async () => {
  mockState.orders = {
    items: [{ orderNo: 'MS-261002-0001', status: 'paid', total: 152000, itemCount: 3, firstItem: 'Kerupuk', createdAt: '2026-10-02T03:00:00Z' }],
    total: 1,
    page: 1,
    perPage: 10,
  };
  await renderAt('/pesanan', USER);
  expect(container.querySelector('h1').textContent).toBe('Pesanan Saya');
  expect(container.textContent).toContain('MS-261002-0001');
  expect(container.textContent).toContain('Dibayar');
  expect(container.textContent).toContain('Rp 152.000');
});

test('/pesanan/:no: tombol Konfirmasi via WhatsApp ke nomor toko, tombol batal saat menunggu pembayaran', async () => {
  mockState.order = sampleOrder;
  mockState.storeInfo = { storeWhatsapp: '+6281299998888', paymentConfigured: false };
  await renderAt('/pesanan/MS-261002-0001', USER);
  const wa = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Konfirmasi via WhatsApp'));
  expect(wa.getAttribute('href')).toMatch(/^https:\/\/wa\.me\/6281299998888\?text=/);
  const text = decodeURIComponent(wa.getAttribute('href').split('text=')[1]);
  expect(text).toContain('MS-261002-0001');
  expect(text).toContain('Rp 152.000');
  expect(text).not.toContain('Melati');
  expect(container.textContent).toContain('Info rekening toko belum diatur');
  expect(btn('Batalkan pesanan')).toBeTruthy();
  expect(container.textContent).toContain('-Rp 5.000');
});

test('/pesanan/:no: tombol WhatsApp disembunyikan bila nomor toko belum diisi', async () => {
  mockState.order = { ...sampleOrder, status: 'completed', canCancel: false };
  mockState.storeInfo = { storeWhatsapp: null, paymentConfigured: false };
  await renderAt('/pesanan/MS-261002-0001', USER);
  expect(container.textContent).not.toContain('Konfirmasi via WhatsApp');
  expect(btn('Batalkan pesanan')).toBeUndefined();
});

const adminOrder = {
  ...sampleOrder,
  id: 5,
  status: 'pending_payment',
  pricingLocked: false,
  allowedNext: ['paid', 'cancelled'],
  customer: { name: 'Budi', username: 'budi', email: 'budi@example.com' },
  history: [{ from: null, to: 'pending_payment', toLabel: 'Menunggu pembayaran', actor: 'pelanggan:budi', createdAt: '2026-10-02T03:00:00Z' }],
  updatedAt: '2026-10-02T03:00:00Z',
};

test('admin Pesanan: menu baru, daftar dengan lencana status', async () => {
  mockState.admin = {
    '/orders': {
      items: [
        { id: 5, orderNo: 'MS-261002-0005', status: 'pending_payment', total: 152000, itemCount: 3, recipientName: 'Budi', city: 'Tangerang', customerName: 'Budi', createdAt: '2026-10-02T03:00:00Z' },
        { id: 6, orderNo: 'MS-261002-0006', status: 'completed', total: 10000, itemCount: 1, recipientName: 'Ani', city: 'Bogor', customerName: 'Ani', createdAt: '2026-10-02T03:00:00Z' },
      ],
      total: 2,
      page: 1,
      perPage: 20,
    },
  };
  await renderAt('/admin/orders');
  const tabs = [...container.querySelectorAll('main nav a')].map((a) => a.textContent.trim());
  expect(tabs).toEqual(['Ringkasan', 'Pesanan', 'Produk', 'Kategori', 'Invoice', 'Pengaturan Toko', 'Log aktivitas']);
  expect(container.querySelector('h1').textContent).toBe('Pesanan');
  expect(container.textContent).toContain('MS-261002-0005');
  expect(container.textContent).toContain('Menunggu pembayaran');
  expect(container.textContent).toContain('Selesai');
  expect(container.querySelector('a[href="/admin/orders/5"]')).not.toBeNull();
});

test('admin detail pesanan: diskon/ongkir, tombol status, invoice, WhatsApp pelanggan', async () => {
  mockState.admin = { '/orders/5': adminOrder, '/settings': { settings: { bank_name: 'BCA', bank_account_number: '123', bank_account_holder: 'Toko' } } };
  await renderAt('/admin/orders/5');
  expect(container.querySelector('h1').textContent).toContain('MS-261002-0001');
  expect(btn('Tandai Dibayar')).toBeTruthy();
  expect(btn('Batalkan')).toBeTruthy();
  expect(btn('Tandai Selesai')).toBeUndefined();
  expect(btn('Simpan diskon & ongkir')).toBeTruthy();
  expect(btn('Cetak invoice')).toBeTruthy();
  const wa = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Kirim ringkasan ke WhatsApp pelanggan'));
  expect(wa.getAttribute('href')).toMatch(/^https:\/\/wa\.me\/6281311112222\?text=/);
  // Tandai dibayar mengirim status lama (from) untuk deteksi konflik.
  await act(async () => btn('Tandai Dibayar').click());
  const modalBtn = [...container.querySelectorAll('[role="dialog"] button')].find((b) => b.textContent === 'Tandai dibayar');
  mockState.admin['/orders/5/status'] = { ...adminOrder, status: 'paid', allowedNext: ['completed', 'cancelled'], pricingLocked: true };
  await act(async () => modalBtn.click());
  await flush();
  const patch = mockState.calls.find((c) => c.method === 'PATCH' && c.url === 'admin/orders/5/status');
  expect(patch.body).toMatchObject({ from: 'pending_payment', to: 'paid' });
  expect(container.textContent).toContain('Terkunci');
});

test('admin Pengaturan Toko: lima kunci dan simpan', async () => {
  mockState.admin = { '/settings': { settings: { store_whatsapp: '', bank_name: '', bank_account_number: '', bank_account_holder: '', payment_note: '' } } };
  await renderAt('/admin/settings');
  expect(container.querySelector('h1').textContent).toBe('Pengaturan Toko');
  expect(container.querySelectorAll('form input, form textarea')).toHaveLength(5);
  expect(container.textContent).toContain('Belum diisi');
  await act(async () => btn('Simpan pengaturan').click());
  await flush();
  const put = mockState.calls.find((c) => c.method === 'PUT' && c.url === 'admin/settings');
  expect(Object.keys(put.body).sort()).toEqual(['bank_account_holder', 'bank_account_number', 'bank_name', 'payment_note', 'store_whatsapp']);
});

test('admin Ringkasan menampilkan hitungan pesanan', async () => {
  mockState.admin = { '/summary': { products: { total: 20, active: 20, inactive: 0 }, categories: 6, orders: { pendingPayment: 4, paid: 2, last7Days: 9 }, recentActivity: [] } };
  await renderAt('/admin');
  expect(container.textContent).toContain('Menunggu pembayaran');
  expect(container.textContent).toContain('Pesanan 7 hari terakhir');
  expect(container.textContent).toContain('9');
});
