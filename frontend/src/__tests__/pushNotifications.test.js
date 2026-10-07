// Tes notifikasi push admin: deteksi platform, kartu "Notifikasi" (status, aktif, mati, tes),
// panduan "Pasang ke layar utama" iPhone, tombol "Pasang aplikasi" (beforeinstallprompt).
// Catatan: CRA memakai resetMocks, jadi implementasi mock dipasang ulang di beforeEach.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';

const mockApi = { calls: [], config: { enabled: true, publicKey: 'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U', subscriptions: 0 }, fail: null };

jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path, opts = {}) => {
      mockApi.calls.push({ path, method: opts.method || 'GET', body: opts.body });
      if (mockApi.fail) return Promise.reject(new Error(mockApi.fail));
      if (path === '/push/public-key') return Promise.resolve(mockApi.config);
      if (path === '/push/test') return Promise.resolve({ sent: 1, total: 1 });
      return Promise.resolve({ success: true });
    },
  };
});

// eslint-disable-next-line import/first
import PushCard from '../admin/PushCard';
// eslint-disable-next-line import/first
import * as push from '../admin/push';

global.IS_REACT_ACT_ENVIRONMENT = true;

const UA = {
  iphoneSafari: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
  iphoneChrome: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/124.0 Mobile/15E148 Safari/604.1',
  iphoneFirefox: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/125.0 Mobile/15E148 Safari/605.1.15',
  ipadOS: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15',
  android: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36',
  desktop: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36',
};

const setNav = (prop, value) => Object.defineProperty(window.navigator, prop, { value, configurable: true, writable: true });

let standaloneMedia = false;
let env;

// Lingkungan browser tiruan: Notification, serviceWorker, PushManager.
const installBrowser = ({ permission = 'default', grant = 'granted', existing = null } = {}) => {
  const state = { permission, sub: existing };
  const makeSub = (endpoint) => ({
    endpoint,
    options: { applicationServerKey: push.urlBase64ToUint8Array(mockApi.config.publicKey).buffer },
    toJSON: () => ({ endpoint, expirationTime: null, keys: { p256dh: 'p', auth: 'a' } }),
    unsubscribe: jest.fn(() => {
      state.sub = null;
      return Promise.resolve(true);
    }),
  });
  if (existing === true) state.sub = makeSub('https://fcm.googleapis.com/fcm/send/lama');
  const pushManager = {
    getSubscription: jest.fn(() => Promise.resolve(state.sub)),
    subscribe: jest.fn(() => {
      state.sub = makeSub('https://fcm.googleapis.com/fcm/send/baru');
      return Promise.resolve(state.sub);
    }),
  };
  const reg = { pushManager };
  const Notification = function Notification() {};
  Object.defineProperty(Notification, 'permission', { get: () => state.permission, configurable: true });
  Notification.requestPermission = jest.fn(() => {
    state.permission = grant;
    return Promise.resolve(grant);
  });
  window.Notification = Notification;
  window.PushManager = function PushManager() {};
  setNav('serviceWorker', {
    register: jest.fn(() => Promise.resolve(reg)),
    ready: Promise.resolve(reg),
    getRegistration: jest.fn(() => Promise.resolve(state.sub || existing ? reg : undefined)),
  });
  return { state, pushManager, Notification };
};

const removeBrowser = () => {
  delete window.Notification;
  delete window.PushManager;
  delete window.navigator.serviceWorker;
};

let container;
let root;
const flush = async () => {
  for (let i = 0; i < 6; i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await act(async () => {
      await Promise.resolve();
    });
  }
};
const render = async () => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<PushCard />));
  await flush();
};
const button = (label) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === label);
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};
const status = () => container.querySelector('[data-testid="push-status"]').textContent;

beforeEach(() => {
  mockApi.calls = [];
  mockApi.fail = null;
  mockApi.config = { ...mockApi.config, enabled: true };
  standaloneMedia = false;
  window.matchMedia = (q) => ({ matches: q === '(display-mode: standalone)' ? standaloneMedia : false, addEventListener() {}, removeEventListener() {} });
  setNav('userAgent', UA.desktop);
  setNav('platform', 'Win32');
  setNav('maxTouchPoints', 0);
  setNav('standalone', undefined);
  try {
    window.localStorage.clear();
  } catch {
    /* abaikan */
  }
  push.__resetInstallForTests();
});

afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
  removeBrowser();
});

