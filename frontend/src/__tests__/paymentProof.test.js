// Tes bukti transfer: tombol per status, sheet unggah (pilih, pratinjau, validasi, kirim, galat, klik ganda),
// alur "Bukti terkirim" -> WhatsApp, ganti/hapus dengan konfirmasi, terkunci setelah dibayar, gambar via
// permintaan berotorisasi (blob URL), admin (kartu bukti & penanda daftar), teks WhatsApp baru.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

const mockState = {
  calls: [],
  order: null,
  storeInfo: null,
  uploadHold: null,
  uploadError: null,
  deleteError: null,
  prep: 'ok',
  admin: {},
  orders: null,
};

jest.mock('axios', () => {
  const respond = (method, url, body, cfg) => {
    mockState.calls.push({ method, url, body, cfg });
    if (url.endsWith('/auth/me')) return { data: { user: { name: 'Budi Akun', role: 'customer' } } };
    if (url.endsWith('/api/products') || url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart')) return { data: { items: [] } };
    if (url.endsWith('/api/store-info')) return { data: mockState.storeInfo };
    if (url.endsWith('/payment-proof')) {
      if (method === 'get') return { data: new Blob(['jpeg'], { type: 'image/jpeg' }) };
      if (method === 'post') {
        if (mockState.uploadError) {
          const e = new Error('x');
          e.response = { status: 409, data: { error: mockState.uploadError } };
          throw e;
        }
        if (mockState.uploadHold) return new Promise((r) => mockState.uploadHold.push(r));
        mockState.order = { ...mockState.order, paymentProof: { uploadedAt: '2026-10-09T03:00:00Z', sizeBytes: 1234, mime: 'image/jpeg' } };
        return { data: { success: true, replaced: false } };
      }
      if (method === 'delete') {
        if (mockState.deleteError) {
          const e = new Error('x');
          e.response = { status: 409, data: { error: mockState.deleteError } };
          throw e;
        }
        mockState.order = { ...mockState.order, paymentProof: null };
        return { status: 204, data: '' };
      }
    }
    if (url.includes('/api/orders?')) return { data: mockState.orders };
    if (url.includes('/api/orders/')) return { data: mockState.order };
    return { data: {} };
  };
  const wrap = (fn) => (...a) => {
    try {
      const v = fn(...a);
      return v instanceof Promise ? v : Promise.resolve(v);
    } catch (e) {
      return Promise.reject(e);
    }
  };
  return {
    __esModule: true,
    default: {
      get: wrap((url, cfg) => respond('get', url, undefined, cfg)),
      post: wrap((url, body, cfg) => respond('post', url, body, cfg)),
      put: wrap((url, body, cfg) => respond('put', url, body, cfg)),
      delete: wrap((url, cfg) => respond('delete', url, undefined, cfg)),
    },
  };
});
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });
jest.mock('../admin/imagePrep', () => {
  const actual = jest.requireActual('../admin/imagePrep');
  return {
    ...actual,
    prepareImage: (file, opts) => {
      mockState.prepOpts = opts;
      if (mockState.prep === 'type') return Promise.reject(new actual.ImagePrepError(actual.MSG_TYPE));
      if (mockState.prep === 'decode') return Promise.reject(new actual.ImagePrepError(actual.MSG_DECODE));
      return Promise.resolve({ blob: new Blob(['jpg-kecil'], { type: 'image/jpeg' }), width: 1200, height: 900, quality: 0.85 });
    },
  };
});
jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path) => {
      mockState.calls.push({ method: 'admin', url: path });
      if (path === '/me') return Promise.resolve({ user: { email: 'admin@example.com' } });
      if (path === '/summary') return Promise.resolve({ orders: {} });
      const key = Object.keys(mockState.admin)
        .filter((k) => path.startsWith(k))
        .sort((a, b) => b.length - a.length)[0];
      return Promise.resolve(key ? mockState.admin[key] : {});
    },
  };
});

const App = require('../App').default;
const { buildPaymentConfirmText } = require('../shop/format');

global.IS_REACT_ACT_ENVIRONMENT = true;

