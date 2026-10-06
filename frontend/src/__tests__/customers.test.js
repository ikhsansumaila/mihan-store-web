// Tes menu admin "Pelanggan", alias pelanggan di pesanan, dan toggle "Pakai nama alias" pada invoice.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { normalizeAlias, aliasLength, ALIAS_MAX } from '../admin/alias';
import { orderToInvoice } from '../invoicePdf';
import { buildAdminSummaryText } from '../shop/format';
import { clearListSnapshots } from '../admin/infiniteList';
import { installIntersectionObserver, intersect, uninstallIntersectionObserver } from '../testUtils/intersection';

const mockState = { calls: [], admin: {}, invoices: [], patchReply: null };

jest.mock('axios', () => ({
  __esModule: true,
  default: {
    get: () => Promise.resolve({ data: {} }),
    post: () => Promise.resolve({ data: {} }),
    put: () => Promise.resolve({ data: {} }),
    delete: () => Promise.resolve({ data: {} }),
  },
}));
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });
jest.mock('../invoicePdf', () => {
  const actual = jest.requireActual('../invoicePdf');
  return {
    ...actual,
    generateInvoicePdf: (inv) => {
      mockState.invoices.push(inv);
      return Promise.resolve();
    },
  };
});
jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path, opts) => {
      mockState.calls.push({ method: opts?.method || 'GET', path, body: opts?.body });
      if (path === '/me') return Promise.resolve({ user: { email: 'admin@example.com' } });
      if (opts?.method === 'PATCH' && path.endsWith('/alias')) {
        if (mockState.patchReply) return Promise.reject(mockState.patchReply);
        const a = opts.body.alias;
        return Promise.resolve({ id: Number(path.split('/')[2]), alias: a === '' ? null : a });
      }
      const key = Object.keys(mockState.admin)
        .filter((k) => path.startsWith(k))
        .sort((a, b) => b.length - a.length)[0];
      const v = key ? mockState.admin[key] : {};
      return typeof v === 'function' ? v(path) : Promise.resolve(v);
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

const typeInto = async (el, value) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
const click = async (el) => {
  await act(async () => {
    el.click();
  });
  await flush();
};
const byText = (sel, text) => [...container.querySelectorAll(sel)].find((e) => e.textContent.trim() === text);
const lastCall = (pred) => [...mockState.calls].reverse().find(pred);

const CUSTOMERS = {
  items: [
    { id: 11, name: 'Siti Aminah', username: 'siti', email: 'siti@example.com', phone: '+6281211112222', alias: 'Bu Siti Toko Maju', status: 'active', orderCount: 4, totalSpent: 250000, createdAt: '2026-10-01T03:00:00Z' },
    { id: 12, name: 'Bayu', username: 'bayu', email: 'bayu@example.com', phone: null, alias: null, status: 'suspended', orderCount: 0, totalSpent: 0, createdAt: '2026-10-02T03:00:00Z' },
  ],
  total: 2,
  page: 1,
  perPage: 20,
};

const ORDER = (alias) => ({
  id: 5,
  orderNo: 'MS-261003-0005',
  status: 'paid',
  subtotal: 90000,
  discount: 0,
  shippingFee: 10000,
  total: 100000,
  items: [{ name: 'Kerupuk', qty: 2, unitPrice: 45000, lineTotal: 90000, unit: 'pcs' }],
  recipient: { name: 'Penerima Rumah', phone: '+6281299998888', address: 'Jl. Melati 1', city: 'Tangerang', postalCode: '15111' },
  customer: { id: 11, name: 'Siti Aminah', username: 'siti', email: 'siti@example.com', alias },
  history: [],
  allowedNext: ['completed'],
  pricingLocked: true,
  createdAt: '2026-10-03T03:00:00Z',
});

beforeEach(() => {
  clearListSnapshots();
  mockState.calls = [];
  mockState.invoices = [];
  mockState.patchReply = null;
  mockState.admin = {
    '/summary': { products: {}, orders: {}, customers: 2, recentActivity: [] },
    '/customers': CUSTOMERS,
    '/customers/11': {
      customer: { ...CUSTOMERS.items[0], lastOrderAt: '2026-10-03T03:00:00Z' },
      recentOrders: [
        { id: 5, orderNo: 'MS-261003-0005', status: 'paid', statusLabel: 'Dibayar', total: 100000, createdAt: '2026-10-03T03:00:00Z' },
        { id: 4, orderNo: 'MS-261002-0004', status: 'cancelled', statusLabel: 'Dibatalkan', total: 50000, createdAt: '2026-10-02T03:00:00Z' },
      ],
    },
    '/settings': { settings: {} },
  };
});

afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
});