describe('deteksi platform', () => {
  test('iOS, iPadOS, browser lain di iOS, Android', () => {
    expect(push.isIOS(UA.iphoneSafari)).toBe(true);
    expect(push.isIOS(UA.iphoneChrome)).toBe(true);
    expect(push.isIOS(UA.ipadOS, 'MacIntel', 5)).toBe(true);
    expect(push.isIOS(UA.ipadOS, 'MacIntel', 0)).toBe(false); // Mac biasa
    expect(push.isIOS(UA.android, 'Linux armv8l', 5)).toBe(false);
    expect(push.isIOSOtherBrowser(UA.iphoneChrome)).toBe(true);
    expect(push.isIOSOtherBrowser(UA.iphoneFirefox)).toBe(true);
    expect(push.isIOSOtherBrowser(UA.iphoneSafari)).toBe(false);
  });

  test('standalone lewat navigator.standalone atau display-mode', () => {
    expect(push.isStandalone()).toBe(false);
    setNav('standalone', true);
    expect(push.isStandalone()).toBe(true);
    setNav('standalone', undefined);
    standaloneMedia = true;
    expect(push.isStandalone()).toBe(true);
  });

  test('needsIOSInstall hanya untuk iOS yang belum dipasang', () => {
    setNav('userAgent', UA.iphoneSafari);
    expect(push.needsIOSInstall()).toBe(true);
    setNav('standalone', true);
    expect(push.needsIOSInstall()).toBe(false);
    setNav('standalone', undefined);
    setNav('userAgent', UA.android);
    expect(push.needsIOSInstall()).toBe(false);
  });

  test('isPushSupported butuh serviceWorker + PushManager + Notification', () => {
    expect(push.isPushSupported()).toBe(false);
    installBrowser();
    expect(push.isPushSupported()).toBe(true);
  });

  test('urlBase64ToUint8Array: kunci publik VAPID 65 byte', () => {
    const k = push.urlBase64ToUint8Array(mockApi.config.publicKey);
    expect(k).toBeInstanceOf(Uint8Array);
    expect(k.length).toBe(65);
    expect(k[0]).toBe(4);
  });

  test('localStorage yang melempar galat tidak merusak', () => {
    const spy = jest.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('diblokir');
    });
    const spy2 = jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('diblokir');
    });
    expect(push.storageGet('x')).toBeNull();
    expect(() => push.storageSet('x', '1')).not.toThrow();
    spy.mockRestore();
    spy2.mockRestore();
  });
});

