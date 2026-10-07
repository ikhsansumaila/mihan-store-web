/**
 * @jest-environment node
 */
// Tes public/sw.js: tanpa cache/fetch handler, notifikasi dari payload minimal, url hanya /admin same-origin.
const fs = require('fs');
const path = require('path');

const SW_PATH = path.join(__dirname, '..', '..', 'public', 'sw.js');
const ORIGIN = 'https://store.mihan.web.id';

const loadSW = ({ clientsList = [] } = {}) => {
  const handlers = {};
  const shown = [];
  const opened = [];
  const fakeSelf = {
    location: { origin: ORIGIN },
    addEventListener: (type, fn) => {
      handlers[type] = fn;
    },
    skipWaiting: jest.fn(),
    registration: {
      showNotification: (title, options) => {
        shown.push({ title, options });
        return Promise.resolve();
      },
    },
    clients: {
      claim: jest.fn(() => Promise.resolve()),
      matchAll: jest.fn(() => Promise.resolve(clientsList)),
      openWindow: jest.fn((u) => {
        opened.push(u);
        return Promise.resolve(null);
      }),
    },
  };
  const mod = { exports: {} };
  // eslint-disable-next-line no-new-func
  new Function('self', 'module', fs.readFileSync(SW_PATH, 'utf8'))(fakeSelf, mod);
  return { handlers, shown, opened, fakeSelf, api: mod.exports };
};

const pushEvent = (payload) => {
  let waited;
  return {
    ev: {
      data: payload === undefined ? null : { json: () => (typeof payload === 'string' ? JSON.parse(payload) : payload) },
      waitUntil: (p) => {
        waited = p;
      },
    },
    done: () => waited,
  };
};

