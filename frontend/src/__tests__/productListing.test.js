// Tes render (jsdom) daftar produk toko di HP: grid 2 kolom (< 640px) tanpa mengubah kelas tampilan >= 640px,
// tombol "Tambah" dengan ikon keranjang di kiri, lencana Grosir ringkas, dan placeholder gambar.
// Catatan: jsdom tidak menghitung media query/Tailwind, jadi tes memeriksa kelas; tampilan diukur di browser.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

const mockState = { calls: [], products: [], cart: null };

jest.mock('axios', () => {
  const respond = (method, url, body) => {
    mockState.calls.push({ method, url, body });
    if (url.endsWith('/auth/me')) return { data: { user: { name: 'Budi', role: 'customer' } } };
    if (url.endsWith('/api/products')) return { data: mockState.products };
    if (url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart') || url.includes('/api/cart/items')) return { data: mockState.cart };
    return { data: {} };
  };
  const wrap = (fn) => (...a) => {
    try {
      return Promise.resolve(fn(...a));
    } catch (e) {
      return Promise.reject(e);
    }
  };
  return {
    __esModule: true,
    default: {
      get: wrap((url) => respond('get', url)),
      post: wrap((url, body) => respond('post', url, body)),
      put: wrap((url, body) => respond('put', url, body)),
      delete: wrap((url) => respond('delete', url)),
    },
  };
});
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });

const AppModule = require('../App');

const App = AppModule.default;
const { ProductImage, productImageUrl } = AppModule;

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
const renderEl = async (el) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
  await act(async () => {
    root.render(el);
  });
};
const USER = { name: 'Budi', role: 'customer', phone: '+6281234567890' };
const classes = (el) => el.getAttribute('class').split(/\s+/).filter(Boolean);
// Kelas yang berlaku di >= 640px: semua kelas kecuali penyesuaian HP (`max-sm:`) dan yang disembunyikan di sm.
const desktopClasses = (el) => classes(el).filter((c) => !c.startsWith('max-sm:'));

const PRODUCTS = [
  {
    id: 7,
    name: 'Kerupuk Grosir',
    category: 'kerupuk',
    price: 45000,
    description: 'Gurih',
    image: 'kerupuk1.jpg',
    unit: 'pak',
    tiers: [
      { minQty: 10, type: 'fixed', value: 42000, unitPrice: 42000 },
      { minQty: 50, type: 'percent', value: 10, unitPrice: 40500 },
    ],
  },
  {
    id: 8,
    name: 'Kerupuk Udang Super Renyah Kemasan Ekonomis Untuk Warung Dan Rumah Makan Isi Banyak Sekali',
    category: 'sendok_plastik',
    price: 1000000,
    description: 'Deskripsi panjang',
    image: '',
    unit: 'bungkus kecil isi 10',
    tiers: [],
  },
];