describe('normalizeAlias', () => {
  test('rapikan spasi, buang kontrol/bidi/tak terlihat, hitung code point', () => {
    const zw = String.fromCharCode(0x200b);
    const rlo = String.fromCharCode(0x202e);
    const nbsp = String.fromCharCode(0xa0);
    expect(normalizeAlias(`  Bu${nbsp}Siti \t Toko\nMaju ${zw}${rlo} `)).toBe('Bu Siti Toko Maju');
    expect(normalizeAlias('   ')).toBe('');
    expect(aliasLength('é'.repeat(ALIAS_MAX))).toBe(100);
    expect(aliasLength('😀😀')).toBe(2);
  });
});

test('menu Pelanggan di sidebar & bilah ikon (Penjualan, setelah Pesanan) dan bisa dicari', async () => {
  await renderAt('/admin');
  const links = [...container.querySelectorAll('#admin-sidebar nav a')];
  const labels = links.map((a) => a.getAttribute('aria-label'));
  expect(labels.indexOf('Pelanggan')).toBe(labels.findIndex((l) => l.startsWith('Pesanan')) + 1);
  const menu = links.find((a) => a.getAttribute('aria-label') === 'Pelanggan');
  expect(menu.getAttribute('href')).toBe('/admin/customers');
  expect(menu.getAttribute('title')).toBe('Pelanggan'); // tooltip bilah ikon
  expect(menu.querySelector('svg')).not.toBeNull();
  expect(menu.closest('[data-group]').getAttribute('data-group')).toBe('Penjualan');
  await typeInto(container.querySelector('input[aria-label="Cari menu"]'), 'alias');
  expect([...container.querySelectorAll('#admin-sidebar nav a')].map((a) => a.textContent.trim())).toEqual(['Pelanggan']);
  // Dashboard: kartu jumlah pelanggan + pintasan.
  await typeInto(container.querySelector('input[aria-label="Cari menu"]'), '');
  const stats = container.querySelector('section[aria-label="Statistik"]');
  expect(stats.textContent).toContain('Pelanggan2');
  expect(container.querySelector('section[aria-labelledby="pintasan-cepat"] a[href="/admin/customers"]')).not.toBeNull();
});

