// Tes render halaman Pricelist (/admin/pricelist): menu, pengaturan, pratinjau, unduh, bagikan (Web Share & fallback).
// jsdom tidak punya Canvas 2D: konteks tiruan dipasang agar alur render -> PNG -> pratinjau bisa diuji.
// Catatan: CRA memakai resetMocks, jadi mock dipasang ulang di beforeEach.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';
import { defaultDateText } from '../pricelist/layout';

const mockState = { calls: [], settings: {}, products: [] };

jest.mock('axios', () => ({
  __esModule: true,
  default: { get: () => Promise.resolve({ data: [] }), post: () => Promise.resolve({ data: {} }) },
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
      if (path === '/summary') return Promise.resolve({ products: {}, orders: {}, recentActivity: [] });
      if (path === '/settings') return Promise.resolve({ settings: mockState.settings });
      if (path === '/categories')
        return Promise.resolve({
          items: [
            { id: 2, name: 'Tepung', slug: 'tepung', sortOrder: 20 },
            { id: 1, name: 'Kerupuk', slug: 'kerupuk', sortOrder: 10 },
          ],
        });
      if (path.startsWith('/products')) {
        const page = Number(new URLSearchParams(path.split('?')[1]).get('page') || 1);
        const per = Number(new URLSearchParams(path.split('?')[1]).get('per_page') || 20);
        const items = mockState.products.slice((page - 1) * per, page * per);
        return Promise.resolve({ items, total: mockState.products.length, page, perPage: per });
      }
      return Promise.resolve({});
    },
  };
});

const App = require('../App').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

const baseProducts = [
  { id: 1, name: 'Kerupuk Finna Udang', categoryId: 1, categoryName: 'Kerupuk', price: 45000, isActive: true },
  { id: 2, name: 'Kerupuk Aci', categoryId: 1, categoryName: 'Kerupuk', price: 25000, isActive: true },
  { id: 3, name: 'Tepung Beras', categoryId: 2, categoryName: 'Tepung', price: 32000, isActive: true },
  { id: 4, name: 'Tepung Maizena', categoryId: 2, categoryName: 'Tepung', price: 28000, isActive: false },
];

let container;
let root;
let drawn;
let opened;
let downloads;

const fakeCtx = () => {
  const ctx = {
    font: '',
    fillStyle: '',
    textAlign: 'left',
    textBaseline: 'alphabetic',
    measureText: (s) => ({ width: String(s).length * 16 }),
    fillText: (s) => drawn.push(String(s)),
  };
  ['fillRect', 'beginPath', 'moveTo', 'arcTo', 'closePath', 'fill', 'drawImage', 'stroke'].forEach((m) => {
    ctx[m] = () => {};
  });
  return ctx;
};

const flush = async (ms = 0) => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
};

const renderAt = async (path) => {
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

const waitPreview = async () => {
  for (let i = 0; i < 20; i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await flush(60);
    const imgs = container.querySelectorAll('[data-testid="pl-previews"] img');
    const status = container.querySelector('[data-testid="pl-status"]');
    if (imgs.length && !status.textContent.includes('Memperbarui')) return imgs;
  }
  return container.querySelectorAll('[data-testid="pl-previews"] img');
};

const button = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith(text));
const click = async (el) => {
  await act(async () => {
    el.click();
  });
  await flush();
};

const savedImage = window.Image;
const savedOpen = window.open;

