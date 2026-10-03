// Tes opsi "Tampilkan foto produk" di Pricelist PNG: bawaan mati (tata letak lama identik), menyala menambah
// thumbnail persegi di kiri baris (tinggi baris menyesuaikan, pemecahan halaman tetap benar, kategori tidak
// terpotong), placeholder untuk produk tanpa foto/gagal dimuat, dan pemuatan foto dengan batas waktu.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';
import { groupProducts, toItem } from '../pricelist/layout';
import { layoutPricelist, renderPricelist, loadPhotos, MAX_HEIGHT, PHOTO_SIZE } from '../pricelist/render';

const mockState = { products: [] };

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
      if (path === '/me') return Promise.resolve({ user: { email: 'pemilik@example.com' } });
      if (path === '/categories') return Promise.resolve({ items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk', sortOrder: 10 }] });
      if (path.startsWith('/products')) return Promise.resolve({ items: mockState.products, total: mockState.products.length });
      return Promise.resolve({});
    },
  };
});

const App = require('../App').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

let drawLog;
const fakeCtx = () => {
  const ctx = {
    font: '',
    fillStyle: '',
    textAlign: 'left',
    textBaseline: 'alphabetic',
    measureText: (s) => ({ width: String(s).length * 16 }),
    fillText: (s, x) => drawLog.push(['text', String(s), x]),
    drawImage: (img, ...a) => drawLog.push(['image', img && img.src, ...a]),
    clip: () => drawLog.push(['clip']),
    save: () => {},
    restore: () => {},
  };
  ['fillRect', 'beginPath', 'moveTo', 'arcTo', 'closePath', 'fill', 'stroke'].forEach((m) => {
    ctx[m] = () => {};
  });
  return ctx;
};

const cats = [
  { id: 1, name: 'Kerupuk', sortOrder: 10 },
  { id: 2, name: 'Tepung', sortOrder: 20 },
];
const prod = (id, name, categoryId, thumb = '') => ({ id, name, categoryId, price: 10000 + id, isActive: true, thumb, unit: 'pcs', tiers: [] });
const products = [
  prod(1, 'Kerupuk Udang', 1, '/uploads/products/1/a_t.jpg'),
  prod(2, 'Kerupuk Bawang', 1),
  prod(3, 'Tepung Beras', 2, '/uploads/products/3/c_t.jpg'),
];
const opts = (extra = {}) => ({ groups: groupProducts(products, cats), columns: 1, title: 'Daftar', dateText: 'Berlaku', orderUrl: 'https://store.mihan.web.id', ...extra });
const cellsOf = (L) => L.pages.flatMap((p) => p.blocks.flatMap((b) => b.rows.flatMap((r) => r.cells)));

beforeEach(() => {
  drawLog = [];
});

test('toItem membawa thumb same-origin saja', () => {
  expect(toItem({ id: 1, thumb: '/uploads/products/1/a_t.jpg' }).thumb).toBe('/uploads/products/1/a_t.jpg');
  expect(toItem({ id: 1, thumb: 'https://cdn.example/x.jpg' }).thumb).toBe('');
  expect(toItem({ id: 1, thumb: '//jahat.example/x.jpg' }).thumb).toBe('');
  expect(toItem({ id: 1 }).thumb).toBe('');
});

test('bawaan mati: tata letak identik dengan sebelumnya (showPhotos tidak dikirim = false)', () => {
  const mctx = fakeCtx();
  const a = layoutPricelist(opts(), mctx);
  const b = layoutPricelist(opts({ showPhotos: false }), mctx);
  expect(b.pages.map((p) => p.canvasHeight)).toEqual(a.pages.map((p) => p.canvasHeight));
  expect(cellsOf(b).map((c) => [c.h, c.lines])).toEqual(cellsOf(a).map((c) => [c.h, c.lines]));
  expect(a.showPhotos).toBe(false);
  expect(a.photo).toBe(0);
});

