// Tes render (jsdom) checkout dengan data wilayah: nama kosong di awal, semua field wajib kecuali catatan,
// pilihan bertingkat (memilih tingkat atas mengosongkan tingkat bawah), dimuat per induk, pencarian, galat.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { chooseRegion, fillCheckout, setNativeValue } from '../testUtils/regionFixtures';
import { clearRegionCache, filterRegions } from '../shop/regionsApi';
import { validateCheckoutForm, validPhone } from '../shop/Checkout';

const mockState = { calls: [], regionCalls: [], regionOverride: {}, orderReply: null };

jest.mock('axios', () => {
  const cart = {
    items: [{ productId: 1, name: 'Kerupuk Udang', price: 45000, unitPrice: 45000, qty: 2, lineTotal: 90000, available: true, unit: 'pcs' }],
    subtotal: 90000,
    itemCount: 2,
    lineCount: 1,
    hasUnavailable: false,
  };
  const respond = (method, url, body) => {
    mockState.calls.push({ method, url, body });
    if (url.endsWith('/auth/me')) return Promise.resolve({ data: { user: { name: 'Nama Akun', role: 'customer', phone: '+6281234567890' } } });
    if (url.endsWith('/api/cart') || url.includes('/api/cart/')) return Promise.resolve({ data: cart });
    if (method === 'post' && url.endsWith('/api/orders')) {
      if (mockState.orderReply) {
        const err = new Error('gagal');
        err.response = mockState.orderReply;
        return Promise.reject(err);
      }
      return Promise.resolve({ data: { orderNo: 'MS-261003-0001' } });
    }
    if (url.includes('/api/orders/')) return Promise.resolve({ data: null });
    if (url.endsWith('/api/products')) return Promise.resolve({ data: [] });
    return Promise.resolve({ data: {} });
  };
  return {
    __esModule: true,
    default: {
      get: (url) => require('../testUtils/regionFixtures').mockRegionGet(url, mockState.regionCalls, mockState.regionOverride) || respond('get', url),
      post: (url, body) => respond('post', url, body),
      put: (url, body) => respond('put', url, body),
      delete: (url) => respond('delete', url),
    },
  };
});
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });

const App = require('../App').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

let container;
let root;
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};
const USER = { name: 'Nama Akun', role: 'customer', phone: '+6281234567890' };