beforeEach(() => {
  mockState.calls = [];
  mockState.settings = { store_whatsapp: '081234567890' };
  mockState.products = baseProducts;
  drawn = [];
  opened = [];
  downloads = [];
  HTMLCanvasElement.prototype.getContext = function getContext() {
    if (!this.mockCtx) this.mockCtx = fakeCtx();
    return this.mockCtx;
  };
  HTMLCanvasElement.prototype.toBlob = function toBlob(cb) {
    setTimeout(() => cb(new Blob([`png-${this.width}x${this.height}`], { type: 'image/png' })), 0);
  };
  let n = 0;
  URL.createObjectURL = () => {
    n += 1;
    return `blob:mock/${n}`;
  };
  URL.revokeObjectURL = () => {};
  // Logo gagal dimuat -> pricelist tetap dibuat tanpa logo.
  window.Image = class {
    set src(v) {
      this.s = v;
      setTimeout(() => this.onerror && this.onerror(), 0);
    }

    get src() {
      return this.s;
    }
  };
  window.open = (url, target, features) => {
    opened.push([url, target, features]);
    return null;
  };
  HTMLAnchorElement.prototype.click = function clickAnchor() {
    if (this.download) downloads.push(this.download);
  };
  delete navigator.share;
  delete navigator.canShare;
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  window.Image = savedImage;
  window.open = savedOpen;
  delete navigator.share;
  delete navigator.canShare;
});

const sidebarLinks = () => [...container.querySelectorAll('#admin-sidebar nav a')];

test('menu Pricelist ada di grup Penjualan setelah Invoice (sidebar + bilah ikon), breadcrumb sesuai', async () => {
  await renderAt('/admin/pricelist');
  const links = sidebarLinks();
  const labels = links.map((a) => a.getAttribute('data-menu'));
  expect(labels.indexOf('Pricelist')).toBe(labels.indexOf('Invoice') + 1);
  const pl = links.find((a) => a.getAttribute('data-menu') === 'Pricelist');
  expect(pl.getAttribute('href')).toBe('/admin/pricelist');
  expect(pl.closest('[data-group]').getAttribute('data-group')).toBe('Penjualan');
  expect(pl.getAttribute('aria-current')).toBe('page');
  // Bilah ikon: ikon SVG + tooltip; label sr-only.
  expect(pl.querySelector('svg')).not.toBeNull();
  expect(pl.getAttribute('title')).toBe('Pricelist');
  expect(pl.querySelector('span.sr-only').textContent).toBe('Pricelist');
  const crumbs = [...container.querySelectorAll('nav[aria-label="Breadcrumb"] li')].map((li) => li.textContent.replace('/', '').trim());
  expect(crumbs).toEqual(['Admin', 'Penjualan', 'Pricelist']);
  expect(container.querySelector('[data-testid="admin-title"]').textContent).toBe('Pricelist');
});