let container;
let root;
let blobSeq = 0;
const created = [];
const revoked = [];
const flush = async () => {
  for (let i = 0; i < 5; i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
};
const renderAt = async (path) => {
  localStorage.setItem('token', 'tes');
  localStorage.setItem('user', JSON.stringify({ id: 'u1', name: 'Budi Akun', role: 'customer' }));
  window.history.replaceState({}, '', path);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<App />));
  await flush();
};
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};
const btn = (text, scope = container) => [...scope.querySelectorAll('button, a')].find((b) => b.textContent.trim() === text);
const sheet = (id) => container.querySelector(`[data-testid="${id}"]`);
const pickFile = async (file = new File(['x'], 'IMG_0001.jpg', { type: 'image/jpeg' })) => {
  const input = sheet('proof-sheet').querySelector('input[type="file"]');
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
};
const uploads = () => mockState.calls.filter((c) => c.method === 'post' && c.url.endsWith('/payment-proof'));
const deletes = () => mockState.calls.filter((c) => c.method === 'delete' && c.url.endsWith('/payment-proof'));

const base = {
  orderNo: 'MS-261009-0001',
  status: 'pending_payment',
  subtotal: 100000,
  discount: 0,
  shippingFee: 10000,
  total: 110000,
  recipient: { name: 'Siti Penerima', phone: '+6281311112222', address: 'Jl. Melati 9', city: 'Tangerang', postalCode: '15111' },
  items: [{ productId: 1, name: 'Kerupuk', unitPrice: 50000, qty: 2, lineTotal: 100000 }],
  history: [],
  canCancel: true,
  canUploadProof: true,
  paymentProof: null,
  createdAt: '2026-10-09T01:00:00Z',
};
const INFO = { storeWhatsapp: '+6281299998888', bankName: 'BCA', bankAccountNumber: '123', bankAccountHolder: 'Toko', paymentConfigured: true };

beforeEach(() => {
  mockState.calls = [];
  mockState.storeInfo = INFO;
  mockState.uploadHold = null;
  mockState.uploadError = null;
  mockState.deleteError = null;
  mockState.prep = 'ok';
  mockState.admin = {};
  created.length = 0;
  revoked.length = 0;
  global.URL.createObjectURL = jest.fn(() => {
    blobSeq += 1;
    const u = `blob:uji-${blobSeq}`;
    created.push(u);
    return u;
  });
  global.URL.revokeObjectURL = jest.fn((u) => revoked.push(u));
});

afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
  localStorage.clear();
});

describe('teks WhatsApp konfirmasi pembayaran', () => {
  test('persis, tanpa nominal dan tanpa tautan; nama penerima lalu cadangan nama akun', () => {
    expect(buildPaymentConfirmText(base, 'Budi Akun')).toBe(
      'Halo Mihan Store, saya sudah transfer untuk pesanan MS-261009-0001 atas nama Siti Penerima. Tolong periksa bukti pembayaran. Terima kasih.'
    );
    const t = buildPaymentConfirmText({ ...base, recipient: { name: '  ' } }, 'Budi Akun');
    expect(t).toContain('atas nama Budi Akun.');
    expect(t).not.toMatch(/Rp|http|\/admin/);
  });
});

describe('tombol per status', () => {
  test('menunggu pembayaran: "Konfirmasi pembayaran" menggantikan WhatsApp', async () => {
    mockState.order = base;
    await renderAt('/pesanan/MS-261009-0001');
    expect(btn('Konfirmasi pembayaran')).toBeTruthy();
    expect(btn('Konfirmasi via WhatsApp')).toBeUndefined();
  });
  test('menunggu konfirmasi: tombol WhatsApp lama tetap, tanpa Konfirmasi pembayaran', async () => {
    mockState.order = { ...base, status: 'pending_confirmation', canUploadProof: false };
    await renderAt('/pesanan/MS-261009-0001');
    expect(btn('Konfirmasi via WhatsApp')).toBeTruthy();
    expect(btn('Konfirmasi pembayaran')).toBeUndefined();
  });
  test('dibayar: tanpa Konfirmasi pembayaran', async () => {
    mockState.order = { ...base, status: 'paid', canCancel: false, canUploadProof: false };
    await renderAt('/pesanan/MS-261009-0001');
    expect(btn('Konfirmasi pembayaran')).toBeUndefined();
  });
});

