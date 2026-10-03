// Tes foto produk (jsdom): perkecilan di browser (canvas/ImageBitmap tiruan), form admin (pilih, pratinjau,
// unggah setelah simpan, produk baru, galat + coba lagi, hapus dengan konfirmasi), daftar admin (thumbnail),
// ProductImage toko (thumb, lazy, ukuran, fallback), dan keranjang (thumbnail per baris).
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { fitSize, prepareImage, MSG_TYPE, MSG_DECODE, MSG_TOO_BIG, ACCEPT_ATTR } from '../admin/imagePrep';

const mockState = { calls: [], uploads: [], uploadReplies: [], admin: {}, cart: null, products: [] };

jest.mock('axios', () => {
  const respond = (method, url) => {
    mockState.calls.push({ method, url });
    if (url.endsWith('/auth/me')) return { data: { user: { name: 'Budi', role: 'customer' } } };
    if (url.endsWith('/api/products')) return { data: mockState.products };
    if (url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart') || url.includes('/api/cart/items')) return { data: mockState.cart };
    return { data: {} };
  };
  const wrap = (fn) => (...a) => Promise.resolve(fn(...a));
  return {
    __esModule: true,
    default: {
      get: wrap((url) => respond('get', url)),
      post: wrap((url) => respond('post', url)),
      put: wrap((url) => respond('put', url)),
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
      if (opts?.method === 'POST' && path === '/products') return Promise.resolve({ id: 55, name: opts.body.name });
      if (opts?.method === 'DELETE' && path.endsWith('/image')) return Promise.resolve({ image: '', thumb: '' });
      const key = Object.keys(mockState.admin)
        .filter((k) => path.startsWith(k))
        .sort((a, b) => b.length - a.length)[0];
      return Promise.resolve(key ? mockState.admin[key] : {});
    },
    adminUpload: (path, blob, opts) => {
      mockState.uploads.push({ path, blob, filename: opts?.filename });
      if (opts?.onProgress) opts.onProgress(50);
      const next = mockState.uploadReplies.shift();
      if (next && next.error) {
        const err = new Error(next.error);
        err.status = next.status;
        return Promise.reject(err);
      }
      const id = path.split('/')[2];
      return Promise.resolve({ image: `/uploads/products/${id}/baru.jpg`, thumb: `/uploads/products/${id}/baru_t.jpg` });
    },
  };
});

const AppModule = require('../App');

const App = AppModule.default;
const { ProductImage, productImageUrl } = AppModule;

global.IS_REACT_ACT_ENVIRONMENT = true;

// ---------- canvas & ImageBitmap tiruan ----------
let bitmapCalls;
let blobSizes; // ukuran blob berurutan yang dihasilkan toBlob (default 300 KB)
let ctxLog;
const installCanvasMocks = ({ width = 4000, height = 3000, failBitmap = false } = {}) => {
  bitmapCalls = [];
  ctxLog = [];
  global.createImageBitmap = (file, opts) => {
    bitmapCalls.push({ file, opts });
    if (failBitmap) return Promise.reject(new Error('tidak bisa didekode'));
    return Promise.resolve({ width, height, close: () => ctxLog.push('close') });
  };
  HTMLCanvasElement.prototype.getContext = function getContext() {
    const canvas = this;
    return {
      fillRect: (...a) => ctxLog.push(['fillRect', ...a]),
      drawImage: (img, x, y, w, h) => ctxLog.push(['drawImage', w, h, canvas.width, canvas.height]),
      set fillStyle(v) {
        ctxLog.push(['fillStyle', v]);
      },
    };
  };
  HTMLCanvasElement.prototype.toBlob = function toBlob(cb, type, q) {
    const size = blobSizes.length ? blobSizes.shift() : 300 * 1024;
    ctxLog.push(['toBlob', type, q, this.width, this.height]);
    const b = new Blob(['x'], { type });
    Object.defineProperty(b, 'size', { value: size });
    setTimeout(() => cb(b), 0);
  };
};
const fakeFile = (type = 'image/jpeg', size = 4.5 * 1024 * 1024, name = 'IMG_2026.jpg') => {
  const f = new Blob(['x'], { type });
  Object.defineProperty(f, 'size', { value: size });
  Object.defineProperty(f, 'name', { value: name });
  return f;
};

let container;
let root;
const flush = async (ms = 0) => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
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
const renderEl = async (el) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
  await act(async () => {
    root.render(el);
  });
};
const btn = (text, scope = container) => [...scope.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};
const chooseFile = async (input, file) => {
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  await act(async () => {
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  await flush();
};

beforeEach(() => {
  mockState.calls = [];
  mockState.uploads = [];
  mockState.uploadReplies = [];
  mockState.admin = {};
  mockState.products = [];
  mockState.cart = null;
  blobSizes = [];
  let n = 0;
  URL.createObjectURL = () => {
    n += 1;
    return `blob:mock/${n}`;
  };
  URL.revokeObjectURL = () => {};
  installCanvasMocks();
});
afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
});