test('menyala: tinggi baris >= foto + padding, nama bergeser, 1 dan 2 kolom', () => {
  const mctx = fakeCtx();
  [1, 2].forEach((columns) => {
    const off = layoutPricelist(opts({ columns }), mctx);
    const on = layoutPricelist(opts({ columns, showPhotos: true }), mctx);
    expect(on.photo).toBe(PHOTO_SIZE[columns]);
    cellsOf(on).forEach((c, i) => {
      expect(c.h).toBeGreaterThanOrEqual(on.photo + 40);
      expect(c.h).toBeGreaterThanOrEqual(cellsOf(off)[i].h);
    });
    expect(on.pages[0].canvasHeight).toBeGreaterThan(off.pages[0].canvasHeight);
  });
});

test('menyala: pemecahan halaman tetap benar, kategori tidak terpotong bila muat satu halaman', () => {
  const mctx = fakeCtx();
  const many = [];
  for (let i = 1; i <= 60; i += 1) many.push(prod(i, `Produk ${String(i).padStart(2, '0')}`, i <= 12 ? 1 : 2, i % 2 ? `/uploads/products/${i}/x_t.jpg` : ''));
  const L = layoutPricelist({ ...opts({ showPhotos: true }), groups: groupProducts(many, cats) }, mctx);
  expect(L.pages.length).toBeGreaterThan(1);
  L.pages.forEach((p) => expect(p.canvasHeight).toBeLessThanOrEqual(MAX_HEIGHT));
  // Kategori Kerupuk (12 produk) muat satu halaman -> tidak pernah "lanjutan".
  const kerupuk = L.pages.flatMap((p) => p.blocks).filter((b) => b.name === 'Kerupuk');
  expect(kerupuk).toHaveLength(1);
  expect(kerupuk[0].continued).toBe(false);
  // Semua produk tetap tercantum tepat sekali.
  expect(cellsOf(L).map((c) => c.id).sort((a, b) => a - b)).toEqual(many.map((p) => p.id));
  // Kategori Tepung yang lebih tinggi dari satu halaman dipecah dengan judul "(lanjutan)".
  const tepung = L.pages.flatMap((p) => p.blocks).filter((b) => b.name === 'Tepung');
  expect(tepung.length).toBeGreaterThan(1);
  expect(tepung.slice(1).every((b) => b.continued)).toBe(true);
});

test('render: foto dimuat digambar (dipotong tengah), produk tanpa foto/gagal -> placeholder, teks bergeser', () => {
  const photoImg = { src: '/uploads/products/1/a_t.jpg', naturalWidth: 400, naturalHeight: 300 };
  const photos = new Map([
    [1, photoImg],
    [2, null],
    [3, null], // gagal dimuat -> placeholder
  ]);
  const canvases = [];
  const createCanvas = () => {
    const c = { width: 0, height: 0, ctx: fakeCtx(), getContext() { return this.ctx; } };
    canvases.push(c);
    return c;
  };
  renderPricelist(opts({ showPhotos: true }), { createCanvas, photos });
  const images = drawLog.filter((d) => d[0] === 'image');
  expect(images).toHaveLength(1);
  // drawImage(img, sx, sy, sw, sh, dx, dy, 96, 96): potong tengah 300x300 dari 400x300.
  expect(images[0].slice(2)).toEqual([50, 0, 300, 300, 56 + 20, expect.any(Number), 96, 96]);
  const name = drawLog.find((d) => d[0] === 'text' && d[1] === 'Kerupuk Udang');
  expect(name[2]).toBe(56 + 20 + 96 + 18);

  drawLog = [];
  renderPricelist(opts(), { createCanvas, photos });
  expect(drawLog.filter((d) => d[0] === 'image')).toHaveLength(0); // opsi mati: tidak ada foto
  expect(drawLog.find((d) => d[0] === 'text' && d[1] === 'Kerupuk Udang')[2]).toBe(56 + 20);
});

