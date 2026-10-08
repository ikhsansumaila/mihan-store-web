// Tes render layout admin bergaya cPanel (sidebar, bilah ikon layar sempit, topbar, pencarian menu, dashboard).
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
          orders: { pendingConfirmation: 3, pendingPayment: 4, paid: 2, last7Days: 9 },
          customers: 12,
          recentActivity: [{ id: 1, action: 'product.update', summary: 'Ubah harga', userEmail: 'pemilik@example.com', createdAt: '2026-10-02T03:00:00Z' }],
        });
      if (path === '/categories') return Promise.resolve({ items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] });
      if (path.startsWith('/customers/7'))
        return Promise.resolve({ customer: { id: 7, name: 'Siti', username: 'siti', alias: null, status: 'active' }, recentOrders: [] });
      if (path.startsWith('/customers')) return Promise.resolve({ items: [], total: 0, perPage: 20 });
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
  expect(menuLinks().map((a) => [a.getAttribute('aria-label'), a.getAttribute('href')])).toEqual([
    ['Dashboard', '/admin'],
    ['Produk', '/admin/products'],
    ['Kategori', '/admin/categories'],
    ['Pesanan, 3 menunggu konfirmasi', '/admin/orders'],
    ['Pelanggan', '/admin/customers'],
    ['Invoice', '/admin/invoice'],
    ['Pricelist', '/admin/pricelist'],
    ['Pengaturan Toko', '/admin/settings'],
    ['Data Wilayah', '/admin/regions'],
    ['Log Aktivitas', '/admin/activity'],
  ]);
  const orders = linkByLabel('Pesanan');
  expect(orders.getAttribute('title')).toBe('Pesanan, 3 menunggu konfirmasi');
  expect(orders.querySelector('[data-badge="icon"]').textContent).toBe('3');
  expect(orders.querySelector('[data-badge="full"]').textContent).toBe('3');
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
  ['/admin/customers', 'Pelanggan', ['Admin', 'Penjualan', 'Pelanggan']],
  ['/admin/customers/7', 'Pelanggan', ['Admin', 'Penjualan', 'Pelanggan', 'Detail']],
  ['/admin/pricelist', 'Pricelist', ['Admin', 'Penjualan', 'Pricelist']],
  ['/admin/settings', 'Pengaturan Toko', ['Admin', 'Sistem', 'Pengaturan Toko']],
  ['/admin/regions', 'Data Wilayah', ['Admin', 'Sistem', 'Data Wilayah']],
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
  expect(menuLinks()).toHaveLength(10);
  await typeInto(searchInput(), 'daftar harga');
  expect(menuLinks().map((a) => a.textContent.trim())).toEqual(['Pricelist']);
  await typeInto(searchInput(), 'pricelist');
  await key(searchInput(), 'Enter');
  await flush();
  expect(window.location.pathname).toBe('/admin/pricelist');
});