describe('kartu Notifikasi', () => {
  test('browser tidak mendukung: status jelas, tanpa tombol, tanpa panggilan API', async () => {
    await render();
    expect(status()).toContain('Browser:Tidak didukung');
    expect(status()).toContain('Perangkat ini:Tidak didukung');
    expect(button('Aktifkan notifikasi')).toBeUndefined();
    expect(mockApi.calls).toHaveLength(0);
  });

  test('aktifkan: izin diminta setelah ketukan, subscribe dengan kunci server, simpan ke backend', async () => {
    const b = installBrowser();
    await render();
    expect(status()).toContain('Browser:Didukung');
    expect(status()).toContain('Izin:Belum diminta');
    expect(status()).toContain('Perangkat ini:Belum berlangganan');
    // Izin TIDAK diminta otomatis saat halaman dibuka.
    expect(b.Notification.requestPermission).not.toHaveBeenCalled();
    expect(mockApi.calls.map((c) => c.path)).toEqual(['/push/public-key']);

    await click(button('Aktifkan notifikasi'));
    expect(b.Notification.requestPermission).toHaveBeenCalledTimes(1);
    expect(window.navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/', updateViaCache: 'none' });
    expect(b.pushManager.subscribe).toHaveBeenCalledTimes(1);
    const opts = b.pushManager.subscribe.mock.calls[0][0];
    expect(opts.userVisibleOnly).toBe(true);
    expect(opts.applicationServerKey).toEqual(push.urlBase64ToUint8Array(mockApi.config.publicKey));
    const post = mockApi.calls.find((c) => c.path === '/push/subscribe' && c.method === 'POST');
    expect(post.body.endpoint).toBe('https://fcm.googleapis.com/fcm/send/baru');
    expect(status()).toContain('Izin:Diizinkan');
    expect(status()).toContain('Perangkat ini:Berlangganan');
    expect(container.textContent).toContain('Notifikasi aktif di perangkat ini.');
    expect(button('Matikan notifikasi')).toBeDefined();
    expect(button('Kirim tes')).toBeDefined();
  });

  test('izin ditolak: tidak subscribe, pesan petunjuk', async () => {
    const b = installBrowser({ grant: 'denied' });
    await render();
    await click(button('Aktifkan notifikasi'));
    expect(b.pushManager.subscribe).not.toHaveBeenCalled();
    expect(mockApi.calls.some((c) => c.path === '/push/subscribe')).toBe(false);
    expect(status()).toContain('Izin:Diblokir');
    expect(container.textContent).toContain('Izin notifikasi ditolak');
    expect(button('Aktifkan notifikasi').disabled).toBe(true);
  });

  test('server belum dikonfigurasi: tombol nonaktif', async () => {
    installBrowser();
    mockApi.config = { enabled: false, publicKey: '' };
    await render();
    expect(container.textContent).toContain('belum dikonfigurasi di server');
    expect(button('Aktifkan notifikasi').disabled).toBe(true);
  });

  test('sudah berlangganan: sinkron diam-diam, matikan = unsubscribe + DELETE, kirim tes', async () => {
    const b = installBrowser({ permission: 'granted', existing: true });
    const sub = b.state.sub;
    await render();
    expect(status()).toContain('Perangkat ini:Berlangganan');
    // Sinkronisasi ulang ke server (upsert) agar langganan yang terhapus di server pulih.
    expect(mockApi.calls.filter((c) => c.path === '/push/subscribe' && c.method === 'POST')).toHaveLength(1);

    await click(button('Kirim tes'));
    expect(mockApi.calls.some((c) => c.path === '/push/test' && c.method === 'POST')).toBe(true);
    expect(container.textContent).toContain('Notifikasi tes dikirim ke 1 perangkat.');

    await click(button('Matikan notifikasi'));
    expect(sub.unsubscribe).toHaveBeenCalledTimes(1);
    const del = mockApi.calls.find((c) => c.method === 'DELETE');
    expect(del.path).toBe('/push/subscribe');
    expect(del.body).toEqual({ endpoint: 'https://fcm.googleapis.com/fcm/send/lama' });
    expect(status()).toContain('Perangkat ini:Belum berlangganan');
    expect(button('Aktifkan notifikasi')).toBeDefined();
  });

  test('galat backend saat aktifkan ditampilkan', async () => {
    installBrowser();
    await render();
    mockApi.fail = 'Data langganan notifikasi tidak valid';
    await click(button('Aktifkan notifikasi'));
    expect(container.textContent).toContain('Gagal mengaktifkan notifikasi: Data langganan notifikasi tidak valid');
  });
});

describe('panduan iPhone & pemasangan', () => {
  test('iPhone Safari belum dipasang: panduan bergambar, tanpa tombol Aktifkan', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await render();
    const guide = container.querySelector('[data-testid="ios-install-guide"]');
    expect(guide).not.toBeNull();
    expect(guide.textContent).toContain('Pasang ke layar utama');
    expect(guide.textContent).toContain('Bagikan');
    expect(guide.textContent).toContain('Tambah ke Layar Utama');
    expect(guide.textContent).toContain('Tambah');
    expect(guide.querySelectorAll('svg').length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector('[data-testid="ios-open-safari"]')).toBeNull();
    expect(button('Aktifkan notifikasi')).toBeUndefined();
  });

  test('Chrome/Firefox di iOS: catatan buka lewat Safari', async () => {
    setNav('userAgent', UA.iphoneChrome);
    await render();
    expect(container.querySelector('[data-testid="ios-open-safari"]').textContent).toContain('Safari');
  });

  test('Jangan tampilkan lagi: disimpan di localStorage', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await render();
    await click(button('Jangan tampilkan lagi'));
    expect(container.querySelector('[data-testid="ios-install-guide"]')).toBeNull();
    expect(window.localStorage.getItem(push.IOS_GUIDE_DISMISS_KEY)).toBe('1');
    act(() => root.unmount());
    container.remove();
    await render();
    expect(container.querySelector('[data-testid="ios-install-guide"]')).toBeNull();
  });

  test('iPhone dari Layar Utama (standalone): tanpa panduan, tombol Aktifkan tampil', async () => {
    setNav('userAgent', UA.iphoneSafari);
    setNav('standalone', true);
    installBrowser();
    await render();
    expect(container.querySelector('[data-testid="ios-install-guide"]')).toBeNull();
    expect(button('Aktifkan notifikasi')).toBeDefined();
  });

  test('Android: beforeinstallprompt -> tombol Pasang aplikasi memanggil prompt', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await render();
    expect(button('Pasang aplikasi')).toBeUndefined();
    const ev = new Event('beforeinstallprompt', { cancelable: true });
    ev.prompt = jest.fn();
    ev.userChoice = Promise.resolve({ outcome: 'accepted' });
    await act(async () => {
      window.dispatchEvent(ev);
    });
    expect(ev.defaultPrevented).toBe(true);
    await click(button('Pasang aplikasi'));
    expect(ev.prompt).toHaveBeenCalledTimes(1);
    expect(button('Pasang aplikasi')).toBeUndefined();
  });
});