test('tanpa handler fetch dan tanpa Cache Storage', () => {
  const { handlers } = loadSW();
  expect(Object.keys(handlers).sort()).toEqual(['activate', 'install', 'notificationclick', 'push']);
  const src = fs.readFileSync(SW_PATH, 'utf8');
  expect(src).not.toMatch(/caches\.|addEventListener\(\s*['"]fetch/);
});

test('safeUrl: hanya path /admin atau /pesanan relatif same-origin', () => {
  const { api } = loadSW();
  const ok = {
    '/admin': '/admin',
    '/admin/orders/MS-261007-0001': '/admin/orders/MS-261007-0001',
    '/admin/orders?status=pending_payment': '/admin/orders?status=pending_payment',
    '/pesanan': '/pesanan',
    '/pesanan/MS-261007-0001': '/pesanan/MS-261007-0001',
  };
  Object.entries(ok).forEach(([i, o]) => expect(api.safeUrl(i)).toBe(o));
  [
    'https://evil.example/admin',
    'https://evil.example/pesanan/MS-1',
    '//evil.example/admin',
    '//evil.example/pesanan',
    '/admin//evil.example',
    '/pesanan//evil.example',
    '/\\evil.example',
    '/admin\\..\\x',
    '/pesanan/../admin',
    '/admin/../pesanan',
    'javascript:alert(1)',
    'data:text/html,x',
    '/keranjang',
    '/pesananku',
    '/administrator',
    ' /admin',
    null,
    42,
    `/pesanan/${'x'.repeat(400)}`,
  ].forEach((u) => expect(api.safeUrl(u)).toBe('/'));
});

test('push: judul, nomor pesanan, ikon, tag per pesanan, url aman', async () => {
  const { handlers, shown } = loadSW();
  const p = pushEvent({ title: 'Pesanan baru', body: 'MS-261007-0001', url: '/admin/orders/MS-261007-0001', tag: 'pesanan-MS-261007-0001' });
  handlers.push(p.ev);
  await p.done();
  expect(shown).toHaveLength(1);
  expect(shown[0].title).toBe('Pesanan baru');
  expect(shown[0].options).toMatchObject({
    body: 'MS-261007-0001',
    icon: '/icon-192.png',
    tag: 'pesanan-MS-261007-0001',
    renotify: true,
    data: { url: '/admin/orders/MS-261007-0001' },
  });
});

test('push: payload rusak/kosong/url asing tetap aman', async () => {
  const { handlers, shown } = loadSW();
  for (const payload of [undefined, '{rusak', { title: 5, url: 'https://evil.example/' }]) {
    const p = pushEvent(payload);
    handlers.push(p.ev);
    // eslint-disable-next-line no-await-in-loop
    await p.done();
  }
  expect(shown).toHaveLength(3);
  shown.forEach((s) => {
    expect(s.title).toBe('Mihan Store');
    expect(s.options.data.url).toBe('/');
    expect(s.options.tag).toBeUndefined();
  });
});

test('notificationclick: fokus/arahkan tab yang ada, atau buka jendela baru', async () => {
  const client = { url: `${ORIGIN}/admin`, focus: jest.fn(() => Promise.resolve()), navigate: jest.fn(function nav() { return Promise.resolve(client); }) };
  let w = loadSW({ clientsList: [{ url: 'https://lain.example/', focus: jest.fn() }, client] });
  let waited;
  const close = jest.fn();
  w.handlers.notificationclick({ notification: { close, data: { url: '/admin/orders/MS-1' } }, waitUntil: (p) => (waited = p) });
  await waited;
  expect(close).toHaveBeenCalled();
  expect(client.navigate).toHaveBeenCalledWith(`${ORIGIN}/admin/orders/MS-1`);
  expect(client.focus).toHaveBeenCalled();
  expect(w.opened).toHaveLength(0);

  w = loadSW({ clientsList: [] });
  w.handlers.notificationclick({ notification: { close, data: { url: 'https://evil.example/' } }, waitUntil: (p) => (waited = p) });
  await waited;
  expect(w.opened).toEqual([`${ORIGIN}/`]);
});

test('manifest: nama, start_url, standalone, ikon 192/512', () => {
  const m = JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', 'public', 'manifest.webmanifest'), 'utf8'));
  expect(m.name).toBe('Mihan Store');
  expect(m.short_name).toBeTruthy();
  expect(m.start_url).toBe('/');
  expect(m.scope).toBe('/');
  expect(m.display).toBe('standalone');
  expect(m.theme_color).toMatch(/^#[0-9a-f]{6}$/i);
  expect(m.icons.map((i) => i.sizes)).toEqual(['192x192', '512x512']);
  m.icons.forEach((i) => expect(fs.existsSync(path.join(__dirname, '..', '..', 'public', i.src.split('?')[0]))).toBe(true));
  const html = fs.readFileSync(path.join(__dirname, '..', '..', 'public', 'index.html'), 'utf8');
  expect(html).toContain('<link rel="manifest" href="/manifest.webmanifest">');
  expect(html).toContain('<meta name="apple-mobile-web-app-capable" content="yes">');
});

test('push pelanggan: url /pesanan/<nomor> dipertahankan', async () => {
  const { handlers, shown } = loadSW();
  const p = pushEvent({ title: 'Pembayaran diterima', body: 'Pesanan MS-1: pembayaran sudah kami terima', url: '/pesanan/MS-1', tag: 'pesanan-MS-1' });
  handlers.push(p.ev);
  await p.done();
  expect(shown[0].options.data.url).toBe('/pesanan/MS-1');
  expect(shown[0].options.tag).toBe('pesanan-MS-1');
});

test('push admin: body multi-baris (dari/penerima/nomor) diteruskan apa adanya', async () => {
  const { handlers, shown } = loadSW();
  const body = 'dari Bu Siti Toko Maju\npenerima Siti Aminah\nMS-261007-0001';
  const p = pushEvent({ title: 'Pesanan Baru', body, url: '/admin/orders/MS-261007-0001', tag: 'pesanan-MS-261007-0001' });
  handlers.push(p.ev);
  await p.done();
  expect(shown[0].title).toBe('Pesanan Baru');
  expect(shown[0].options.body).toBe(body);
  expect(shown[0].options.body.split('\n')).toHaveLength(3);
});