// ---------- perkecilan di browser ----------

describe('perkecilan foto di browser', () => {
  test('fitSize: sisi terpanjang <= 1600, rasio terjaga, tidak diperbesar', () => {
    expect(fitSize(4000, 3000)).toEqual({ width: 1600, height: 1200 });
    expect(fitSize(3000, 4000)).toEqual({ width: 1200, height: 1600 });
    expect(fitSize(800, 600)).toEqual({ width: 800, height: 600 });
    expect(fitSize(1600, 1600)).toEqual({ width: 1600, height: 1600 });
    expect(fitSize(0, 10)).toEqual({ width: 0, height: 0 });
  });

  test('foto kamera 4000x3000 (4,5 MB) -> JPEG 1600x1200 kualitas 0,85, orientasi EXIF dari gambar, latar putih', async () => {
    const out = await prepareImage(fakeFile());
    expect(out).toMatchObject({ width: 1600, height: 1200, quality: 0.85 });
    expect(out.blob.type).toBe('image/jpeg');
    expect(bitmapCalls[0].opts).toEqual({ imageOrientation: 'from-image' });
    expect(ctxLog).toContainEqual(['fillStyle', '#ffffff']);
    expect(ctxLog).toContainEqual(['drawImage', 1600, 1200, 1600, 1200]);
    expect(ctxLog).toContainEqual(['toBlob', 'image/jpeg', 0.85, 1600, 1200]);
    expect(ctxLog).toContain('close'); // ImageBitmap dilepas
  });

  test('potret (orientasi sudah diterapkan bitmap) 3024x4032 -> 1200x1600; foto kecil tidak diperbesar', async () => {
    installCanvasMocks({ width: 3024, height: 4032 });
    expect(await prepareImage(fakeFile())).toMatchObject({ width: 1200, height: 1600 });
    installCanvasMocks({ width: 640, height: 480 });
    expect(await prepareImage(fakeFile('image/png', 200000, 'logo.png'))).toMatchObject({ width: 640, height: 480 });
  });

  test('hasil > 2 MB: kualitas diturunkan bertahap, lalu ukuran diperkecil; gagal total -> pesan ramah', async () => {
    blobSizes = [3e6, 2.5e6, 1.5e6];
    const out = await prepareImage(fakeFile());
    expect(out.quality).toBe(0.65);
    expect(ctxLog.filter((c) => c[0] === 'toBlob').map((c) => c[2])).toEqual([0.85, 0.75, 0.65]);

    blobSizes = Array(20).fill(5e6);
    ctxLog = [];
    await expect(prepareImage(fakeFile())).rejects.toThrow(MSG_TOO_BIG);
    const sizes = ctxLog.filter((c) => c[0] === 'toBlob').map((c) => c[3]);
    expect(Math.min(...sizes)).toBeLessThan(1600); // ukuran ikut diperkecil
  });

  test('tipe tidak didukung dan foto rusak -> pesan ramah', async () => {
    for (const t of ['image/gif', 'image/heic', 'image/svg+xml', 'image/avif', 'text/plain', '']) {
      // eslint-disable-next-line no-await-in-loop
      await expect(prepareImage(fakeFile(t))).rejects.toThrow(MSG_TYPE);
    }
    installCanvasMocks({ failBitmap: true });
    const savedImage = window.Image;
    window.Image = class {
      set src(v) {
        setTimeout(() => this.onerror && this.onerror(new Error(v)), 0);
      }
    };
    await expect(prepareImage(fakeFile())).rejects.toThrow(MSG_DECODE);
    window.Image = savedImage;
    expect(ACCEPT_ATTR).toBe('image/jpeg,image/png,image/webp');
  });
});

// ---------- form admin ----------

