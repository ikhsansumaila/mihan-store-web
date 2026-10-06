// Tes daftar produk admin: kartu di HP (tanpa geser horizontal), tabel di layar lebar, gulir tanpa batas.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { installIntersectionObserver, intersect, uninstallIntersectionObserver } from '../testUtils/intersection';

const mockState = { calls: [], total: 45, active: {} };

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
  const product = (id) => ({
    id,
    name: `Produk ${id}`,
    categoryId: 1,
    categoryName: 'Kerupuk',
    price: 1000 * id,
    unit: 'pcs',
    isActive: mockState.active[id] ?? true,
    tiers: id === 1 ? [{ minQty: 10, unitPrice: 900 }] : [],
    description: '',
  });
  return {
    ...actual,
    adminFetch: (path, opts) => {
      const method = opts?.method || 'GET';
      mockState.calls.push({ method, url: path, body: opts?.body });
      if (path === '/me') return Promise.resolve({ user: { email: 'admin@example.com' } });
      if (path === '/summary') return Promise.resolve({ orders: {} });
      if (path === '/categories') return Promise.resolve({ items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] });
      const m = path.match(/^\/products\/(\d+)\/active$/);
      if (m && method === 'PATCH') {
        mockState.active[m[1]] = opts.body.active;
        return Promise.resolve({});
      }
      if (path.startsWith('/products?')) {
        const page = Number(new URLSearchParams(path.split('?')[1]).get('page'));
        const start = (page - 1) * 20 + 1;
        const end = Math.min(page * 20, mockState.total);
        const items = [];
        for (let id = start; id <= end; id += 1) items.push(product(id));
        return Promise.resolve({ items, total: mockState.total, page, perPage: 20 });
      }
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
  mockState.total = 45;
  mockState.active = {};
  installIntersectionObserver();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  uninstallIntersectionObserver();
});

const listCalls = () => mockState.calls.filter((c) => c.url.startsWith('/products?')).map((c) => c.url);
const cards = () => [...container.querySelectorAll('[data-testid="product-card"]')];
const rows = () => [...container.querySelectorAll('tbody tr')].filter((tr) => tr.querySelector('button'));
const footerText = () => container.querySelector('[data-testid="infinite-footer"] [role="status"]').textContent;
const dialog = () => container.querySelector('[role="dialog"][aria-modal="true"]:not(#admin-sidebar)');
const scrollDown = async () => {
  await act(async () => {
    intersect();
    intersect(false);
  });
  await flush();
};

test('produk: kartu di HP, tabel di layar lebar, tanpa paginasi', async () => {
  await renderAt('/admin/products');
  const list = container.querySelector('[data-testid="product-cards"]');
  expect(list.className).toContain('md:hidden');
  expect(container.querySelector('table').parentElement.className).toMatch(/\bhidden\b.*\bmd:block\b/);
  expect(cards()).toHaveLength(20);
  expect(rows()).toHaveLength(20);
  const c = cards()[0];
  ['Produk 1', '#1', 'Kerupuk', 'Rp', '/ pcs', 'Grosir (1 jenjang)', 'Aktif', 'Ubah', 'Hapus'].forEach((t) => expect(c.textContent).toContain(t));
  expect(container.textContent).not.toMatch(/halaman \d+ dari/);
  expect([...container.querySelectorAll('button')].some((b) => /Berikutnya|Sebelumnya/.test(b.textContent))).toBe(false);
});

