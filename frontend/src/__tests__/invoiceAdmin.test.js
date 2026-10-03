// Tes ringan (jsdom, tanpa dependensi tambahan): halaman invoice pindah ke /admin/invoice.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa (bukan jest.fn).
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';

jest.mock('axios', () => ({
  __esModule: true,
  default: {
    get: (url) =>
      Promise.resolve({
        data: url.endsWith('/auth/me') ? { user: { name: 'Admin', role: 'admin' } } : [],
      }),
    post: () => Promise.resolve({ data: {} }),
  },
}));
jest.mock('jspdf', () => ({ __esModule: true, default: jest.fn() }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), {
  virtual: true,
});
jest.mock('../admin/api', () => ({
  __esModule: true,
  ADMIN_HEADER: {},
  adminFetch: (path) =>
    Promise.resolve(path === '/me' ? { user: { email: 'admin@example.com' } } : {}),
}));

const App = require('../App').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

const realLocation = window.location;
let container;
let root;

// Ganti window.location dengan objek tiruan agar replace() bisa diamati
// (BrowserRouter tetap membaca pathname/search/hash dari sini).
const setLocation = (path) => {
  const url = new URL(path, 'http://localhost');
  window.history.replaceState({}, '', path);
  delete window.location;
  window.location = {
    href: url.href,
    origin: url.origin,
    protocol: url.protocol,
    host: url.host,
    hostname: url.hostname,
    port: url.port,
    pathname: url.pathname,
    search: url.search,
    hash: url.hash,
    assign: jest.fn(),
    reload: jest.fn(),
    replace: jest.fn(),
    toString: () => url.href,
  };
};

const renderAt = async (path, user = null) => {
  localStorage.clear();
  if (user) {
    localStorage.setItem('token', 'tes');
    localStorage.setItem('user', JSON.stringify(user));
  }
  setLocation(path);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
  await act(async () => {
    root.render(<App />);
  });
  // Biarkan promise (adminFetch/axios) selesai.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  window.location = realLocation;
});

const hrefs = () => [...container.querySelectorAll('a')].map((a) => a.getAttribute('href'));

test('/admin/invoice merender form invoice di dalam layout admin dengan menu Invoice aktif', async () => {
  await renderAt('/admin/invoice');
  expect(container.querySelector('h1').textContent).toBe('Buat Invoice');
  expect(container.querySelector('input[placeholder="Nama Pelanggan"]')).not.toBeNull();
  expect(container.textContent).toContain('Preview PDF');
  expect(container.textContent).toContain('Masuk sebagai admin@example.com');

  const tabs = [...container.querySelectorAll('#admin-sidebar nav a')];
  expect(tabs.map((a) => a.textContent.trim())).toEqual([
    'Dashboard',
    'Produk',
    'Kategori',
    'Pesanan',
    'Pelanggan',
    'Invoice',
    'Pricelist',
    'Pengaturan Toko',
    'Data Wilayah',
    'Log Aktivitas',
  ]);
  const inv = tabs.find((a) => a.textContent.trim() === 'Invoice');
  expect(inv.getAttribute('href')).toBe('/admin/invoice');
  expect(inv.getAttribute('aria-current')).toBe('page');
  expect(inv.className).toContain('border-purple-400');
  const active = tabs.filter((a) => a.getAttribute('aria-current') === 'page');
  expect(active).toHaveLength(1);
  expect(window.location.replace).not.toHaveBeenCalled();
});

test.each(['/', '/login', '/register', '/privasi', '/syarat'])(
  'halaman publik %s tidak punya tautan invoice',
  async (path) => {
    await renderAt(path);
    expect(hrefs().some((h) => h && h.includes('invoice'))).toBe(false);
    expect(container.textContent).not.toContain('Buat Invoice');
  }
);

test('navbar admin: anchor biasa ke /admin tetap ada, tanpa tautan invoice', async () => {
  await renderAt('/', { name: 'Admin', role: 'admin' });
  const nav = container.querySelector('nav');
  const adminLink = [...nav.querySelectorAll('a')].find((a) => a.textContent.trim() === 'Admin');
  expect(adminLink.getAttribute('href')).toBe('/admin');
  expect([...nav.querySelectorAll('a')].some((a) => a.getAttribute('href').includes('invoice'))).toBe(false);
});

test('/invoice/create memicu pengalihan penuh ke /admin/invoice tanpa menampilkan form', async () => {
  await renderAt('/invoice/create');
  expect(window.location.replace).toHaveBeenCalledWith('/admin/invoice');
  expect(container.querySelector('input[placeholder="Nama Pelanggan"]')).toBeNull();
  expect(container.textContent).toContain('Mengalihkan');
});
