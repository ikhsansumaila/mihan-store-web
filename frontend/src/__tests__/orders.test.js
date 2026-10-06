// Tes render ringan (jsdom) fitur pesanan: keranjang, checkout, pesanan saya, admin Pesanan & Pengaturan.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { fillCheckout } from '../testUtils/regionFixtures';
import { clearRegionCache } from '../shop/regionsApi';

const mockState = {
  calls: [],
  cart: null,
  orders: { items: [], total: 0, page: 1, perPage: 10 },
  order: null,
  storeInfo: null,
  products: [],
  admin: {},
  postDelay: 0,
  regionCalls: [],
  regionOverride: {},
  orderReply: null,
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
    if (method === 'post' && url.endsWith('/api/orders')) {
      if (mockState.orderReply) {
        const err = new Error('gagal');
        err.response = mockState.orderReply;
        return Promise.reject(err);
      }
      return { data: { orderNo: 'MS-261002-0001' } };
    }
    if (url.includes('/api/orders?')) return { data: mockState.orders };
    if (url.includes('/api/orders/')) return { data: mockState.order };
    return { data: {} };
  };
  const later = (v) => new Promise((r, j) => setTimeout(() => (v instanceof Promise ? v.then(r, j) : r(v)), mockState.postDelay));
  return {
    __esModule: true,
    default: {
      get: (url) => require('../testUtils/regionFixtures').mockRegionGet(url, mockState.regionCalls, mockState.regionOverride) || Promise.resolve(respond('get', url)),
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
  mockState.regionCalls = [];
  mockState.regionOverride = {};
  mockState.orderReply = null;
  clearRegionCache();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const USER = { name: 'Budi', role: 'customer', phone: '+6281234567890' };
const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);

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

test('/checkout: hanya data penerima + idempotencyKey + expectedTotal, klik ganda hanya satu permintaan, lalu halaman sukses', async () => {
  mockState.cart = mockCart([{ productId: 1, name: 'Kerupuk Udang', price: 45000, qty: 2, lineTotal: 90000, available: true }]);
  mockState.order = sampleOrder;
  mockState.storeInfo = { storeWhatsapp: '+6281299998888', bankName: 'BCA', bankAccountNumber: '123', bankAccountHolder: 'Toko', paymentConfigured: true };
  await renderAt('/checkout', USER);
  expect(container.querySelector('h1').textContent).toBe('Checkout');
  // Nama penerima kosong pada awal (tidak diisi dari nama akun); telepon boleh dari akun.
  expect(container.querySelector('#co-name').value).toBe('');
  expect(container.querySelector('#co-phone').value).toBe('+6281234567890');
  await fillCheckout(container, { name: 'Budi' });
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
  expect(Object.keys(body).sort()).toEqual([
    'address',
    'districtCode',
    'expectedTotal',
    'idempotencyKey',
    'note',
    'postalCode',
    'provinceCode',
    'recipientName',
    'recipientPhone',
    'regencyCode',
    'villageCode',
  ]);
  expect([body.provinceCode, body.regencyCode, body.districtCode, body.villageCode]).toEqual(['36', '36.71', '36.71.01', '36.71.01.1001']);
  expect(body.postalCode).toBe('15111');
  expect(body.expectedTotal).toBe(90000); // total yang ditampilkan (harga grosir), diperiksa server
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
  expect(text).toContain('- Kerupuk Finna Udang x2\n- Kerupuk Finna Bawang x1');
  expect(text).not.toContain('Rp');
  expect(text).not.toContain('Melati');
  // Nama di teks WhatsApp = nama penerima pesanan, bukan nama akun login (USER.name = 'Budi').
  expect(text).toContain('Nama: Budi Penerima');
  expect(text).not.toContain('Buka di admin');
  expect(text).not.toContain('/admin/');
  expect(text.endsWith(`\n\nMohon infokan terkait ongkir dan total yang harus saya bayar, Terima Kasih\n\n${window.location.origin}/pesanan/MS-261002-0001`)).toBe(true);
  expect(text).not.toMatch(/Nama: Budi$/m);
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
  const tabs = [...container.querySelectorAll('#admin-sidebar nav a')].map((a) => a.textContent.trim());
  expect(tabs).toEqual(['Dashboard', 'Produk', 'Kategori', 'Pesanan', 'Pelanggan', 'Invoice', 'Pricelist', 'Pengaturan Toko', 'Data Wilayah', 'Log Aktivitas']);
  expect(container.querySelector('h1').textContent).toBe('Pesanan');
  expect(container.textContent).toContain('MS-261002-0005');
  expect(container.textContent).toContain('Menunggu pembayaran');
  expect(container.textContent).toContain('Selesai');
  expect(container.querySelector('a[href="/admin/orders/5"]')).not.toBeNull();
});

const ADMIN_LIST = {
  items: [
    { id: 5, orderNo: 'MS-261002-0005', status: 'paid', total: 152000, itemCount: 3, recipientName: 'Budi', city: 'Tangerang', customerName: 'Budi', createdAt: '2026-10-02T03:00:00Z' },
  ],
  total: 45,
  page: 1,
  perPage: 20,
};
const tabEls = () => [...container.querySelectorAll('[role="tablist"] [role="tab"]')];
const tabBy = (status) => container.querySelector(`[role="tab"][data-status="${status}"]`);
const listCalls = () => mockState.calls.filter((c) => c.url.startsWith('admin/orders?')).map((c) => c.url);
const typeInto = async (el, value) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
};