test('layar sempit: bilah ikon selalu terlihat, label hanya untuk pembaca layar, tanpa hamburger/overlay', async () => {
  await renderAt('/admin/products');
  const sb = sidebar();
  // Bilah 64px (w-16) di bawah 1024px, sidebar penuh 260px di >= 1024px; selalu tampil (bukan laci).
  expect(sb.className).toContain('w-16');
  expect(sb.className).toContain('lg:w-[260px]');
  expect(sb.className).toContain('inset-y-0');
  ['invisible', '-translate-x-full', 'max-w-[85vw]'].forEach((c) => expect(sb.className.split(' ')).not.toContain(c));
  expect(sb.getAttribute('role')).toBeNull();
  expect(sb.getAttribute('aria-modal')).toBeNull();
  expect(sb.hasAttribute('data-open')).toBe(false);
  // Tidak ada hamburger, tombol tutup, maupun overlay.
  expect(container.querySelector('button[aria-label="Buka menu"]')).toBeNull();
  expect(container.querySelector('button[aria-label="Tutup menu"]')).toBeNull();
  expect(container.querySelector('[data-testid="admin-overlay"]')).toBeNull();
  expect(container.querySelector('[aria-controls="admin-sidebar"]')).toBeNull();

  // Setiap menu: ikon SVG, target sentuh 44px (h-11), tooltip title + aria-label; label teks
  // disembunyikan secara visual (sr-only) di mode ikon tetapi tetap ada di DOM.
  for (const a of menuLinks()) {
    expect(a.querySelector('svg')).not.toBeNull();
    expect(a.className).toContain('h-11');
    expect(a.getAttribute('title')).toBeTruthy();
    expect(a.getAttribute('aria-label')).toBe(a.getAttribute('title'));
    const label = a.querySelector('span.sr-only');
    expect(label).not.toBeNull();
    expect(label.className).toContain('lg:not-sr-only');
    expect(a.getAttribute('aria-label').startsWith(label.textContent)).toBe(true);
  }
  // Judul grup, kolom cari menu, email, dan versi hanya tampil di mode penuh.
  const fullOnly = (el) => {
    const c = el.className.split(/\s+/);
    return c.includes('hidden') && c.includes('lg:block');
  };
  const headings = [...sb.querySelectorAll('[data-group] > div:not([aria-hidden])')];
  expect(headings).toHaveLength(4);
  headings.forEach((h) => expect(fullOnly(h)).toBe(true));
  expect(fullOnly(searchInput().closest('div'))).toBe(true);
  expect(fullOnly(sb.querySelector('[data-testid="admin-email"]'))).toBe(true);
  // Kembali ke toko: ikon + tooltip, anchor biasa.
  const back = sb.querySelector('a[href="/"]');
  expect(back.getAttribute('title')).toBe('Kembali ke toko');
  expect(back.getAttribute('aria-label')).toBe('Kembali ke toko');
  expect(back.querySelector('svg')).not.toBeNull();
  expect(back.querySelector('span.sr-only').textContent).toBe('Kembali ke toko');

  // Menu aktif ditandai (aria-current + garis kiri/latar).
  const active = linkByLabel('Produk');
  expect(active.getAttribute('aria-current')).toBe('page');
  expect(active.className).toContain('border-purple-400');
  expect(active.className).toContain('bg-purple-600/30');
  expect(linkByLabel('Kategori').className).toContain('border-transparent');

  // Konten mengisi sisa lebar di samping bilah/sidebar, tanpa max-width pembatas.
  const main = container.querySelector('#admin-main');
  expect(main.className).not.toMatch(/max-w-/);
  expect(main.parentElement.className).toContain('pl-16');
  expect(main.parentElement.className).toContain('lg:pl-[260px]');
  expect(main.parentElement.className).toContain('min-w-0');

  // Topbar: judul + breadcrumb ringkas, inisial admin.
  expect(container.querySelector('[data-testid="admin-title"]').textContent).toBe('Produk');
  const crumbs = [...container.querySelectorAll('nav[aria-label="Breadcrumb"] li')];
  expect(crumbs[0].className).toContain('hidden sm:flex'); // "Admin" disembunyikan di layar sempit
  expect(crumbs[2].className).not.toContain('hidden');
  expect(container.querySelector('[aria-label="Admin: pemilik@example.com"]').textContent).toBe('p');

  // Memilih menu menavigasi tanpa mengunci scroll halaman.
  await act(async () => linkByLabel('Pesanan').click());
  await flush();
  expect(window.location.pathname).toBe('/admin/orders');
  expect(linkByLabel('Pesanan').getAttribute('aria-current')).toBe('page');
  expect(linkByLabel('Produk').getAttribute('aria-current')).toBeNull();
  expect(document.body.style.overflow).toBe('');
  await key(document, 'Escape');
  expect(sidebar().className).toContain('w-16');
});