test('daftar pelanggan: alias menonjol, statistik, status, pencarian & urutan', async () => {
  await renderAt('/admin/customers');
  expect(container.querySelector('h1').textContent).toBe('Pelanggan');
  expect(container.querySelector('[data-testid="admin-title"]').textContent).toBe('Pelanggan');
  const row = container.querySelector('[data-customer-row="11"]');
  const alias = row.querySelector('[data-testid="alias"]');
  expect(alias.textContent).toBe('Bu Siti Toko Maju');
  expect(alias.className).toContain('font-semibold');
  expect(row.textContent).toContain('Siti Aminah');
  expect(row.textContent).toContain('siti@example.com');
  expect(row.textContent).toContain('+6281211112222');
  expect(row.textContent).toContain('Rp 250.000');
  expect(row.textContent).toContain('Aktif');
  const row2 = container.querySelector('[data-customer-row="12"]');
  expect(row2.textContent).toContain('Belum ada alias');
  expect(row2.textContent).toContain('Ditangguhkan');
  expect(row.querySelector('a').getAttribute('href')).toBe('/admin/customers/11');
  expect(lastCall((c) => c.path.startsWith('/customers')).path).toBe('/customers?sort=newest&page=1&per_page=20');

  await typeInto(container.querySelector('input[type="search"]:not([aria-label="Cari menu"])'), '  Toko Maju ');
  await click(byText('button', 'Cari'));
  expect(lastCall((c) => c.path.startsWith('/customers')).path).toBe('/customers?q=Toko+Maju&sort=newest&page=1&per_page=20');

  const sel = container.querySelector('select[aria-label="Urutkan pelanggan"]');
  expect([...sel.options].map((o) => o.value)).toEqual(['newest', 'orders', 'spent']);
  await act(async () => {
    sel.value = 'spent';
    sel.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  expect(lastCall((c) => c.path.startsWith('/customers')).path).toBe('/customers?q=Toko+Maju&sort=spent&page=1&per_page=20');
});

test('modal edit alias di daftar: penghitung, batas 100, simpan & kosongkan = hapus', async () => {
  await renderAt('/admin/customers');
  await click(container.querySelector('button[aria-label="Ubah alias Bayu"]'));
  const dialog = container.querySelector('[role="dialog"]');
  expect(dialog.textContent).toContain('Beri alias pelanggan');
  const input = dialog.querySelector('input');
  expect(dialog.querySelector('[data-testid="alias-counter"]').textContent).toBe('0/100');
  await typeInto(input, 'a'.repeat(101));
  expect(dialog.querySelector('[data-testid="alias-counter"]').textContent).toBe('101/100');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  expect(byText('button', 'Simpan alias').disabled).toBe(true);
  expect(dialog.textContent).toContain('Alias maksimal 100 karakter');
  await typeInto(input, '  Pak   Bayu Grosir ');
  expect(dialog.querySelector('[data-testid="alias-counter"]').textContent).toBe('15/100');
  await click(byText('button', 'Simpan alias'));
  const patch = lastCall((c) => c.method === 'PATCH');
  expect(patch).toEqual({ method: 'PATCH', path: '/customers/12/alias', body: { alias: 'Pak Bayu Grosir' } });
  expect(container.querySelector('[role="dialog"]')).toBeNull();
  expect(container.querySelector('[data-customer-row="12"] [data-testid="alias"]').textContent).toBe('Pak Bayu Grosir');

  // Kosongkan = hapus alias.
  await click(container.querySelector('button[aria-label="Ubah alias Siti Aminah"]'));
  await typeInto(container.querySelector('[role="dialog"] input'), '   ');
  await click(byText('button', 'Simpan alias'));
  expect(lastCall((c) => c.method === 'PATCH').body).toEqual({ alias: '' });
  expect(container.querySelector('[data-customer-row="11"]').textContent).toContain('Belum ada alias');
});

test('modal edit alias menampilkan galat server', async () => {
  mockState.patchReply = Object.assign(new Error('Alias maksimal 100 karakter'), { status: 400 });
  await renderAt('/admin/customers');
  await click(container.querySelector('button[aria-label="Ubah alias Bayu"]'));
  await typeInto(container.querySelector('[role="dialog"] input'), 'x');
  await click(byText('button', 'Simpan alias'));
  expect(container.querySelector('[role="dialog"]').textContent).toContain('Alias maksimal 100 karakter');
});

test('detail pelanggan: data akun, statistik, 10 pesanan terakhir bertaut nomor pesanan, ubah alias', async () => {
  await renderAt('/admin/customers/11');
  const bc = [...container.querySelectorAll('nav[aria-label="Breadcrumb"] li')].map((li) => li.textContent.replace('/', '').trim());
  expect(bc).toEqual(['Admin', 'Penjualan', 'Pelanggan', 'Detail']);
  expect(container.querySelector('h1').textContent).toContain('Bu Siti Toko Maju');
  expect(container.textContent).toContain('Nama akun: Siti Aminah');
  const stats = container.querySelector('section[aria-label="Statistik pelanggan"]').textContent;
  expect(stats).toContain('Jumlah pesanan4');
  expect(stats).toContain('Total belanjaRp 250.000');
  const link = container.querySelector('a[href="/admin/orders/MS-261003-0005"]');
  expect(link.textContent).toBe('MS-261003-0005');
  expect(container.textContent).toContain('Dibatalkan');
  await click(byText('button', 'Ubah alias'));
  const input = container.querySelector('[role="dialog"] input');
  expect(input.value).toBe('Bu Siti Toko Maju');
  await typeInto(input, 'Bu Siti (Pasar Baru)');
  await click(byText('button', 'Simpan alias'));
  expect(lastCall((c) => c.method === 'PATCH')).toEqual({ method: 'PATCH', path: '/customers/11/alias', body: { alias: 'Bu Siti (Pasar Baru)' } });
  expect(container.querySelector('h1').textContent).toContain('Bu Siti (Pasar Baru)');
});

test('daftar pesanan: alias tebal di kolom pemesan, nama akun kecil; label cari menyebut alias', async () => {
  mockState.admin['/orders'] = {
    items: [
      { id: 5, orderNo: 'MS-261003-0005', status: 'paid', total: 100000, itemCount: 2, recipientName: 'Penerima Rumah', city: 'Tangerang', customerName: 'Siti Aminah', customer: { id: 11, alias: 'Bu Siti Toko Maju' }, createdAt: '2026-10-03T03:00:00Z' },
      { id: 6, orderNo: 'MS-261003-0006', status: 'paid', total: 100000, itemCount: 2, recipientName: 'X', city: 'Y', customerName: 'Bayu', customer: { id: 12, alias: null }, createdAt: '2026-10-03T03:00:00Z' },
    ],
    total: 2,
    perPage: 20,
  };
  await renderAt('/admin/orders');
  const alias = container.querySelector('[data-testid="order-alias"]');
  expect(alias.textContent).toBe('Bu Siti Toko Maju');
  expect(alias.className).toContain('font-semibold');
  expect(alias.nextSibling.textContent).toBe('Siti Aminah');
  expect(alias.nextSibling.className).toContain('text-xs');
  expect(container.querySelectorAll('[data-testid="order-alias"]')).toHaveLength(1);
  expect(container.textContent).toContain('Cari (no. pesanan, nama, alias, telepon)');
});

test('detail pesanan: alias di akun pemesan + tautan pelanggan; toggle invoice bawaan mati; WA tanpa alias', async () => {
  mockState.admin['/orders/5'] = ORDER('Bu Siti Toko Maju');
  await renderAt('/admin/orders/5');
  const box = container.querySelector('[data-testid="order-customer"]');
  expect(box.querySelector('[data-testid="order-detail-alias"]').textContent).toBe('Bu Siti Toko Maju');
  expect(box.querySelector('a[href="/admin/customers/11"]')).not.toBeNull();

  const toggle = container.querySelector('[data-testid="invoice-use-alias"]');
  expect(toggle).not.toBeNull();
  expect(toggle.checked).toBe(false);
  expect(toggle.closest('label').textContent).toContain('Pakai nama alias');
  await click(byText('button', 'Cetak invoice'));
  expect(mockState.invoices[0].customerName).toBe('Penerima Rumah');
  await click(toggle);
  expect(toggle.checked).toBe(true);
  await click(byText('button', 'Cetak invoice'));
  expect(mockState.invoices[1].customerName).toBe('Bu Siti Toko Maju');
  // Data penerima di invoice tetap (alamat kirim).
  expect(mockState.invoices[1].recipient.name).toBe('Penerima Rumah');

  const wa = [...container.querySelectorAll('a')].find((a) => a.textContent.includes('Kirim ringkasan ke WhatsApp pelanggan'));
  const text = decodeURIComponent(wa.getAttribute('href'));
  expect(text).toContain('Halo Penerima Rumah');
  expect(text).not.toContain('Bu Siti');

  // Ubah alias dari detail pesanan; menghapus alias menyembunyikan toggle.
  await click(byText('button', 'Ubah alias'));
  await typeInto(container.querySelector('[role="dialog"] input'), '');
  await click(byText('button', 'Simpan alias'));
  expect(lastCall((c) => c.method === 'PATCH')).toEqual({ method: 'PATCH', path: '/customers/11/alias', body: { alias: '' } });
  expect(container.querySelector('[data-testid="invoice-use-alias"]')).toBeNull();
  expect(container.querySelector('[data-testid="order-customer"]').textContent).toContain('Belum ada alias');
  await click(byText('button', 'Cetak invoice'));
  expect(mockState.invoices[2].customerName).toBe('Penerima Rumah');
});

test('detail pesanan tanpa alias: toggle invoice tidak muncul', async () => {
  mockState.admin['/orders/5'] = ORDER(null);
  await renderAt('/admin/orders/5');
  expect(container.querySelector('[data-testid="invoice-use-alias"]')).toBeNull();
  expect(container.textContent).not.toContain('Pakai nama alias');
  expect(container.querySelector('[data-testid="order-customer"]').textContent).toContain('Belum ada alias');
  await click(byText('button', 'Beri alias'));
  await typeInto(container.querySelector('[role="dialog"] input'), 'Bu Siti');
  await click(byText('button', 'Simpan alias'));
  const toggle = container.querySelector('[data-testid="invoice-use-alias"]');
  expect(toggle).not.toBeNull();
  expect(toggle.checked).toBe(false);
});

test('orderToInvoice & teks WhatsApp: alias hanya bila diminta, teks WA tidak pernah memuat alias', () => {
  const o = ORDER('Bu Siti Toko Maju');
  expect(orderToInvoice(o, {}).customerName).toBe('Penerima Rumah');
  expect(orderToInvoice(o, {}, { useAlias: false }).customerName).toBe('Penerima Rumah');
  expect(orderToInvoice(o, {}, { useAlias: true }).customerName).toBe('Bu Siti Toko Maju');
  expect(orderToInvoice(ORDER(null), {}, { useAlias: true }).customerName).toBe('Penerima Rumah');
  const wa = buildAdminSummaryText(o, { bank_name: 'BCA', bank_account_number: '123' });
  expect(wa).not.toContain('Bu Siti');
  expect(wa).toContain('Halo Penerima Rumah');
});

describe('daftar pelanggan: kartu di HP, gulir tanpa batas', () => {
  const mkCustomer = (id) => ({
    id,
    name: `Pelanggan ${id}`,
    username: `p${id}`,
    email: `p${id}@example.com`,
    phone: null,
    alias: id === 1 ? 'Bu Satu' : null,
    status: 'active',
    orderCount: id,
    totalSpent: 1000 * id,
    createdAt: '2026-10-01T03:00:00Z',
  });
  // 30 pelanggan, 20 per halaman; halaman 2 sengaja memuat ulang id 20 (uji dedup).
  const paged = (path) => {
    const page = Number(new URLSearchParams(path.split('?')[1]).get('page'));
    const ids = page === 1 ? [...Array(20)].map((_, i) => i + 1) : [20, ...[...Array(10)].map((_, i) => i + 21)];
    return Promise.resolve({ items: ids.map(mkCustomer), total: 30, page, perPage: 20 });
  };
  const cards = () => [...container.querySelectorAll('[data-customer-card]')];
  const btn = (text) => byText('button', text);
  const listPaths = () => mockState.calls.filter((c) => c.path.startsWith('/customers?')).map((c) => c.path);
  const scrollDown = async () => {
    await act(async () => {
      intersect();
      intersect(false);
    });
    await flush();
  };

  beforeEach(() => installIntersectionObserver());
  afterEach(() => uninstallIntersectionObserver());

  test('kartu: info utama tanpa tabel lebar, seluruh kartu menuju detail, tombol Alias tidak ikut memicu', async () => {
    await renderAt('/admin/customers');
    expect(container.querySelector('[data-testid="customer-cards"]').className).toContain('md:hidden');
    expect(container.querySelector('table').parentElement.className).toMatch(/\bhidden\b.*\bmd:block\b/);
    const [c] = cards();
    ['Bu Siti Toko Maju', 'Siti Aminah', '@siti', 'siti@example.com', '4 pesanan', 'Aktif'].forEach((t) => expect(c.textContent).toContain(t));
    const link = c.querySelector('a');
    expect(link.getAttribute('href')).toBe('/admin/customers/11');
    expect(link.className).toContain('after:absolute');
    expect(link.className).toContain('after:inset-0');
    const aliasBtn = c.querySelector('button[aria-label="Ubah alias Siti Aminah"]');
    expect(aliasBtn.closest('a')).toBeNull();
    expect(aliasBtn.className).toMatch(/\brelative\b.*\bz-10\b/);
    await click(aliasBtn);
    expect(window.location.pathname).toBe('/admin/customers');
    expect(container.querySelector('[role="dialog"]')).not.toBeNull();
    // Simpan alias: kartu & baris diperbarui di tempat tanpa memuat ulang daftar.
    const before = listPaths().length;
    const input = container.querySelector('[role="dialog"] input');
    await typeInto(input, 'Bu Siti Baru');
    await click(byText('[role="dialog"] button', 'Simpan alias'));
    expect(cards()[0].querySelector('[data-testid="alias"]').textContent).toBe('Bu Siti Baru');
    expect(listPaths()).toHaveLength(before);
    expect(btn('Berikutnya ›')).toBeUndefined();
  });

  test('gulir memuat halaman berikutnya tanpa duplikat; pencarian & urutan mulai dari halaman 1', async () => {
    mockState.admin['/customers?'] = paged;
    await renderAt('/admin/customers');
    expect(cards()).toHaveLength(20);
    await scrollDown();
    expect(cards()).toHaveLength(30);
    expect(container.querySelector('[data-testid="infinite-footer"] [role="status"]').textContent).toBe('Semua pelanggan sudah ditampilkan (30).');
    expect(listPaths()).toEqual(['/customers?sort=newest&page=1&per_page=20', '/customers?sort=newest&page=2&per_page=20']);
    const sel = container.querySelector('select[aria-label="Urutkan pelanggan"]');
    await act(async () => {
      Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set.call(sel, 'spent');
      sel.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(listPaths().at(-1)).toBe('/customers?sort=spent&page=1&per_page=20');
    expect(cards()).toHaveLength(20);
  });

  test('kembali dari detail pelanggan: pencarian, urutan, dan daftar dipulihkan lalu disegarkan', async () => {
    mockState.admin['/customers?'] = paged;
    mockState.admin['/customers/1'] = { customer: { ...mkCustomer(1), lastOrderAt: null }, recentOrders: [] };
    await renderAt('/admin/customers');
    await typeInto(container.querySelector('input[type="search"]:not([aria-label="Cari menu"])'), 'Pel');
    await click(btn('Cari'));
    await scrollDown();
    expect(cards()).toHaveLength(30);
    await click(cards()[0].querySelector('a'));
    expect(window.location.pathname).toBe('/admin/customers/1');
    mockState.calls = [];
    await click(byText('a', '‹ Semua pelanggan'));
    expect(window.location.pathname).toBe('/admin/customers');
    expect(container.querySelector('input[type="search"]:not([aria-label="Cari menu"])').value).toBe('Pel');
    expect(cards()).toHaveLength(30);
    expect(listPaths()).toEqual(['/customers?q=Pel&sort=newest&page=1&per_page=20', '/customers?q=Pel&sort=newest&page=2&per_page=20']);
  });
});