test('produk: ketuk kartu membuka form ubah; tombol status/hapus di kartu tidak membuka form', async () => {
  await renderAt('/admin/products');
  await act(async () => cards()[1].querySelector('.font-medium').click());
  expect(dialog().textContent).toContain('Ubah produk #2');
  await act(async () => dialog().querySelector('button[aria-label="Tutup"]').click());
  expect(dialog()).toBeNull();

  // Status di kartu: PATCH, tanpa membuka form, daftar disegarkan di tempat.
  mockState.calls = [];
  const toggle = [...cards()[1].querySelectorAll('button')].find((b) => b.textContent === 'Aktif');
  await act(async () => toggle.click());
  await flush();
  expect(dialog()).toBeNull();
  expect(mockState.calls[0]).toMatchObject({ method: 'PATCH', url: '/products/2/active', body: { active: false } });
  expect(listCalls()).toEqual(['/products?page=1&per_page=20']);
  expect([...cards()[1].querySelectorAll('button')].some((b) => b.textContent === 'Nonaktif')).toBe(true);

  const del = [...cards()[1].querySelectorAll('button')].find((b) => b.textContent === 'Hapus');
  await act(async () => del.click());
  expect(dialog().textContent).toContain('Hapus produk?');
});

test('produk: gulir memuat halaman berikutnya dengan filter yang sama; ganti filter mulai dari halaman 1', async () => {
  await renderAt('/admin/products');
  await scrollDown();
  expect(cards()).toHaveLength(40);
  await scrollDown();
  expect(cards()).toHaveLength(45);
  expect(footerText()).toBe('Semua produk sudah ditampilkan (45).');
  expect(listCalls()).toEqual(['/products?page=1&per_page=20', '/products?page=2&per_page=20', '/products?page=3&per_page=20']);

  // Setelah halaman 3 dimuat, menyegarkan (mis. sesudah ubah status) memuat ulang halaman 1..3.
  mockState.calls = [];
  const toggle = [...cards()[44].querySelectorAll('button')].find((b) => b.textContent === 'Aktif');
  await act(async () => toggle.click());
  await flush();
  expect(listCalls()).toEqual(['/products?page=1&per_page=20', '/products?page=2&per_page=20', '/products?page=3&per_page=20']);
  expect(cards()).toHaveLength(45);

  mockState.calls = [];
  const statusSelect = [...container.querySelectorAll('select')].find((s) => s.textContent.includes('Semua status'));
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set.call(statusSelect, 'aktif');
    statusSelect.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  expect(listCalls()).toEqual(['/products?status=aktif&page=1&per_page=20']);
  expect(cards()).toHaveLength(20);
  await scrollDown();
  expect(listCalls().at(-1)).toBe('/products?status=aktif&page=2&per_page=20');
});

test('produk: daftar kosong menampilkan "Tidak ada produk." tanpa pesan akhir', async () => {
  mockState.total = 0;
  await renderAt('/admin/products');
  expect(container.textContent).toContain('Tidak ada produk.');
  expect(footerText()).toBe('');
});

// Kategori: API tidak berhalaman, jadi hanya kartu (HP) / tabel (md+), tanpa gulir bertahap.
test('kategori: kartu di HP, ketuk kartu = Ubah, tombol Hapus di kartu tidak membuka form', async () => {
  await renderAt('/admin/categories');
  const list = container.querySelector('[data-testid="category-cards"]');
  expect(list.className).toContain('md:hidden');
  expect(container.querySelector('table').parentElement.className).toMatch(/\bhidden\b.*\bmd:block\b/);
  const [card] = container.querySelectorAll('[data-testid="category-card"]');
  ['Kerupuk', 'kerupuk', 'Urutan', 'Produk aktif / total'].forEach((t) => expect(card.textContent).toContain(t));
  expect(container.querySelector('[data-testid="infinite-footer"]')).toBeNull();

  await act(async () => card.querySelector('.font-medium').click());
  expect(dialog().textContent).toContain('Ubah kategori');
  expect(dialog().querySelector('input').value).toBe('Kerupuk');
  await act(async () => dialog().querySelector('button[aria-label="Tutup"]').click());

  const del = [...card.querySelectorAll('button')].find((b) => b.textContent === 'Hapus');
  await act(async () => del.click());
  expect(dialog().textContent).toContain('Hapus kategori?');
  expect(dialog().textContent).not.toContain('Ubah kategori');
});
