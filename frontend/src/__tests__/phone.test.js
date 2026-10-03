// Nomor telepon: aturan sama dengan backend (TestNormalizePhone* di backend/unit_test.go) dan
// checkout: nomor dari kontak/WhatsApp HP (NBSP, tanda hubung Unicode, pembungkus bidi) tidak
// boleh ditolak; nomor dikirim dalam bentuk baku +628xx; galat server 422 tampil di kolom telepon.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { fillCheckout, setNativeValue } from '../testUtils/regionFixtures';
import { clearRegionCache } from '../shop/regionsApi';
import { validateCheckoutForm, validPhone } from '../shop/Checkout';
import { PHONE_ERRORS, normalizePhone } from '../phone';

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
    if (url.endsWith('/auth/me')) return Promise.resolve({ data: { user: { name: 'Nama Akun', role: 'customer', phone: '' } } });
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

const WANT = '+6281234567890';
const OK = {
  '081234567890': WANT,
  '+6281234567890': WANT,
  '6281234567890': WANT,
  '0812-3456-7890': WANT,
  '+62 812 3456 7890': WANT,
  '(0812) 3456.7890': WANT,
  ' 081234567890 ': WANT,
  '0812\u00a03456\u00a07890': WANT, // NBSP
  '+62\u202f812\u20113456\u20117890': WANT, // narrow NBSP + non-breaking hyphen
  '0812\u20103456\u20127890': WANT,
  '0812\u20133456\u20147890': WANT, // en/em dash
  '0812\u22123456\u22127890': WANT, // minus
  '\u202a+62 812-3456-7890\u202c': WANT, // pembungkus bidi (WhatsApp)
  '\u200b081234567890\ufeff': WANT,
  '\uff10\uff18\uff11\uff12\uff13\uff14\uff15\uff16\uff17\uff18\uff19\uff10': WANT, // angka lebar
  '\u0660\u0668\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668\u0669\u0660': WANT, // Arab-Indic
  '\u06f0\u06f8\u06f1\u06f2\u06f3\u06f4\u06f5\u06f6\u06f7\u06f8\u06f9\u06f0': WANT,
  '\uff0b62 812 3456 7890': WANT,
  '+62 0812 3456 7890': WANT, // "+62 0" dirapikan
  '+620812-3456-7890': WANT,
  '620812 3456 7890': WANT,
  '': '',
  '\u00a0\u2003': '',
};
const BAD = {
  '08123': 'short',
  '+62812': 'short',
  '+6281111111111081234': 'long', // telepon dari akun + ketikan baru tanpa dihapus
  '+62812345678901234': 'long',
  '0812abc4567': 'chars',
  '++6281234567': 'chars',
  '0812/3456/7890': 'chars',
  '62+81234567890': 'chars',
  '021555123': 'prefix',
  '+1555123456': 'prefix',
  '12345678': 'prefix',
  '62': 'prefix',
  '+': 'prefix',
};

test('normalizePhone: varian Unicode diseragamkan, galat spesifik (sama dengan backend)', () => {
  Object.entries(OK).forEach(([inp, want]) => {
    expect([inp, normalizePhone(inp)]).toEqual([inp, { phone: want, error: '' }]);
  });
  Object.entries(BAD).forEach(([inp, kind]) => {
    expect([inp, normalizePhone(String(inp))]).toEqual([inp, { phone: '', error: PHONE_ERRORS[kind] }]);
  });
  expect(validPhone('0812\u20113456\u20117890')).toBe(true);
  const region = { province: { code: '36' }, regency: { code: '36.71' }, district: { code: '36.71.01' }, village: { code: '36.71.01.1001' } };
  const form = { recipientName: 'Ani', recipientPhone: '', address: 'Jl. A No. 1', postalCode: '15111', note: '' };
  expect(validateCheckoutForm(form, region).recipientPhone).toBe('Nomor telepon wajib diisi.');
  expect(validateCheckoutForm({ ...form, recipientPhone: '08123' }, region).recipientPhone).toBe(PHONE_ERRORS.short);
  expect(validateCheckoutForm({ ...form, recipientPhone: '0812\u00a03456\u00a07890' }, region)).toEqual({});
});

let container;
let root;
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};
const renderCheckout = async () => {
  localStorage.clear();
  localStorage.setItem('token', 'tes');
  localStorage.setItem('user', JSON.stringify({ name: 'Nama Akun', role: 'customer', phone: '' }));
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
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
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
const setPhone = async (v) => {
  await act(async () => {
    setNativeValue($('#co-phone'), v);
  });
};

test.each([
  ['NBSP dari kontak HP', '0812\u00a03456\u00a07890'],
  ['tanda hubung tak-putus + bidi (salin dari WhatsApp)', '\u202a+62 812\u20113456\u20117890\u202c'],
  ['+62 0812', '+62 0812 3456 7890'],
  ['biasa 08', '081234567890'],
])('checkout: telepon %s diterima dan dikirim sebagai +628xx', async (_label, typed) => {
  await renderCheckout();
  await fillCheckout(container);
  await setPhone(typed);
  await click(btn('Buat pesanan'));
  expect($('#co-phone-err')).toBeNull();
  expect(orderPosts()).toHaveLength(1);
  expect(orderPosts()[0].body.recipientPhone).toBe(WANT);
});

test('checkout: galat spesifik di kolom telepon + fokus; galat 422 server juga di kolom telepon', async () => {
  await renderCheckout();
  await fillCheckout(container);
  await setPhone('+6281111111111081234');
  await click(btn('Buat pesanan'));
  expect(orderPosts()).toHaveLength(0);
  expect($('#co-phone-err').textContent).toBe(PHONE_ERRORS.long);
  expect(document.activeElement).toBe($('#co-phone'));

  await setPhone('081234567890');
  mockState.orderReply = { status: 422, data: { error: 'Nomor telepon terlalu pendek, minimal 8 digit', field: 'recipientPhone' } };
  await act(async () => {
    $('#co-name').focus();
  });
  await click(btn('Buat pesanan'));
  expect(orderPosts()).toHaveLength(1);
  expect($('#co-phone-err').textContent).toBe('Nomor telepon terlalu pendek, minimal 8 digit');
  expect($('#co-phone').getAttribute('aria-invalid')).toBe('true');
  expect($('#co-name-err')).toBeNull();
  expect(document.activeElement).toBe($('#co-phone'));
});