test.each(['/admin', '/admin/products', '/admin/orders', '/admin/orders/5', '/admin/invoice', '/admin/pricelist', '/admin/settings', '/admin/activity'])(
  'halaman %s: tanpa max-width pembatas lebar halaman, tabel di dalam wadah scroll',
  async (path) => {
    await renderAt(path);
    const main = container.querySelector('#admin-main');
    expect(main.className).toContain('min-w-0');
    expect(main.className).toContain('break-words');
    const wrappers = [main, ...main.querySelectorAll(':scope > div, :scope > div > div')];
    wrappers.forEach((el) => expect(String(el.className)).not.toMatch(/\bmax-w-(2xl|3xl|4xl|5xl|6xl|7xl|screen)/));
    expect(container.innerHTML).not.toMatch(/w-screen|100vw/);
    main.querySelectorAll('table').forEach((t) => expect(t.parentElement.className).toContain('overflow-x-auto'));
  }
);

test('dashboard: kartu statistik, pintasan cepat, aktivitas terakhir', async () => {
  await renderAt('/admin');
  expect(container.querySelector('h1').textContent).toBe('Dashboard');
  const stats = container.querySelector('section[aria-label="Statistik"]');
  const text = stats.textContent;
  ['Produk aktif18', 'Kategori6', 'Menunggu konfirmasi3', 'Menunggu pembayaran4', 'Dibayar2', 'Pesanan 7 hari terakhir9', 'Pelanggan12'].forEach((t) => expect(text).toContain(t));
  expect(stats.querySelectorAll('a svg')).toHaveLength(7);
  const shortcutLinks = [...container.querySelectorAll('section[aria-labelledby="pintasan-cepat"] a')];
  expect(shortcutLinks.map((a) => a.querySelector('.font-semibold').textContent)).toEqual([
    'Tambah Produk',
    'Pesanan baru',
    'Pelanggan',
    'Buat Pricelist',
    'Pengaturan Toko',
    'Log Aktivitas',
  ]);
  expect(shortcutLinks.map((a) => a.getAttribute('href'))).toEqual([
    '/admin/products?tambah=1',
    '/admin/orders?status=pending_confirmation',
    '/admin/customers',
    '/admin/pricelist',
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

const activeTab = () => container.querySelector('[role="tablist"] [role="tab"][aria-selected="true"]');

test('pintasan Pesanan baru membuka tab menunggu konfirmasi; ?status=pending_payment tetap bekerja', async () => {
  await renderAt('/admin/orders?status=pending_confirmation');
  expect(activeTab().getAttribute('data-status')).toBe('pending_confirmation');
  expect(mockState.calls.some((c) => c.startsWith('/orders?') && c.includes('status=pending_confirmation'))).toBe(true);
  act(() => root.unmount());
  container.remove();
  await renderAt('/admin/orders?status=pending_payment');
  expect(activeTab().getAttribute('data-status')).toBe('pending_payment');
  expect(mockState.calls.some((c) => c.startsWith('/orders?') && c.includes('status=pending_payment'))).toBe(true);
});

test('status tidak dikenal di URL diabaikan (tab Semua)', async () => {
  await renderAt('/admin/orders?status=constructor');
  expect(activeTab().getAttribute('data-status')).toBe('all');
  expect(mockState.calls.filter((c) => c.startsWith('/orders?')).every((c) => !c.includes('status='))).toBe(true);
});

test('tab pesanan: jumlah dari ringkasan (konfirmasi, pembayaran, dibayar); menu Pesanan kembali ke tab bawaan Menunggu konfirmasi', async () => {
  await renderAt('/admin/orders?status=paid');
  const counts = Object.fromEntries(
    [...container.querySelectorAll('[role="tab"]')].map((t) => [t.getAttribute('data-status'), t.querySelector('[data-testid="tab-count"]')?.textContent ?? null]),
  );
  expect(counts).toEqual({ all: null, pending_confirmation: '3', pending_payment: '4', paid: '2', completed: null, cancelled: null });
  // Klik menu sidebar "Pesanan" (URL tanpa ?status) -> tab bawaan "Menunggu konfirmasi".
  mockState.calls = [];
  await act(async () => linkByLabel('Pesanan').click());
  await flush();
  expect(activeTab().getAttribute('data-status')).toBe('pending_confirmation');
  expect(mockState.calls.some((c) => c.startsWith('/orders?') && c.includes('status=pending_confirmation'))).toBe(true);
});
