// Tes render layout admin bergaya cPanel (sidebar, topbar, laci, pencarian menu, dashboard).
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa (bukan jest.fn).
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';

const mockState = { calls: [] };

jest.mock('axios', () => ({
  __esModule: true,
  default: {
    get: () => Promise.resolve({ data: [] }),
    post: () => Promise.resolve({ data: {} }),
  },
}));
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });
jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path) => {
      mockState.calls.push(path);
      if (path === '/me') return Promise.resolve({ user: { email: 'pemilik@example.com' } });
      if (path === '/summary')
        return Promise.resolve({
          products: { total: 20, active: 18, inactive: 2 },
          categories: 6,
          orders: { pendingPayment: 4, paid: 2, last7Days: 9 },
          recentActivity: [{ id: 1, action: 'product.update', summary: 'Ubah harga', userEmail: 'pemilik@example.com', createdAt: '2026-10-02T03:00:00Z' }],
        });
      if (path === '/categories') return Promise.resolve({ items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] });
      if (path.startsWith('/products')) return Promise.resolve({ items: [], total: 0, perPage: 20 });
      if (path === '/orders/5')
        return Promise.resolve({
          id: 5,
          orderNo: 'MS-261002-0005',
          status: 'pending_payment',
          subtotal: 10000,
          discount: 0,
          shippingFee: 0,
          total: 10000,
          pricingLocked: false,
          allowedNext: ['paid', 'cancelled'],
          recipient: { name: 'Budi', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang', postalCode: '15111' },
          items: [{ productId: 1, name: 'Kerupuk', unitPrice: 10000, qty: 1, lineTotal: 10000 }],
          customer: { name: 'Budi', username: 'budi', email: 'budi@example.com' },
          history: [],
          createdAt: '2026-10-02T03:00:00Z',
          updatedAt: '2026-10-02T03:00:00Z',
        });
      if (path.startsWith('/orders')) return Promise.resolve({ items: [], total: 0, perPage: 20 });
      return Promise.resolve({});
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

beforeEach(() => {
  mockState.calls = [];
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const sidebar = () => container.querySelector('#admin-sidebar');
const menuLinks = () => [...sidebar().querySelectorAll('nav a')];
const linkByLabel = (label) => menuLinks().find((a) => a.textContent.trim().startsWith(label));
const hamburger = () => container.querySelector('button[aria-label="Buka menu"]');
const searchInput = () => sidebar().querySelector('input[type="search"]');
const typeInto = async (el, value) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
const key = async (target, k) => {
  await act(async () => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }));
  });
};

test('sidebar: nama panel, empat grup, semua item, lencana pesanan menunggu, email, Kembali ke toko, versi', async () => {
  await renderAt('/admin');
  const sb = sidebar();
  expect(sb.textContent).toContain('Mihan Store Admin');
  expect(sb.querySelector('img[src="/mihan-store-logo.png"]')).not.toBeNull();
  const groups = [...sb.querySelectorAll('[data-group]')].map((g) => g.getAttribute('data-group'));
  expect(groups).toEqual(['Utama', 'Katalog', 'Penjualan', 'Sistem']);
  expect(menuLinks().map((a) => [a.textContent.trim(), a.getAttribute('href')])).toEqual([
    ['Dashboard', '/admin'],
    ['Produk', '/admin/products'],
    ['Kategori', '/admin/categories'],
    ['Pesanan4', '/admin/orders'],
    ['Invoice', '/admin/invoice'],
    ['Pengaturan Toko', '/admin/settings'],
    ['Log Aktivitas', '/admin/activity'],
  ]);
  expect(sb.querySelector('[aria-label="4 menunggu pembayaran"]')).not.toBeNull();
  expect(sb.textContent).toContain('Masuk sebagai pemilik@example.com');
  const back = [...sb.querySelectorAll('a')].find((a) => a.textContent.trim() === 'Kembali ke toko');
  expect(back.getAttribute('href')).toBe('/');
  expect(sb.textContent).toMatch(/v\d+\.\d+\.\d+/);
  // Navbar & footer toko tidak tampil di area admin.
  expect(container.textContent).not.toContain('Kebijakan Privasi');
  expect(container.querySelector('svg use')).toBeNull(); // ikon inline, tanpa sprite/CDN
});

test.each([
  ['/admin', 'Dashboard', ['Admin', 'Dashboard']],
  ['/admin/products', 'Produk', ['Admin', 'Katalog', 'Produk']],
  ['/admin/categories', 'Kategori', ['Admin', 'Katalog', 'Kategori']],
  ['/admin/orders', 'Pesanan', ['Admin', 'Penjualan', 'Pesanan']],
  ['/admin/orders/5', 'Pesanan', ['Admin', 'Penjualan', 'Pesanan', 'Detail']],
  ['/admin/settings', 'Pengaturan Toko', ['Admin', 'Sistem', 'Pengaturan Toko']],
  ['/admin/activity', 'Log Aktivitas', ['Admin', 'Sistem', 'Log Aktivitas']],
])('rute %s: menu aktif %s dan breadcrumb sesuai', async (path, label, crumbs) => {
  await renderAt(path);
  const active = menuLinks().filter((a) => a.getAttribute('aria-current') === 'page');
  expect(active).toHaveLength(1);
  expect(active[0].textContent.trim().startsWith(label)).toBe(true);
  const bc = [...container.querySelectorAll('nav[aria-label="Breadcrumb"] li')].map((li) => li.textContent.replace('/', '').trim());
  expect(bc).toEqual(crumbs);
});