const renderCheckout = async () => {
  localStorage.clear();
  localStorage.setItem('token', 'tes');
  localStorage.setItem('user', JSON.stringify(USER));
  window.history.replaceState({}, '', '/checkout');
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
  mockState.regionCalls = [];
  mockState.regionOverride = {};
  mockState.orderReply = null;
  clearRegionCache();
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const $ = (sel) => container.querySelector(sel);
const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const orderPosts = () => mockState.calls.filter((c) => c.method === 'post' && c.url.endsWith('/api/orders'));
const click = async (el) => {
  await act(async () => {
    el.click();
  });
  await flush();
};

test('awal: nama penerima kosong, telepon dari akun, tingkat bawah nonaktif, hanya daftar provinsi yang dimuat', async () => {
  await renderCheckout();
  expect($('h1').textContent).toBe('Checkout');
  expect($('#co-name').value).toBe('');
  expect($('#co-phone').value).toBe('+6281234567890');
  expect($('#co-province').disabled).toBe(false);
  ['regency', 'district', 'village'].forEach((k) => expect($(`#co-${k}`).disabled).toBe(true));
  expect($('#co-regency').getAttribute('placeholder')).toBe('Pilih provinsi dulu');
  expect($('#co-village').getAttribute('placeholder')).toBe('Pilih kecamatan dulu');
  expect(mockState.regionCalls).toEqual(['provinces']);
  // Combobox: role & ukuran nyaman di HP (huruf 16px, tinggi >= 44px).
  const prov = $('#co-province');
  expect(prov.getAttribute('role')).toBe('combobox');
  expect(prov.className).toContain('text-base');
  expect(prov.className).toContain('min-h-[44px]');
  expect(container.textContent).toContain('Kelurahan/Desa');
  expect(container.textContent).toContain('Kode pos');
});

test('semua field wajib kecuali catatan: kirim kosong -> pesan per field, tanpa permintaan pesanan', async () => {
  await renderCheckout();
  await click(btn('Buat pesanan'));
  expect(orderPosts()).toHaveLength(0);
  const text = container.textContent;
  [
    'Nama penerima wajib diisi.',
    'Pilih provinsi.',
    'Pilih kabupaten/kota.',
    'Pilih kecamatan.',
    'Pilih kelurahan/desa.',
    'Alamat lengkap wajib diisi',
    'Kode pos wajib diisi.',
  ].forEach((m) => expect(text).toContain(m));
  expect(text).not.toContain('Catatan maksimal');
  expect($('#co-name').getAttribute('aria-invalid')).toBe('true');
  expect(document.activeElement).toBe($('#co-name')); // fokus ke field pertama yang salah
  // Kode pos 4 digit ditolak; huruf dibuang saat diketik.
  await act(async () => setNativeValue($('#co-postal'), '15a11'));
  expect($('#co-postal').value).toBe('1511');
  await click(btn('Buat pesanan'));
  expect(container.textContent).toContain('Kode pos harus 5 digit angka.');
});

test('pilihan bertingkat: dimuat per induk, memilih ulang tingkat atas mengosongkan tingkat bawah', async () => {
  await renderCheckout();
  await chooseRegion(container, 'province', 'Banten');
  expect(mockState.regionCalls).toEqual(['provinces', 'regencies/36']);
  expect($('#co-regency').disabled).toBe(false);
  expect($('#co-district').disabled).toBe(true);
  await chooseRegion(container, 'regency', 'Kota Tangerang');
  await chooseRegion(container, 'district', 'Tangerang');
  await chooseRegion(container, 'village', 'Sukarasa');
  expect($('#co-village').value).toBe('Sukarasa');
  expect(mockState.regionCalls).toEqual(['provinces', 'regencies/36', 'districts/36.71', 'villages/36.71.01']);
  // Ganti provinsi -> kab/kota, kecamatan, desa kosong; kecamatan & desa nonaktif lagi.
  await chooseRegion(container, 'province', 'DKI Jakarta');
  expect($('#co-province').value).toBe('DKI Jakarta');
  expect($('#co-regency').value).toBe('');
  expect($('#co-district').value).toBe('');
  expect($('#co-village').value).toBe('');
  expect($('#co-district').disabled).toBe(true);
  expect($('#co-village').disabled).toBe(true);
  // Pencarian: "jak sel" hanya menampilkan Jakarta Selatan (daftar diurutkan menurut nama).
  await act(async () => $('#co-regency').focus());
  let opts = [...container.querySelectorAll('#co-regency-list [role="option"]')].map((o) => o.textContent);
  expect(opts).toEqual(['Kota Administrasi Jakarta Pusat', 'Kota Administrasi Jakarta Selatan']);
  await act(async () => setNativeValue($('#co-regency'), 'jak sel'));
  opts = [...container.querySelectorAll('#co-regency-list [role="option"]')].map((o) => o.textContent);
  expect(opts).toEqual(['Kota Administrasi Jakarta Selatan']);
  // Keyboard: Enter memilih pilihan aktif.
  await act(async () => {
    $('#co-regency').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
  expect($('#co-regency').value).toBe('Kota Administrasi Jakarta Selatan');
  expect($('#co-district').disabled).toBe(false);
});

test('galat memuat: pesan ramah + Coba lagi; data wilayah belum tersedia (503)', async () => {
  mockState.regionOverride = { 'regencies/36': { status: 500 } };
  await renderCheckout();
  await chooseRegion(container, 'province', 'Banten');
  expect(container.textContent).toContain('Gagal memuat daftar kabupaten/kota. Periksa koneksi lalu coba lagi.');
  mockState.regionOverride = {};
  await click(btn('Coba lagi'));
  await flush();
  expect(container.textContent).not.toContain('Gagal memuat daftar kabupaten/kota');
  await chooseRegion(container, 'regency', 'Kota Tangerang');

  act(() => root.unmount());
  container.remove();
  clearRegionCache();
  mockState.regionOverride = { provinces: { status: 503 } };
  await renderCheckout();
  expect(container.textContent).toContain('Data wilayah belum tersedia');
});

test('kirim lengkap: hanya kode wilayah dikirim; 422 dari server tampil di field yang salah', async () => {
  await renderCheckout();
  await fillCheckout(container);
  mockState.orderReply = { status: 422, data: { error: 'Kelurahan/desa tidak ditemukan. Pilih ulang dari daftar.', field: 'villageCode' } };
  await click(btn('Buat pesanan'));
  expect(orderPosts()).toHaveLength(1);
  const body = orderPosts()[0].body;
  expect(body).toMatchObject({
    recipientName: 'Budi Penerima',
    provinceCode: '36',
    regencyCode: '36.71',
    districtCode: '36.71.01',
    villageCode: '36.71.01.1001',
    address: 'Jl. Melati No. 9, RT 03/RW 05',
    postalCode: '15111',
    note: '',
  });
  expect(body).not.toHaveProperty('city');
  expect(body).not.toHaveProperty('villageName');
  expect($('#co-village-err').textContent).toContain('tidak ditemukan');
  expect($('#co-village').getAttribute('aria-invalid')).toBe('true');
  expect(window.location.pathname).toBe('/checkout');
  // Kirim ulang sukses -> halaman pesanan.
  mockState.orderReply = null;
  await click(btn('Buat pesanan'));
  expect(orderPosts()).toHaveLength(2);
  expect(window.location.pathname).toBe('/pesanan/MS-261003-0001');
});

test('logika murni: validasi form, telepon, pencarian wilayah', () => {
  const region = { province: { code: '36' }, regency: { code: '36.71' }, district: { code: '36.71.01' }, village: { code: '36.71.01.1001' } };
  const ok = { recipientName: 'Ani', recipientPhone: '0812-3456-7890', address: 'Jl. A No. 1', postalCode: '15111', note: '' };
  expect(validateCheckoutForm(ok, region)).toEqual({});
  expect(Object.keys(validateCheckoutForm({ ...ok, recipientPhone: '12345' }, region))).toEqual(['recipientPhone']);
  expect(Object.keys(validateCheckoutForm(ok, { ...region, district: null }))).toEqual(['districtCode']);
  expect(Object.keys(validateCheckoutForm({ ...ok, note: 'x'.repeat(501) }, region))).toEqual(['note']);
  expect(validPhone('+62 812 3456 7890')).toBe(true);
  expect(validPhone('6281234567890')).toBe(true);
  expect(validPhone('0712345678')).toBe(false);
  const items = [
    { code: '1', name: 'Kota Administrasi Jakarta Selatan' },
    { code: '2', name: 'Kabupaten Kepulauan Seribu' },
    { code: '3', name: 'Pondok Labu' },
  ];
  expect(filterRegions(items, 'JAKARTA  sel').map((i) => i.code)).toEqual(['1']);
  expect(filterRegions(items, 'kep. seribu').map((i) => i.code)).toEqual(['2']);
  expect(filterRegions(items, '').length).toBe(3);
  expect(filterRegions(items, 'xyz')).toEqual([]);
});