describe('unggah bukti', () => {
  test('pilih foto -> pratinjau -> Kirim bukti (sekali walau klik ganda) -> Bukti terkirim -> WhatsApp teks baru', async () => {
    mockState.order = base;
    await renderAt('/pesanan/MS-261009-0001');
    await click(btn('Konfirmasi pembayaran'));
    const sh = sheet('proof-sheet');
    expect(document.getElementById(sh.querySelector('[role="dialog"]').getAttribute('aria-labelledby')).textContent).toBe('Upload bukti transfer');
    const input = sh.querySelector('input[type="file"]');
    expect(input.getAttribute('accept')).toBe('image/*');
    expect(input.hasAttribute('capture')).toBe(false);
    expect(btn('Kirim bukti', sh).disabled).toBe(true);
    await pickFile();
    expect(mockState.prepOpts).toMatchObject({ maxSide: 2000, maxBytes: 5 * 1024 * 1024 });
    expect(mockState.prepOpts.acceptTypes).toEqual(expect.arrayContaining(['image/jpeg', 'image/heic']));
    expect(sheet('proof-sheet').querySelector('[data-testid="proof-preview"] img').getAttribute('src')).toMatch(/^blob:uji-/);
    mockState.uploadHold = [];
    const send = btn('Kirim bukti', sheet('proof-sheet'));
    await act(async () => {
      send.click();
      send.click();
    });
    expect(uploads()).toHaveLength(1);
    expect(uploads()[0].body).toBeInstanceOf(FormData);
    expect(uploads()[0].body.get('file')).toBeInstanceOf(Blob);
    expect(uploads()[0].cfg.headers.Authorization).toBe('Bearer tes');
    expect(btn('Mengirim...', sheet('proof-sheet')).disabled).toBe(true);
    mockState.order = { ...base, paymentProof: { uploadedAt: '2026-10-09T03:00:00Z', sizeBytes: 1234, mime: 'image/jpeg' } };
    await act(async () => mockState.uploadHold[0]({ data: { success: true } }));
    await flush();
    expect(sheet('proof-sheet')).toBeNull();
    const sent = sheet('proof-sent');
    expect(sent.textContent).toContain('Bukti transfer sudah kami terima. Kirim juga pesan ke WhatsApp admin?');
    expect(sent.textContent).toContain('Foto tidak ikut terkirim lewat WhatsApp');
    const wa = btn('Kirim ke WhatsApp', sent);
    expect(wa.getAttribute('href')).toMatch(/^https:\/\/wa\.me\/6281299998888\?text=/);
    const text = decodeURIComponent(wa.getAttribute('href').split('text=')[1]);
    expect(text).toBe(buildPaymentConfirmText(base, 'Budi Akun'));
    await click(btn('Nanti saja', sent));
    expect(sheet('proof-sent')).toBeNull();
    // Kartu bukti dengan thumbnail via blob (permintaan berotorisasi, bukan URL publik).
    const card = container.querySelector('[data-testid="proof-card"]');
    expect(card.textContent).toContain('Bukti pembayaran terkirim');
    const img = card.querySelector('img');
    expect(img.getAttribute('src')).toMatch(/^blob:/);
    expect(container.innerHTML).not.toMatch(/\/uploads\/|payment-proofs/);
    const get = mockState.calls.find((c) => c.method === 'get' && c.url.endsWith('/payment-proof'));
    expect(get.cfg.responseType).toBe('blob');
    expect(get.cfg.headers.Authorization).toBe('Bearer tes');
  });

  test('nomor WhatsApp toko tidak valid: tombol disembunyikan + penjelasan', async () => {
    mockState.order = base;
    mockState.storeInfo = { ...INFO, storeWhatsapp: null };
    await renderAt('/pesanan/MS-261009-0001');
    await click(btn('Konfirmasi pembayaran'));
    await pickFile();
    await click(btn('Kirim bukti', sheet('proof-sheet')));
    expect(btn('Kirim ke WhatsApp', sheet('proof-sent'))).toBeUndefined();
    expect(sheet('proof-sent').querySelector('[data-testid="proof-no-wa"]')).not.toBeNull();
  });

  test('validasi foto (tipe / tidak terbaca) dan galat server tampil di sheet', async () => {
    mockState.order = base;
    await renderAt('/pesanan/MS-261009-0001');
    await click(btn('Konfirmasi pembayaran'));
    await pickFile(new File(['x'], 'catatan.txt', { type: 'text/plain' }));
    expect(sheet('proof-sheet').querySelector('[data-testid="proof-error"]').textContent).toBe('Pilih berkas foto (gambar).');
    mockState.prep = 'decode';
    await pickFile();
    expect(sheet('proof-sheet').querySelector('[data-testid="proof-error"]').textContent).toContain('Foto tidak bisa dibaca');
    mockState.prep = 'ok';
    await pickFile();
    mockState.uploadError = 'Bukti transfer hanya bisa diunggah atau diubah saat pesanan menunggu pembayaran.';
    await click(btn('Kirim bukti', sheet('proof-sheet')));
    expect(sheet('proof-sheet').textContent).toContain('hanya bisa diunggah atau diubah saat pesanan menunggu pembayaran');
    expect(sheet('proof-sent')).toBeNull();
  });

  test('Batal menutup tanpa mengirim; blob pratinjau dibebaskan', async () => {
    mockState.order = base;
    await renderAt('/pesanan/MS-261009-0001');
    await click(btn('Konfirmasi pembayaran'));
    await pickFile();
    const previewUrl = sheet('proof-sheet').querySelector('[data-testid="proof-preview"] img').getAttribute('src');
    await click(btn('Batal', sheet('proof-sheet')));
    expect(sheet('proof-sheet')).toBeNull();
    expect(uploads()).toHaveLength(0);
    expect(revoked).toContain(previewUrl);
  });
});