beforeEach(() => {
  mockState.calls = [];
  mockState.products = PRODUCTS;
  mockState.cart = { items: [], subtotal: 0, itemCount: 0, lineCount: 0, hasUnavailable: false, maxQty: 999, maxLines: 50, savings: 0 };
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

test('grid produk: 2 kolom di HP, kolom & jarak >= 640px tetap (sm:2, lg:3, xl:4, gap-6)', async () => {
  await renderAt('/');
  const grid = container.querySelector('[data-testid="product-grid"]');
  const c = classes(grid);
  expect(c).toEqual(expect.arrayContaining(['grid', 'grid-cols-2', 'sm:grid-cols-2', 'lg:grid-cols-3', 'xl:grid-cols-4', 'gap-6', 'mt-8', 'max-sm:gap-2']));
  expect(c).not.toContain('grid-cols-1');
  // Semua kelas tambahan hanya untuk HP (max-sm:), kecuali grid-cols-2 dasar yang di >= 640px ditimpa sm:grid-cols-2.
  expect(desktopClasses(grid).sort()).toEqual(['grid', 'grid-cols-2', 'sm:grid-cols-2', 'lg:grid-cols-3', 'xl:grid-cols-4', 'gap-6', 'mt-8'].sort());
  expect(grid.querySelectorAll('[data-testid="product-card"]')).toHaveLength(2);
});

test('kartu: kelas >= 640px sama seperti sebelumnya; HP: tinggi penuh, nama maks. 2 baris, kategori 1 baris', async () => {
  await renderAt('/');
  const card = container.querySelector('[data-testid="product-card"]');
  expect(desktopClasses(card).sort()).toEqual(
    'bg-white rounded-xl shadow-lg p-6 hover:shadow-2xl hover:-translate-y-2 transition-all duration-300 flex flex-col'.split(' ').sort()
  );
  expect(classes(card)).toEqual(expect.arrayContaining(['max-sm:h-full', 'max-sm:min-w-0', 'max-sm:p-2']));
  const h3 = card.querySelector('h3');
  expect(desktopClasses(h3).sort()).toEqual('text-lg font-semibold text-gray-800 mb-2'.split(' ').sort());
  expect(classes(h3)).toEqual(expect.arrayContaining(['max-sm:line-clamp-2', 'max-sm:text-[13px]', 'max-sm:break-words']));
  const cat = h3.nextElementSibling;
  expect(classes(cat)).toEqual(expect.arrayContaining(['max-sm:truncate', 'max-sm:!text-[11px]']));
  // Harga tebal ±15px, satuan kecil.
  const price = cat.nextElementSibling;
  expect(price.textContent).toBe('Rp 45.000/ pak');
  expect(classes(price)).toEqual(expect.arrayContaining(['font-bold', 'max-sm:text-[15px]']));
  expect(desktopClasses(price).sort()).toEqual('text-2xl font-bold text-purple-700 mt-2'.split(' ').sort());
  // Nama panjang tetap utuh di DOM (dipotong visual oleh line-clamp) dan tersedia di title.
  const long = container.querySelectorAll('[data-testid="product-card"]')[1].querySelector('h3');
  expect(long.getAttribute('title')).toBe(PRODUCTS[1].name);
  expect(container.textContent).toContain('Rp 1.000.000');
  expect(container.textContent).toContain('/ bungkus kecil isi 10');
});

test('tombol HP: ikon keranjang di kiri, "Tambah" di kanan, " ke keranjang" hanya tersembunyi visual; aria-label lengkap', async () => {
  await renderAt('/');
  const button = container.querySelector('[data-testid="product-card"] [data-testid="add-to-cart"]');
  expect(button.getAttribute('aria-label')).toBe('Tambah Kerupuk Grosir ke keranjang');
  expect(button.textContent).toBe('Tambah ke keranjang'); // teks lama tetap (tes & >= 640px)
  const kids = [...button.children];
  expect(kids[0].tagName.toLowerCase()).toBe('svg');
  expect(kids[0].getAttribute('aria-hidden')).toBe('true');
  expect(kids[0].getAttribute('data-testid')).toBe('add-to-cart-icon');
  expect(classes(kids[0])).toContain('sm:hidden'); // ikon hanya di HP
  // Teks lama: satu simpul teks (>= 640px sama persis), di HP hanya tersembunyi visual.
  expect(kids).toHaveLength(2);
  expect(kids[1].textContent).toBe('Tambah ke keranjang');
  expect(kids[1].childNodes).toHaveLength(1);
  expect(classes(kids[1])).toEqual(['max-sm:sr-only']);
  // HP: label "Tambah" dari ::after (setelah ikon -> di kanan), flex baris, lebar penuh; tinggi min. 40px dari index.css.
  expect(classes(button)).toContain("max-sm:after:content-['Tambah']");
  expect(classes(button)).toEqual(expect.arrayContaining(['w-full', 'max-sm:flex', 'max-sm:items-center', 'max-sm:justify-center']));
  expect(classes(button)).not.toContain('max-sm:flex-row-reverse');
  expect(classes(button).filter((c) => /^(sm:|md:|lg:|xl:)?(order-|flex-row-reverse)/.test(c))).toEqual([]);
  expect(desktopClasses(button)).toEqual(expect.arrayContaining(['w-full', 'py-2', 'rounded-lg', 'font-semibold', 'bg-purple-700']));
});

test('tombol: belum login -> ke /login lalu kembali ke toko', async () => {
  await renderAt('/');
  await act(async () => container.querySelector('button[aria-label="Tambah Kerupuk Grosir ke keranjang"]').click());
  expect(window.location.pathname).toBe('/login');
  expect(sessionStorage.getItem('returnTo')).toBe('/');
});

test('tombol: login -> memanggil API dan umpan balik "Ditambahkan ✓" dengan ikon tetap di kiri', async () => {
  await renderAt('/', USER);
  const button = container.querySelector('button[aria-label="Tambah Kerupuk Grosir ke keranjang"]');
  await act(async () => button.click());
  await flush();
  const post = mockState.calls.find((c) => c.method === 'post' && c.url.endsWith('/cart/items'));
  expect(post.body).toEqual({ productId: 7, qty: 1 });
  expect(button.textContent).toBe('Ditambahkan ✓');
  expect(button.firstElementChild.tagName.toLowerCase()).toBe('svg');
  expect(classes(button)).not.toContain("max-sm:after:content-['Tambah']"); // label pendek hanya saat siap
  expect(button.getAttribute('aria-label')).toBe('Tambah Kerupuk Grosir ke keranjang');
  expect(container.textContent).toContain('Kerupuk Grosir ditambahkan ke keranjang');
});

test('lencana Grosir tetap ada, ringkas di HP, dan membuka daftar jenjang', async () => {
  await renderAt('/');
  const badges = container.querySelectorAll('[data-testid="tier-badge"]');
  expect(badges).toHaveLength(1); // produk tanpa jenjang tidak punya lencana
  const badge = badges[0];
  expect(badge.textContent).toContain('Grosir · mulai Rp 40.500 / pak');
  expect(badge.textContent.startsWith('Grosir')).toBe(true);
  // >= 640px: teks lengkap (tersembunyi visual di HP); HP: label ringkas aria-hidden, harga tidak dipotong.
  expect(classes(badge.firstElementChild)).toEqual(['max-sm:sr-only']);
  expect(badge.firstElementChild.textContent).toBe('Grosir · mulai Rp 40.500 / pak');
  const short = badge.querySelector('[data-testid="tier-badge-short"]');
  expect(short.textContent).toBe('Grosir · mulai Rp 40.500');
  expect(short.getAttribute('aria-hidden')).toBe('true');
  expect(classes(short)).toContain('sm:hidden');
  expect(short.querySelector('.whitespace-nowrap').textContent).toBe('Rp 40.500');
  expect(classes(badge)).toEqual(expect.arrayContaining(['max-sm:!text-[11px]', 'max-sm:w-full']));
  expect(desktopClasses(badge).sort()).toEqual(
    'inline-flex items-center gap-1 rounded-full border border-amber-300 bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-900 hover:bg-amber-200'.split(' ').sort()
  );
  await act(async () => badge.click());
  const list = container.querySelector('[data-testid="tier-list"]');
  expect(list.textContent).toContain('Beli 10+ pak');
  expect(list.textContent).toContain('Rp 42.000 / pak');
  expect(classes(list.querySelector('li'))).toContain('max-sm:flex-wrap');
});

test('gambar: nama berkas tanpa URL -> placeholder tanpa <img>; URL gagal dimuat -> placeholder', async () => {
  expect(productImageUrl({ image: 'kerupuk1.jpg' })).toBeNull();
  expect(productImageUrl({ image: '' })).toBeNull();
  expect(productImageUrl({ image: 'https://cdn.example.com/a.jpg' })).toBe('https://cdn.example.com/a.jpg');
  expect(productImageUrl({ image: '/media/a.jpg' })).toBe('/media/a.jpg');
  expect(productImageUrl({ image: '//evil.example/a.jpg' })).toBeNull();

  await renderEl(<ProductImage product={{ id: 1, name: 'Saos', image: 'https://cdn.example.com/saos.jpg' }} />);
  const img = container.querySelector('img');
  expect(img.getAttribute('src')).toBe('https://cdn.example.com/saos.jpg');
  expect(img.getAttribute('alt')).toBe('Saos');
  expect(classes(img)).toContain('object-cover');
  expect(container.querySelector('[data-testid="product-image-placeholder"]')).toBeNull();
  await act(async () => {
    img.dispatchEvent(new Event('error'));
  });
  expect(container.querySelector('img')).toBeNull();
  const ph = container.querySelector('[data-testid="product-image-placeholder"]');
  expect(ph).not.toBeNull();
  expect(ph.getAttribute('aria-label')).toBe('Gambar Saos belum tersedia');
  // Area gambar: >= 640px h-48 seperti sebelumnya; HP rasio 4/3.
  expect(classes(ph.parentElement)).toEqual(expect.arrayContaining(['h-48', 'max-sm:aspect-[4/3]', 'max-sm:h-auto']));
});

test('kartu toko: produk tanpa gambar menampilkan placeholder', async () => {
  await renderAt('/');
  expect(container.querySelectorAll('[data-testid="product-card"] img')).toHaveLength(0);
  expect(container.querySelectorAll('[data-testid="product-image-placeholder"]')).toHaveLength(2);
});