test('admin Pesanan: tab status menggantikan dropdown, default Semua, tab bisa di-scroll horizontal', async () => {
  mockState.admin = { '/orders': ADMIN_LIST };
  await renderAt('/admin/orders');
  expect(container.querySelector('select[aria-label="Filter status"]')).toBeNull();
  expect(tabEls().map((t) => t.textContent)).toEqual(['Semua', 'Menunggu pembayaran', 'Dibayar', 'Selesai', 'Dibatalkan']);
  expect(tabBy('all').getAttribute('aria-selected')).toBe('true');
  expect(listCalls()).toEqual(['admin/orders?page=1&per_page=20']);
  // Baris tab scroll sendiri (bukan halaman), tombol tidak menyusut/terpotong.
  const scroller = container.querySelector('[data-testid="order-status-tabs"]');
  expect(scroller.className).toContain('overflow-x-auto');
  expect(container.querySelector('[role="tablist"]').className).toContain('w-max');
  tabEls().forEach((t) => expect(t.className).toMatch(/\bshrink-0\b.*\bwhitespace-nowrap\b/));
  expect(container.querySelector('#order-list-panel[role="tabpanel"]')).not.toBeNull();
  // Tab menempel di tepi atas kartu daftar, langsung di atas tabel (judul kolom); filter tetap di atas kartu.
  const card = container.querySelector('[data-testid="order-list-card"]');
  expect(card.firstElementChild).toBe(scroller);
  expect(scroller.nextElementSibling.id).toBe('order-list-panel');
  expect(scroller.nextElementSibling.firstElementChild.tagName).toBe('TABLE');
  expect(scroller.className).not.toMatch(/\bm[btxy]?-\d/);
  const form = container.querySelector('form');
  expect(form.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(tabBy('all').className).toContain('bg-gray-50');
  expect(container.querySelector('#order-list-panel thead').className).toContain('bg-gray-50');
});

test('admin Pesanan: klik tab menyimpan ?status di URL (replace), memuat ulang dari halaman 1, pencarian tetap berlaku', async () => {
  mockState.admin = { '/orders': ADMIN_LIST };
  await renderAt('/admin/orders');
  // Cari + tanggal, lalu ke halaman 2.
  await typeInto(container.querySelector('form input[maxlength="100"]'), '  Budi ');
  await typeInto(container.querySelector('form input[type="date"]'), '2026-10-01');
  await act(async () => btn('Terapkan').click());
  await flush();
  await act(async () => btn('Berikutnya ›').click());
  await flush();
  expect(listCalls().at(-1)).toBe('admin/orders?from=2026-10-01&q=Budi&page=2&per_page=20');

  const historyLength = window.history.length;
  await act(async () => tabBy('paid').click());
  await flush();
  expect(window.location.pathname).toBe('/admin/orders');
  expect(window.location.search).toBe('?status=paid');
  expect(window.history.length).toBe(historyLength);
  expect(tabBy('paid').getAttribute('aria-selected')).toBe('true');
  expect(listCalls().at(-1)).toBe('admin/orders?status=paid&from=2026-10-01&q=Budi&page=1&per_page=20');

  // Reset mengosongkan pencarian/tanggal, tab tetap.
  await act(async () => btn('Reset').click());
  await flush();
  expect(listCalls().at(-1)).toBe('admin/orders?status=paid&page=1&per_page=20');
  expect(window.location.search).toBe('?status=paid');

  // Kembali ke Semua: ?status dihapus.
  await act(async () => tabBy('all').click());
  await flush();
  expect(window.location.search).toBe('');
  expect(listCalls().at(-1)).toBe('admin/orders?page=1&per_page=20');
});

test('admin Pesanan: refresh di ?status=cancelled tetap di tab itu; panah kanan/kiri berpindah tab', async () => {
  mockState.admin = { '/orders': ADMIN_LIST };
  await renderAt('/admin/orders?status=cancelled');
  expect(tabBy('cancelled').getAttribute('aria-selected')).toBe('true');
  expect(listCalls()).toEqual(['admin/orders?status=cancelled&page=1&per_page=20']);
  await act(async () => tabBy('cancelled').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })));
  await flush();
  expect(window.location.search).toBe('');
  expect(document.activeElement).toBe(tabBy('all'));
  await act(async () => tabBy('all').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })));
  await flush();
  expect(window.location.search).toBe('?status=cancelled');
});