test('loadPhotos: same-origin saja, gagal/timeout -> null, cache dipakai ulang', async () => {
  const saved = window.Image;
  const requested = [];
  window.Image = class {
    set src(v) {
      requested.push(v);
      this.s = v;
      if (v.includes('ok')) {
        this.naturalWidth = 400;
        this.naturalHeight = 400;
        setTimeout(() => this.onload && this.onload(), 0);
      } else if (v.includes('rusak')) setTimeout(() => this.onerror && this.onerror(), 0);
      // "lambat": tidak pernah selesai -> batas waktu
    }

    get src() {
      return this.s;
    }
  };
  const cache = new Map();
  const items = [
    { id: 1, thumb: '/uploads/ok_t.jpg' },
    { id: 2, thumb: '/uploads/rusak_t.jpg' },
    { id: 3, thumb: '/uploads/lambat_t.jpg' },
    { id: 4, thumb: '' },
    { id: 5, thumb: 'https://lain.example/ok.jpg' },
  ];
  const m = await loadPhotos(items, { timeoutMs: 30, cache });
  expect(m.get(1).naturalWidth).toBe(400);
  expect(m.get(2)).toBeNull();
  expect(m.get(3)).toBeNull();
  expect(m.get(4)).toBeNull();
  expect(m.get(5)).toBeNull();
  expect(requested).toEqual(['/uploads/ok_t.jpg', '/uploads/rusak_t.jpg', '/uploads/lambat_t.jpg']);
  await loadPhotos(items.slice(0, 1), { cache });
  expect(requested).toHaveLength(3); // dari cache
  window.Image = saved;
});

describe('halaman Pricelist', () => {
  let container;
  let root;
  const flush = async (ms = 0) => {
    await act(async () => {
      await new Promise((r) => setTimeout(r, ms));
    });
  };
  const savedImage = window.Image;
  let requested;
  beforeEach(() => {
    requested = [];
    mockState.products = [
      { id: 1, name: 'Kerupuk Udang', categoryId: 1, categoryName: 'Kerupuk', price: 45000, isActive: true, thumb: '/uploads/products/1/a_t.jpg' },
      { id: 2, name: 'Kerupuk Aci', categoryId: 1, categoryName: 'Kerupuk', price: 25000, isActive: true, thumb: '' },
    ];
    HTMLCanvasElement.prototype.getContext = function getContext() {
      if (!this.mockCtx) this.mockCtx = fakeCtx();
      return this.mockCtx;
    };
    HTMLCanvasElement.prototype.toBlob = function toBlob(cb) {
      setTimeout(() => cb(new Blob([`png-${this.width}x${this.height}`], { type: 'image/png' })), 0);
    };
    URL.createObjectURL = () => 'blob:mock/x';
    URL.revokeObjectURL = () => {};
    window.Image = class {
      set src(v) {
        requested.push(v);
        this.s = v;
        if (v.startsWith('/uploads/')) {
          this.naturalWidth = 400;
          this.naturalHeight = 300;
          setTimeout(() => this.onload && this.onload(), 0);
        } else setTimeout(() => this.onerror && this.onerror(), 0);
      }

      get src() {
        return this.s;
      }
    };
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    window.Image = savedImage;
  });

  test('opsi "Tampilkan foto produk" bawaan mati; menyala memuat thumbnail lalu menggambar ulang', async () => {
    window.history.replaceState({}, '', '/admin/pricelist');
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
    await act(async () => {
      root.render(<App />);
    });
    for (let i = 0; i < 10; i += 1) await flush(60); // eslint-disable-line no-await-in-loop
    const cb = container.querySelector('[data-testid="pl-photos"]');
    expect(cb.checked).toBe(false);
    expect(container.textContent).toContain('Tampilkan foto produk');
    expect(requested.filter((s) => s.startsWith('/uploads/'))).toHaveLength(0);
    const heightOff = container.querySelector('[data-testid="pl-previews"] img').getAttribute('height');
    await act(async () => cb.click());
    for (let i = 0; i < 10; i += 1) await flush(60); // eslint-disable-line no-await-in-loop
    expect(cb.checked).toBe(true);
    expect(requested).toContain('/uploads/products/1/a_t.jpg');
    expect(requested.filter((s) => s.startsWith('/uploads/'))).toHaveLength(1); // produk tanpa foto tidak dimuat
    const heightOn = container.querySelector('[data-testid="pl-previews"] img').getAttribute('height');
    expect(Number(heightOn)).toBeGreaterThan(Number(heightOff));
  });
});