describe('ganti & hapus bukti', () => {
  const withProof = { ...base, paymentProof: { uploadedAt: '2026-10-09T03:00:00Z', sizeBytes: 1234, mime: 'image/jpeg' } };

  test('ganti: konfirmasi "Ganti bukti?" lalu pilih foto baru dan kirim', async () => {
    mockState.order = withProof;
    await renderAt('/pesanan/MS-261009-0001');
    expect(btn('Konfirmasi pembayaran')).toBeUndefined();
    await click(container.querySelector('[data-testid="proof-manage"]'));
    const sh = sheet('proof-sheet');
    expect(sh.querySelector('[data-testid="proof-current"] img').getAttribute('src')).toMatch(/^blob:/);
    await click(btn('Ganti bukti', sh));
    expect(sheet('proof-sheet').querySelector('[data-testid="proof-confirm-replace"]').textContent).toContain('Ganti bukti?');
    await click(btn('Ya, pilih foto baru', sheet('proof-sheet')));
    await pickFile();
    await click(btn('Kirim bukti', sheet('proof-sheet')));
    expect(uploads()).toHaveLength(1);
    expect(sheet('proof-sent')).not.toBeNull();
  });

  test('hapus: konfirmasi "Hapus bukti ini?" lalu DELETE; Batal tidak menghapus', async () => {
    mockState.order = withProof;
    await renderAt('/pesanan/MS-261009-0001');
    await click(container.querySelector('[data-testid="proof-manage"]'));
    await click(btn('Hapus bukti', sheet('proof-sheet')));
    expect(sheet('proof-sheet').querySelector('[data-testid="proof-confirm-delete"]').textContent).toContain('Hapus bukti ini?');
    await click(btn('Batal', sheet('proof-sheet')));
    expect(deletes()).toHaveLength(0);
    await click(btn('Hapus bukti', sheet('proof-sheet')));
    await click(btn('Ya, hapus bukti', sheet('proof-sheet')));
    expect(deletes()).toHaveLength(1);
    expect(sheet('proof-sheet')).toBeNull();
    expect(container.querySelector('[data-testid="proof-card"]')).toBeNull();
    expect(btn('Konfirmasi pembayaran')).toBeTruthy();
  });

  test('terkunci setelah dibayar: hanya Lihat, tanpa Ganti/Hapus; viewer memperbesar', async () => {
    mockState.order = { ...withProof, status: 'paid', canCancel: false, canUploadProof: false };
    await renderAt('/pesanan/MS-261009-0001');
    const card = container.querySelector('[data-testid="proof-card"]');
    expect(card.textContent).toContain('Bukti terkunci');
    expect(card.querySelector('[data-testid="proof-manage"]')).toBeNull();
    await click(btn('Lihat', card));
    expect(sheet('proof-viewer').querySelector('img').getAttribute('src')).toMatch(/^blob:/);
  });

  test('daftar Pesanan Saya: penanda "Bukti terkirim" hanya saat menunggu pembayaran', async () => {
    mockState.orders = {
      items: [
        { orderNo: 'MS-1', status: 'pending_payment', total: 1, itemCount: 1, firstItem: 'A', createdAt: '2026-10-09T01:00:00Z', hasPaymentProof: true },
        { orderNo: 'MS-2', status: 'paid', total: 1, itemCount: 1, firstItem: 'B', createdAt: '2026-10-09T01:00:00Z', hasPaymentProof: true },
      ],
      total: 2,
      page: 1,
      perPage: 10,
    };
    await renderAt('/pesanan');
    expect(container.querySelectorAll('[data-testid="proof-badge"]')).toHaveLength(1);
    expect(container.querySelector('[data-testid="proof-badge"]').textContent).toBe('Bukti terkirim');
  });
});