test('admin Pesanan: tautan "Semua pesanan" di detail kembali ke tab asal', async () => {
  mockState.admin = { '/orders/5': adminOrder, '/orders': ADMIN_LIST, '/settings': { settings: {} } };
  await renderAt('/admin/orders?status=paid');
  await act(async () => container.querySelector('a[href="/admin/orders/5"]').click());
  await flush();
  expect(window.location.pathname).toBe('/admin/orders/5');
  const back = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Semua pesanan'));
  expect(back.getAttribute('href')).toBe('/admin/orders?status=paid');
  await act(async () => back.click());
  await flush();
  expect(window.location.search).toBe('?status=paid');
  expect(tabBy('paid').getAttribute('aria-selected')).toBe('true');
});

test('admin detail dibuka langsung: tautan "Semua pesanan" ke daftar tanpa status', async () => {
  mockState.admin = { '/orders/5': adminOrder, '/settings': { settings: {} } };
  await renderAt('/admin/orders/5');
  const back = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Semua pesanan'));
  expect(back.getAttribute('href')).toBe('/admin/orders');
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

test('admin /admin/orders/<nomor pesanan>: dicocokkan persis lalu URL diganti ke id numerik', async () => {
  mockState.admin = {
    '/orders?': {
      // Pencarian backend memakai LIKE: hasil bisa memuat nomor lain yang mirip; harus dipilih yang persis.
      items: [
        { id: 9, orderNo: 'MS-261002-00010', status: 'paid', total: 1, itemCount: 1, recipientName: 'Lain', city: 'X', customerName: 'Lain', createdAt: '2026-10-02T03:00:00Z' },
        { id: 5, orderNo: 'MS-261002-0001', status: 'pending_payment', total: 152000, itemCount: 3, recipientName: 'Budi', city: 'Tangerang', customerName: 'Budi', createdAt: '2026-10-02T03:00:00Z' },
      ],
      total: 2,
      page: 1,
      perPage: 100,
    },
    '/orders/5': adminOrder,
    '/settings': { settings: {} },
  };
  const historyLengthAtStart = window.history.length;
  await renderAt('/admin/orders/ms-261002-0001');
  await flush();
  const search = mockState.calls.find((c) => c.url.startsWith('admin/orders?'));
  expect(search.url).toBe('admin/orders?q=ms-261002-0001&per_page=100');
  expect(window.location.pathname).toBe('/admin/orders/5');
  expect(mockState.calls.some((c) => c.url === 'admin/orders/5')).toBe(true);
  expect(mockState.calls.some((c) => c.url === 'admin/orders/9')).toBe(false);
  expect(container.querySelector('h1').textContent).toContain('MS-261002-0001');
  // replace, bukan push: kembali (history) tidak mengarah ke URL nomor pesanan.
  expect(window.history.length).toBe(historyLengthAtStart);
});

test('admin /admin/orders/<nomor> tidak ditemukan (hanya ada nomor mirip): pesan + tautan ke daftar', async () => {
  mockState.admin = {
    '/orders?': { items: [{ id: 9, orderNo: 'MS-261002-00010', status: 'paid', total: 1, itemCount: 1, createdAt: '2026-10-02T03:00:00Z' }], total: 1, page: 1, perPage: 100 },
  };
  await renderAt('/admin/orders/MS-261002-0001');
  await flush();
  const box = container.querySelector('[data-testid="order-not-found"]');
  expect(box.textContent).toContain('Pesanan MS-261002-0001 tidak ditemukan');
  expect(box.querySelector('a').getAttribute('href')).toBe('/admin/orders');
  expect(window.location.pathname).toBe('/admin/orders/MS-261002-0001');
  expect(mockState.calls.some((c) => c.url === 'admin/orders/9')).toBe(false);
});

test('admin /admin/orders/<id numerik> tetap langsung memuat detail tanpa pencarian', async () => {
  mockState.admin = { '/orders/5': adminOrder, '/settings': { settings: {} } };
  await renderAt('/admin/orders/5');
  expect(container.querySelector('h1').textContent).toContain('MS-261002-0001');
  expect(mockState.calls.some((c) => c.url.startsWith('admin/orders?'))).toBe(false);
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

const REGION_RECIPIENT = {
  name: 'Siti Penerima',
  phone: '+6281311112222',
  address: 'Jl. Mawar No. 5\nRT 01/RW 02',
  city: 'Kota Administrasi Jakarta Selatan',
  postalCode: '12440',
  region: {
    province: { code: '31', name: 'DKI Jakarta' },
    regency: { code: '31.74', name: 'Kota Administrasi Jakarta Selatan' },
    district: { code: '31.74.06', name: 'Cilandak' },
    village: { code: '31.74.06.1004', name: 'Lebak Bulus' },
  },
};
const REGION_FULL = 'Jl. Mawar No. 5, RT 01/RW 02, Lebak Bulus, Kec. Cilandak, Kota Administrasi Jakarta Selatan, DKI Jakarta 12440';

test('detail pesanan pelanggan & admin: alamat tersusun (baru) dan alamat + kota (lama)', async () => {
  mockState.order = { ...sampleOrder, recipient: REGION_RECIPIENT };
  mockState.storeInfo = { storeWhatsapp: null, paymentConfigured: false };
  await renderAt('/pesanan/MS-261002-0001', USER);
  expect(container.querySelector('[data-testid="order-full-address"]').textContent).toBe(REGION_FULL);
  act(() => root.unmount());
  container.remove();

  mockState.order = sampleOrder; // pesanan lama
  await renderAt('/pesanan/MS-261002-0001', USER);
  expect(container.querySelector('[data-testid="order-full-address"]').textContent).toBe('Jl. Melati 9, Tangerang 15111');
  act(() => root.unmount());
  container.remove();

  mockState.admin = { '/orders/5': { ...adminOrder, recipient: REGION_RECIPIENT }, '/settings': { settings: {} } };
  await renderAt('/admin/orders/5');
  expect(container.querySelector('[data-testid="admin-full-address"]').textContent).toBe(REGION_FULL);
  const wa = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Kirim ringkasan ke WhatsApp'));
  const text = decodeURIComponent(wa.getAttribute('href').split('text=')[1]);
  expect(text).toContain(`Alamat pengiriman:\n${REGION_FULL}`);
});