test('pengaturan default, pilihan produk default (aktif saja), pratinjau, tombol unduh & bagikan', async () => {
  await renderAt('/admin/pricelist');
  expect(container.querySelector('h1').textContent).toBe('Pricelist');
  const byLabel = (text) => {
    const l = [...container.querySelectorAll('label')].find((x) => x.textContent.trim().startsWith(text));
    return l && container.querySelector(`#${l.getAttribute('for')}`);
  };
  expect(byLabel('Judul').value).toBe('Daftar Harga Mihan Store');
  expect(byLabel('Keterangan tanggal').value).toBe(defaultDateText(new Date()));
  expect(byLabel('Catatan').getAttribute('maxlength')).toBe('200');
  expect(container.querySelectorAll('input[name="pl-theme"]')).toHaveLength(3);
  expect(container.querySelector('input[name="pl-theme"]:checked').value).toBe('ungu');
  expect(container.querySelector('input[name="pl-cols"]:checked').value).toBe('1');

  // Daftar centang: urutan kategori sortOrder (Kerupuk, Tepung), produk urut nama, nonaktif tidak tercentang + ditandai.
  const cats = [...container.querySelectorAll('fieldset[data-category]')].map((f) => f.getAttribute('data-category'));
  expect(cats).toEqual(['Kerupuk', 'Tepung']);
  const maizena = container.querySelector('#pl-p-4');
  expect(maizena.checked).toBe(false);
  expect(maizena.closest('label').querySelector('[data-inactive]').textContent).toBe('Nonaktif');
  expect(container.querySelector('#pl-p-1').checked).toBe(true);
  expect(container.querySelector('#pl-p-1').closest('label').textContent).toContain('Rp 45.000');

  const imgs = await waitPreview();
  expect(imgs).toHaveLength(1);
  expect(imgs[0].getAttribute('alt')).toBe('Pratinjau pricelist halaman 1 dari 1');
  expect(imgs[0].getAttribute('width')).toBe('1080');
  expect(imgs[0].className).toContain('w-full'); // skala turun lewat CSS
  // Isi kanvas: judul, kategori, nama, harga, footer WA.
  // Footer: alamat web pemesanan (bawaan = origin halaman, jsdom: http://localhost) tanpa skema, tanpa nomor WA.
  ['Daftar Harga Mihan Store', 'Kerupuk', 'Kerupuk Aci', 'Rp 25.000', 'Tepung Beras', 'Pesan online: localhost', 'Mihan Store'].forEach((t) =>
    expect(drawn).toContain(t)
  );
  expect(drawn.join('\n')).not.toMatch(/WhatsApp|\+62|812|Pemesanan/);
  expect(byLabel('Alamat web pemesanan').value).toBe(window.location.origin);
  // Pengaturan toko (store_whatsapp) tidak lagi dibaca halaman Pricelist.
  expect(mockState.calls).not.toContain('/settings');
  expect(drawn).not.toContain('Tepung Maizena');
  expect(drawn.some((t) => t.startsWith('Halaman'))).toBe(false);

  expect(button('Bagikan ke WhatsApp').disabled).toBe(false);
  expect(button('Unduh PNG').disabled).toBe(false);
  expect(button('Unduh halaman 1')).toBeTruthy();
  expect(button('Salin teks pendamping')).toBeTruthy();
  const text = container.querySelector('#pl-share-text').value;
  expect(text).toContain('Daftar Harga Mihan Store');
  expect(text).toContain(`Pesan online di ${window.location.origin}`);
  expect(text).not.toMatch(/wa\.me|\+62|6281234567890/);

  // Unduh: nama file berpola tanggal.
  await click(button('Unduh PNG'));
  expect(downloads).toHaveLength(1);
  expect(downloads[0]).toMatch(/^pricelist-mihan-store-\d{8}-1\.png$/);
});

test('produk nonaktif ikut bila dicentang manual dan diberi peringatan; Kosongkan menonaktifkan tombol', async () => {
  await renderAt('/admin/pricelist');
  await waitPreview();
  await click(container.querySelector('#pl-p-4'));
  await waitPreview();
  expect(container.querySelector('[data-testid="pl-inactive-warning"]').textContent).toContain('1 produk nonaktif');
  expect(drawn).toContain('Tepung Maizena');

  await click(button('Kosongkan'));
  await flush(300);
  expect(container.textContent).toContain('Belum ada produk dipilih');
  expect(button('Bagikan ke WhatsApp').disabled).toBe(true);
  expect(button('Unduh PNG').disabled).toBe(true);
  await click(button('Pilih semua'));
  await waitPreview();
  expect(container.querySelector('#pl-p-4').checked).toBe(false);
  expect(container.querySelector('#pl-p-1').checked).toBe(true);
});

test('fallback komputer (tanpa Web Share): unduh PNG lalu buka wa.me dengan teks pendamping + petunjuk', async () => {
  await renderAt('/admin/pricelist');
  await waitPreview();
  expect(container.querySelector('[data-testid="pl-share-hint"]').textContent).toContain('WhatsApp Web tidak bisa menerima gambar dari tombol web');
  await click(button('Bagikan ke WhatsApp'));
  expect(downloads).toHaveLength(1);
  expect(opened).toHaveLength(1);
  expect(opened[0][0].startsWith('https://wa.me/?text=')).toBe(true);
  expect(decodeURIComponent(opened[0][0].split('text=')[1])).toBe(container.querySelector('#pl-share-text').value);
  expect(opened[0][1]).toBe('_blank');
  expect(container.querySelector('[data-testid="pl-fallback"]').textContent).toContain('lampirkan file PNG yang baru diunduh');
});