test('pencarian menu memfilter item; Enter membuka hasil pertama', async () => {
  await renderAt('/admin');
  await typeInto(searchInput(), 'kat');
  expect(menuLinks().map((a) => a.textContent.trim())).toEqual(['Produk', 'Kategori']); // "Katalog" cocok dengan grup
  await typeInto(searchInput(), 'aktivitas');
  expect(menuLinks().map((a) => a.textContent.trim())).toEqual(['Log Aktivitas']);
  await typeInto(searchInput(), 'zzz');
  expect(menuLinks()).toHaveLength(0);
  expect(sidebar().textContent).toContain('Tidak ada menu yang cocok');
  await typeInto(searchInput(), 'invoice');
  await key(searchInput(), 'Enter');
  await flush();
  expect(window.location.pathname).toBe('/admin/invoice');
  expect(searchInput().value).toBe('');
  expect(menuLinks()).toHaveLength(7);
});

test('laci di layar sempit: buka lewat hamburger, tutup lewat Escape, overlay, dan saat memilih menu; fokus dikelola', async () => {
  await renderAt('/admin/products');
  const btn = hamburger();
  expect(btn.getAttribute('aria-controls')).toBe('admin-sidebar');
  expect(btn.getAttribute('aria-expanded')).toBe('false');
  expect(sidebar().getAttribute('data-open')).toBe('false');
  expect(sidebar().className).toContain('invisible');
  expect(sidebar().className).toContain('lg:visible');
  expect(container.querySelector('[data-testid="admin-overlay"]')).toBeNull();

  // Buka -> fokus ke tombol tutup, Escape -> tutup dan fokus kembali ke hamburger.
  await act(async () => btn.click());
  expect(btn.getAttribute('aria-expanded')).toBe('true');
  expect(sidebar().getAttribute('data-open')).toBe('true');
  expect(sidebar().getAttribute('role')).toBe('dialog');
  expect(sidebar().className).toContain('translate-x-0');
  expect(container.querySelector('[data-testid="admin-overlay"]')).not.toBeNull();
  expect(document.activeElement.getAttribute('aria-label')).toBe('Tutup menu');
  expect(document.body.style.overflow).toBe('hidden');
  await key(document, 'Escape');
  expect(sidebar().getAttribute('data-open')).toBe('false');
  expect(document.activeElement).toBe(hamburger());
  expect(document.body.style.overflow).toBe('');

  // Overlay menutup.
  await act(async () => hamburger().click());
  await act(async () => container.querySelector('[data-testid="admin-overlay"]').click());
  expect(sidebar().getAttribute('data-open')).toBe('false');

  // Tombol tutup menutup.
  await act(async () => hamburger().click());
  await act(async () => sidebar().querySelector('button[aria-label="Tutup menu"]').click());
  expect(sidebar().getAttribute('data-open')).toBe('false');

  // Memilih menu menavigasi dan menutup laci.
  await act(async () => hamburger().click());
  await act(async () => linkByLabel('Pesanan').click());
  await flush();
  expect(window.location.pathname).toBe('/admin/orders');
  expect(sidebar().getAttribute('data-open')).toBe('false');
  expect(linkByLabel('Pesanan').getAttribute('aria-current')).toBe('page');
});

test('dashboard: kartu statistik, pintasan cepat, aktivitas terakhir', async () => {
  await renderAt('/admin');
  expect(container.querySelector('h1').textContent).toBe('Dashboard');
  const stats = container.querySelector('section[aria-label="Statistik"]');
  const text = stats.textContent;
  ['Produk aktif18', 'Kategori6', 'Menunggu pembayaran4', 'Dibayar2', 'Pesanan 7 hari terakhir9'].forEach((t) => expect(text).toContain(t));
  expect(stats.querySelectorAll('a svg')).toHaveLength(5);
  const shortcutLinks = [...container.querySelectorAll('section[aria-labelledby="pintasan-cepat"] a')];
  expect(shortcutLinks.map((a) => a.querySelector('.font-semibold').textContent)).toEqual([
    'Tambah Produk',
    'Pesanan baru',
    'Pengaturan Toko',
    'Log Aktivitas',
  ]);
  expect(shortcutLinks.map((a) => a.getAttribute('href'))).toEqual([
    '/admin/products?tambah=1',
    '/admin/orders?status=pending_payment',
    '/admin/settings',
    '/admin/activity',
  ]);
  expect(container.textContent).toContain('10 aktivitas terakhir');
  expect(container.textContent).toContain('Ubah harga');
  // /summary hanya diminta sekali saat mount (sidebar + dashboard berbagi data).
  expect(mockState.calls.filter((c) => c === '/summary')).toHaveLength(1);
});

test('pintasan Tambah Produk membuka form tambah produk', async () => {
  await renderAt('/admin/products?tambah=1');
  const dialog = container.querySelector('[role="dialog"][aria-modal="true"]:not(#admin-sidebar)');
  expect(dialog).not.toBeNull();
  expect(dialog.textContent).toContain('Tambah produk');
  expect(window.location.search).toBe('');
});

test('pintasan Pesanan baru memfilter status menunggu pembayaran', async () => {
  await renderAt('/admin/orders?status=pending_payment');
  expect(container.querySelector('select[aria-label="Filter status"]').value).toBe('pending_payment');
  expect(mockState.calls.some((c) => c.startsWith('/orders?') && c.includes('status=pending_payment'))).toBe(true);
});

test('status tidak dikenal di URL diabaikan', async () => {
  await renderAt('/admin/orders?status=constructor');
  expect(container.querySelector('select[aria-label="Filter status"]').value).toBe('');
});