const product = {
  id: 7,
  name: 'Kerupuk Foto',
  description: 'Gurih',
  categoryId: 1,
  categoryName: 'Kerupuk',
  price: 45000,
  unit: 'pak',
  imagePath: 'products/7/lama.jpg',
  image: '/uploads/products/7/lama.jpg',
  thumb: '/uploads/products/7/lama_t.jpg',
  isActive: true,
  tiers: [],
};
const adminData = () => ({
  '/products': { items: [product, { ...product, id: 8, name: 'Saos Tanpa Foto', image: '', thumb: '', imagePath: 'saos1.jpg' }], total: 2, page: 1, perPage: 20 },
  '/categories': { items: [{ id: 1, name: 'Kerupuk', slug: 'kerupuk' }] },
});
const openEdit = async (index = 0) => {
  await renderAt('/admin/products');
  await click([...container.querySelectorAll('button')].filter((b) => b.textContent.trim() === 'Ubah')[index]);
  return container.querySelector('[role="dialog"]');
};

describe('form foto produk (admin)', () => {
  test('daftar admin menampilkan thumbnail kecil; produk tanpa foto -> placeholder', async () => {
    mockState.admin = adminData();
    await renderAt('/admin/products');
    const rows = [...container.querySelectorAll('tbody tr')];
    const img = rows[0].querySelector('[data-testid="thumb-img"]');
    expect(img.getAttribute('src')).toBe('/uploads/products/7/lama_t.jpg');
    expect(img.getAttribute('width')).toBe('40');
    expect(img.getAttribute('loading')).toBe('lazy');
    expect(rows[1].querySelector('[data-testid="thumb-placeholder"]')).not.toBeNull();
    // Kolom "Path gambar" lama sudah tidak ada.
    await click(btn('Ubah'));
    expect(container.textContent).not.toContain('Path gambar');
  });

  test('ubah: pratinjau foto saat ini, pilih foto -> diperkecil + pratinjau, Simpan -> PUT lalu unggah', async () => {
    mockState.admin = adminData();
    const dialog = await openEdit();
    const field = dialog.querySelector('[data-testid="photo-field"]');
    expect(field.querySelector('[data-testid="photo-preview-current"]').getAttribute('src')).toBe('/uploads/products/7/lama_t.jpg');
    const input = field.querySelector('[data-testid="photo-input"]');
    expect(input.getAttribute('accept')).toBe('image/jpeg,image/png,image/webp');
    expect(btn('Ganti foto', field)).toBeTruthy();
    expect(btn('Hapus foto', field)).toBeTruthy();

    await chooseFile(input, fakeFile());
    const prev = field.querySelector('[data-testid="photo-preview-new"]');
    expect(prev.getAttribute('src')).toMatch(/^blob:mock\//);
    expect(field.querySelector('[data-testid="photo-pending-info"]').textContent).toContain('1600×1200 px');
    expect(field.textContent).toContain('Diunggah saat Anda menekan Simpan');
    expect(mockState.uploads).toHaveLength(0); // belum diunggah sebelum Simpan

    await click(btn('Simpan'));
    await flush();
    const put = mockState.calls.find((c) => c.method === 'PUT' && c.url === 'admin/products/7');
    expect(put).toBeTruthy();
    expect(put.body).not.toHaveProperty('imagePath'); // foto tidak lagi lewat path
    expect(mockState.uploads).toHaveLength(1);
    expect(mockState.uploads[0].path).toBe('/products/7/image');
    expect(mockState.uploads[0].blob.type).toBe('image/jpeg');
    const putIdx = mockState.calls.indexOf(put);
    expect(putIdx).toBeGreaterThanOrEqual(0);
    expect(container.querySelector('[role="dialog"]')).toBeNull(); // berhasil -> modal tertutup
  });

  test('galat server ramah ditampilkan; produk tetap tersimpan; "Coba unggah lagi" berhasil', async () => {
    mockState.admin = adminData();
    mockState.uploadReplies = [{ error: 'Format foto tidak didukung. Gunakan JPG, PNG, atau WebP.', status: 415 }];
    const dialog = await openEdit();
    await chooseFile(dialog.querySelector('[data-testid="photo-input"]'), fakeFile());
    await click(btn('Simpan'));
    await flush();
    const warn = dialog.querySelector('[data-testid="photo-upload-failed"]');
    expect(warn.textContent).toContain('Produk sudah tersimpan, tetapi foto gagal diunggah');
    expect(warn.textContent).toContain('Format foto tidak didukung');
    expect(container.querySelector('[role="dialog"]')).not.toBeNull();
    await click(btn('Coba unggah lagi'));
    await flush();
    expect(mockState.uploads).toHaveLength(2);
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });

  test('produk BARU: foto dipilih dulu, diunggah otomatis setelah POST (id baru); gagal -> tanpa POST ganda', async () => {
    mockState.admin = adminData();
    mockState.uploadReplies = [{ error: 'Penyimpanan foto penuh.', status: 507 }];
    await renderAt('/admin/products');
    await click(btn('+ Tambah produk'));
    const dialog = container.querySelector('[role="dialog"]');
    const set = (el, v) => {
      const proto = el.tagName === 'SELECT' ? window.HTMLSelectElement.prototype : el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
      el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
    };
    await act(async () => {
      set(dialog.querySelector('input[maxlength="150"]'), 'Produk Baru Berfoto');
      set(dialog.querySelector('select'), '1');
      set(dialog.querySelector('#pf-price'), '12000');
    });
    const field = dialog.querySelector('[data-testid="photo-field"]');
    expect(field.querySelector('[data-testid="photo-empty"]')).not.toBeNull();
    expect(btn('Pilih foto', field)).toBeTruthy();
    expect(btn('Hapus foto', field)).toBeUndefined();
    await chooseFile(field.querySelector('[data-testid="photo-input"]'), fakeFile('image/png', 1e6, 'a.png'));
    await click(btn('Simpan'));
    await flush();
    const posts = () => mockState.calls.filter((c) => c.method === 'POST' && c.url === 'admin/products');
    expect(posts()).toHaveLength(1);
    expect(mockState.uploads[0].path).toBe('/products/55/image');
    expect(dialog.querySelector('[data-testid="photo-upload-failed"]').textContent).toContain('Penyimpanan foto penuh');
    // Simpan lagi -> PUT ke produk yang sudah dibuat (bukan POST kedua), lalu unggah ulang.
    await click(btn('Simpan'));
    await flush();
    expect(posts()).toHaveLength(1);
    expect(mockState.calls.some((c) => c.method === 'PUT' && c.url === 'admin/products/55')).toBe(true);
    expect(mockState.uploads).toHaveLength(2);
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });

  test('hapus foto: konfirmasi dulu, lalu DELETE /products/7/image; batal tidak menghapus', async () => {
    mockState.admin = adminData();
    const dialog = await openEdit();
    const field = dialog.querySelector('[data-testid="photo-field"]');
    await click(btn('Hapus foto', field));
    expect(field.querySelector('[role="alertdialog"]').textContent).toContain('Hapus foto produk ini?');
    await click(btn('Batal', field));
    expect(mockState.calls.some((c) => c.method === 'DELETE')).toBe(false);
    await click(btn('Hapus foto', field));
    await click(btn('Ya, hapus foto', field));
    expect(mockState.calls.some((c) => c.method === 'DELETE' && c.url === 'admin/products/7/image')).toBe(true);
    expect(field.querySelector('[data-testid="photo-empty"]')).not.toBeNull();
    expect(btn('Pilih foto', field)).toBeTruthy();
  });

  test('foto tidak didukung dipilih -> pesan ramah, tidak ada unggahan', async () => {
    mockState.admin = adminData();
    const dialog = await openEdit(1);
    const field = dialog.querySelector('[data-testid="photo-field"]');
    expect(field.querySelector('[data-testid="photo-empty"]')).not.toBeNull(); // nilai lama "saos1.jpg" = tanpa foto
    expect(btn('Hapus foto', field)).toBeUndefined();
    await chooseFile(field.querySelector('[data-testid="photo-input"]'), fakeFile('image/heic', 2e6, 'IMG.HEIC'));
    expect(field.querySelector('[data-testid="photo-error"]').textContent).toBe(MSG_TYPE);
    await click(btn('Simpan'));
    expect(mockState.uploads).toHaveLength(0);
  });
});

// ---------- toko ----------

describe('ProductImage toko & keranjang', () => {
  test('memakai thumb (bukan foto utama), lazy, decoding async, width/height, object-cover; gagal -> placeholder', async () => {
    const p = { id: 3, name: 'Kerupuk', image: '/uploads/products/3/a.jpg', thumb: '/uploads/products/3/a_t.jpg' };
    expect(productImageUrl(p)).toBe('/uploads/products/3/a_t.jpg');
    expect(productImageUrl({ image: '/uploads/products/3/a.jpg', thumb: '' })).toBe('/uploads/products/3/a.jpg');
    expect(productImageUrl({ image: '', thumb: '' })).toBeNull();
    expect(productImageUrl({ thumb: '//jahat.example/x.jpg', image: ['javascript', 'alert(1)'].join(':') })).toBeNull();
    await renderEl(<ProductImage product={p} />);
    const img = container.querySelector('img');
    expect(img.getAttribute('src')).toBe('/uploads/products/3/a_t.jpg');
    expect(img.getAttribute('loading')).toBe('lazy');
    expect(img.getAttribute('decoding')).toBe('async');
    expect(img.getAttribute('width')).toBe('400');
    expect(img.getAttribute('height')).toBe('300');
    expect(img.className).toContain('object-cover');
    // Wadah tetap sama (ukuran kartu tidak berubah): h-48 desktop, rasio 4/3 di HP.
    expect(img.parentElement.className).toContain('h-48');
    expect(img.parentElement.className).toContain('max-sm:aspect-[4/3]');
    await act(async () => img.dispatchEvent(new Event('error')));
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('[data-testid="product-image-placeholder"]')).not.toBeNull();
  });

  test('kartu toko 2 kolom: produk berfoto memuat thumb, produk lama tanpa foto placeholder', async () => {
    mockState.products = [
      { id: 1, name: 'Berfoto', category: 'kerupuk', price: 1000, description: '', image: '/uploads/products/1/a.jpg', thumb: '/uploads/products/1/a_t.jpg', unit: 'pcs', tiers: [] },
      { id: 2, name: 'Lama', category: 'kerupuk', price: 1000, description: '', image: '', thumb: '', unit: 'pcs', tiers: [] },
    ];
    await renderAt('/');
    const cards = container.querySelectorAll('[data-testid="product-card"]');
    expect(cards[0].querySelector('img').getAttribute('src')).toBe('/uploads/products/1/a_t.jpg');
    expect(cards[1].querySelector('img')).toBeNull();
    expect(container.querySelector('[data-testid="product-grid"]').className).toContain('grid-cols-2');
  });

  const cartWith = (items) => ({
    items: items.map((it) => ({ available: true, qty: 1, price: 1000, unitPrice: 1000, lineTotal: 1000, unit: 'pcs', ...it })),
    subtotal: 1000 * items.length,
    itemCount: items.length,
    lineCount: items.length,
    hasUnavailable: false,
    maxQty: 999,
    maxLines: 50,
    savings: 0,
  });

  test('keranjang: thumbnail 48px per baris bila ada foto; baris tanpa foto placeholder', async () => {
    mockState.cart = cartWith([
      { productId: 1, name: 'Berfoto', image: '/uploads/products/1/a.jpg', thumb: '/uploads/products/1/a_t.jpg' },
      { productId: 2, name: 'Tanpa Foto', image: '', thumb: '' },
    ]);
    await renderAt('/keranjang', { name: 'Budi', role: 'customer' });
    const rows = container.querySelectorAll('ul > li');
    const img = rows[0].querySelector('[data-testid="thumb-img"]');
    expect(img.getAttribute('src')).toBe('/uploads/products/1/a_t.jpg');
    expect(img.getAttribute('width')).toBe('48');
    expect(img.getAttribute('alt')).toBe('Berfoto');
    expect(rows[1].querySelector('[data-testid="thumb-placeholder"]')).not.toBeNull();
    await act(async () => img.dispatchEvent(new Event('error')));
    expect(rows[0].querySelector('[data-testid="thumb-placeholder"]')).not.toBeNull();
  });

  test('keranjang tanpa foto sama sekali: tidak ada kolom foto (tampilan lama)', async () => {
    mockState.cart = cartWith([{ productId: 2, name: 'Tanpa Foto', image: '', thumb: '' }]);
    await renderAt('/keranjang', { name: 'Budi', role: 'customer' });
    expect(container.querySelector('[data-testid="thumb-img"]')).toBeNull();
    expect(container.querySelector('[data-testid="thumb-placeholder"]')).toBeNull();
  });
});