describe('admin', () => {
  const adminOrder = {
    ...base,
    id: 5,
    pricingLocked: false,
    allowedNext: ['paid', 'cancelled'],
    customer: { name: 'Budi', username: 'budi', email: 'b@example.com' },
    history: [],
    updatedAt: '2026-10-09T03:00:00Z',
  };
  beforeEach(() => {
    global.fetch = jest.fn(() =>
      Promise.resolve({ ok: true, headers: { get: () => 'image/jpeg' }, blob: () => Promise.resolve(new Blob(['jpg'], { type: 'image/jpeg' })) })
    );
  });

  test('detail: kartu Bukti pembayaran via fetch admin -> blob; viewer; Tandai Dibayar tetap', async () => {
    mockState.admin = { '/orders/5': { ...adminOrder, paymentProof: { uploadedAt: '2026-10-09T03:00:00Z', sizeBytes: 1, mime: 'image/jpeg' } }, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    const card = container.querySelector('[data-testid="admin-proof"]');
    expect(card.querySelector('img').getAttribute('src')).toMatch(/^blob:/);
    expect(global.fetch).toHaveBeenCalledWith('/api/admin/orders/5/payment-proof', expect.objectContaining({ credentials: 'same-origin' }));
    expect(btn('Tandai Dibayar')).toBeTruthy();
    await click(btn('Lihat bukti', card));
    expect(sheet('admin-proof-viewer').querySelector('img').getAttribute('src')).toMatch(/^blob:/);
  });

  test('tombol "Kirim ringkasan ke WhatsApp pelanggan": disembunyikan bila ada bukti, tampil bila tidak', async () => {
    const waBtn = () => [...container.querySelectorAll('a')].find((x) => x.textContent.includes('Kirim ringkasan ke WhatsApp pelanggan'));
    for (const status of ['pending_payment', 'paid']) {
      mockState.admin = { '/orders/5': { ...adminOrder, status, paymentProof: { uploadedAt: '2026-10-09T03:00:00Z', sizeBytes: 1, mime: 'image/jpeg' } }, '/settings': { settings: {} } };
      // eslint-disable-next-line no-await-in-loop
      await renderAt('/admin/orders/5');
      expect(waBtn()).toBeUndefined();
      expect(container.textContent).not.toContain('Nomor penerima tidak valid untuk WhatsApp');
      act(() => root.unmount());
      container.remove();
    }
    mockState.admin = { '/orders/5': adminOrder, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    expect(waBtn().getAttribute('href')).toMatch(/^https:\/\/wa\.me\/6281311112222\?text=/);
  });

  test('detail tanpa bukti: "Belum ada bukti."; Tandai Dibayar tetap tanpa syarat bukti', async () => {
    mockState.admin = { '/orders/5': adminOrder, '/settings': { settings: {} } };
    await renderAt('/admin/orders/5');
    expect(container.querySelector('[data-testid="admin-proof-empty"]').textContent).toBe('Belum ada bukti.');
    expect(global.fetch).not.toHaveBeenCalled();
    expect(btn('Tandai Dibayar')).toBeTruthy();
  });

  test('daftar: penanda "Bukti" pada pesanan menunggu pembayaran yang punya bukti', async () => {
    const item = (id, status, has) => ({
      id, orderNo: `MS-${id}`, status, total: 1, itemCount: 1, recipientName: 'R', city: 'K', customerName: 'C', username: 'c',
      createdAt: '2026-10-09T01:00:00Z', hasPaymentProof: has, customer: { id: 1, alias: null },
    });
    mockState.admin = { '/orders': { items: [item(1, 'pending_payment', true), item(2, 'paid', true), item(3, 'pending_payment', false)], total: 3, perPage: 20 } };
    await renderAt('/admin/orders?status=all');
    // Tabel + kartu HP masing-masing satu penanda.
    expect(container.querySelectorAll('[data-testid="admin-proof-badge"]')).toHaveLength(2);
  });
});