test('Web Share API (HP): file PNG + teks dibagikan; batal tanpa pesan error; galat dengan pesan ramah', async () => {
  const shared = [];
  let mode = 'ok';
  navigator.canShare = (data) => Array.isArray(data.files) && data.files.length > 0;
  navigator.share = (data) => {
    shared.push(data);
    if (mode === 'abort') return Promise.reject(Object.assign(new Error('batal'), { name: 'AbortError' }));
    if (mode === 'fail') return Promise.reject(Object.assign(new Error('x'), { name: 'NotAllowedError' }));
    return Promise.resolve();
  };
  await renderAt('/admin/pricelist');
  await waitPreview();
  expect(container.querySelector('[data-testid="pl-share-hint"]').textContent).toContain('gambar langsung terlampir');

  await click(button('Bagikan ke WhatsApp'));
  expect(shared).toHaveLength(1);
  expect(shared[0].files).toHaveLength(1);
  expect(shared[0].files[0].name).toMatch(/^pricelist-mihan-store-\d{8}-1\.png$/);
  expect(shared[0].files[0].type).toBe('image/png');
  expect(shared[0].text).toContain('Daftar Harga Mihan Store');
  expect(opened).toHaveLength(0);
  expect(downloads).toHaveLength(0);

  mode = 'abort';
  await click(button('Bagikan ke WhatsApp'));
  expect(container.querySelector('[role="alert"]')).toBeNull();

  mode = 'fail';
  await click(button('Bagikan ke WhatsApp'));
  expect(container.querySelector('[role="alert"]').textContent).toContain('tidak bisa dibagikan langsung');
});

const setValue = async (el, value) => {
  const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
  await act(async () => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
};

test('alamat web pemesanan: dipakai di footer (tanpa https://) dan teks pendamping; tidak valid -> tombol nonaktif', async () => {
  await renderAt('/admin/pricelist');
  await waitPreview();
  const input = container.querySelector('#pl-url');
  expect(input.getAttribute('type')).toBe('url');
  await setValue(input, 'https://store.mihan.web.id/');
  await waitPreview();
  expect(drawn).toContain('Pesan online: store.mihan.web.id');
  expect(drawn.some((t) => t.includes('https://'))).toBe(false);
  expect(container.querySelector('#pl-share-text').value).toContain('\n\nPesan online di https://store.mihan.web.id');
  expect(container.querySelector('[data-testid="pl-url-help"]').textContent).toContain('store.mihan.web.id');
  expect(input.getAttribute('aria-invalid')).toBe('false');

  // Tidak valid (skema lain / spasi): pesan galat, unduh & bagikan nonaktif, teks tetap memakai alamat valid terakhir.
  for (const bad of ['ftp://store.mihan.web.id', 'https://store mihan.web.id', `${'javascript'}:alert(1)`]) {
    // eslint-disable-next-line no-await-in-loop
    await setValue(input, bad);
    expect(input.getAttribute('aria-invalid')).toBe('true');
    expect(container.querySelector('[data-testid="pl-url-help"]').textContent).toContain('Alamat tidak valid');
    expect(container.querySelector('[data-testid="pl-url-blocked"]')).not.toBeNull();
    expect(button('Bagikan ke WhatsApp').disabled).toBe(true);
    expect(button('Unduh PNG').disabled).toBe(true);
    expect(button('Unduh halaman 1').disabled).toBe(true);
    expect(container.querySelector('#pl-share-text').value).toContain('Pesan online di https://store.mihan.web.id');
  }

  // Diperbaiki (tanpa skema dianggap https) -> aktif lagi.
  await setValue(input, 'store.mihan.web.id');
  await waitPreview();
  expect(input.getAttribute('aria-invalid')).toBe('false');
  expect(container.querySelector('[data-testid="pl-url-blocked"]')).toBeNull();
  expect(button('Bagikan ke WhatsApp').disabled).toBe(false);
  expect(drawn).toContain('Pesan online: store.mihan.web.id');
});

test('fallback komputer membuka wa.me tanpa nomor tujuan, teks berisi alamat web', async () => {
  mockState.settings = { store_whatsapp: '081234567890' };
  await renderAt('/admin/pricelist');
  await waitPreview();
  await click(button('Bagikan ke WhatsApp'));
  expect(opened).toHaveLength(1);
  expect(opened[0][0]).toMatch(/^https:\/\/wa\.me\/\?text=/);
  const sent = decodeURIComponent(opened[0][0].split('text=')[1]);
  expect(sent).toContain(`Pesan online di ${window.location.origin}`);
  expect(sent).not.toMatch(/wa\.me|6281234567890|\+62/);
});

test('produk dibaca dari semua halaman API (per_page 100) dan dipecah jadi beberapa gambar bila panjang', async () => {
  mockState.products = Array.from({ length: 130 }, (_, i) => ({
    id: 100 + i,
    name: `Produk ${String(i).padStart(3, '0')}`,
    categoryId: (i % 2) + 1,
    categoryName: i % 2 ? 'Tepung' : 'Kerupuk',
    price: 1000 * (i + 1),
    isActive: true,
  }));
  await renderAt('/admin/pricelist');
  const pageCalls = mockState.calls.filter((c) => c.startsWith('/products'));
  expect(pageCalls).toEqual(['/products?page=1&per_page=100', '/products?page=2&per_page=100']);
  const imgs = await waitPreview();
  expect(imgs.length).toBeGreaterThan(1);
  expect(drawn).toContain(`Halaman 1/${imgs.length}`);
  expect(drawn.some((t) => t.endsWith('(lanjutan)'))).toBe(true);
  expect(button('Unduh PNG').textContent).toContain(`${imgs.length} file`);
});

test('tampilan harga: Eceran + grosir (default), Eceran saja, Grosir saja; satuan di samping harga', async () => {
  mockState.products = [
    {
      id: 1,
      name: 'Kerupuk Finna Udang',
      categoryId: 1,
      categoryName: 'Kerupuk',
      price: 45000,
      unit: 'pak',
      isActive: true,
      tiers: [
        { minQty: 10, type: 'fixed', value: 42000, unitPrice: 42000 },
        { minQty: 50, type: 'percent', value: 10, unitPrice: 40500 },
      ],
    },
    { id: 3, name: 'Tepung Beras', categoryId: 2, categoryName: 'Tepung', price: 32000, unit: 'kg', isActive: true, tiers: [] },
  ];
  await renderAt('/admin/pricelist');
  await waitPreview();
  const modes = [...container.querySelectorAll('input[name="pl-price-mode"]')];
  expect(modes.map((m) => m.value)).toEqual(['both', 'retail', 'wholesale']);
  expect(container.querySelector('input[name="pl-price-mode"]:checked').value).toBe('both');
  expect(container.querySelector('[data-testid="pl-mode-help"]').textContent).toContain('1 produk terpilih punya harga grosir');
  expect(container.textContent).toContain('Grosir (2 jenjang)');
  const tierText = '10+ : Rp 42.000 · 50+ : Rp 40.500';
  expect(drawn).toContain(tierText);
  expect(drawn).toContain('Rp 45.000');
  expect(drawn).toContain(' / pak');
  expect(drawn).toContain(' / kg');

  drawn = [];
  await click(modes[1]);
  await waitPreview();
  expect(drawn).not.toContain(tierText);
  expect(drawn).toContain('Rp 45.000');

  drawn = [];
  await click(modes[2]);
  await waitPreview();
  expect(drawn).toContain(tierText);
  expect(drawn).toContain('per pak');
  expect(drawn).not.toContain('Rp 45.000'); // eceran produk berjenjang disembunyikan
  expect(drawn).toContain('Rp 32.000'); // produk tanpa jenjang tetap seperti biasa
});
