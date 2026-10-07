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

const sheet = () => document.querySelector('[data-testid="install-sheet"]');
const sheetButton = (label) => [...(sheet()?.querySelectorAll('button') || [])].find((b) => b.textContent.trim() === label);
const installBtn = () => container.querySelector('[data-testid="install-button"]');
const openSheet = async () => {
  await click(installBtn());
  expect(sheet()).not.toBeNull();
};
const fireBIP = async () => {
  const ev = new Event('beforeinstallprompt', { cancelable: true });
  ev.prompt = jest.fn();
  ev.userChoice = Promise.resolve({ outcome: 'accepted' });
  await act(async () => {
    window.dispatchEvent(ev);
  });
  return ev;
};

describe('tombol Pasang aplikasi & bottom sheet', () => {
  test('iPhone Safari: tombol membuka sheet langkah iOS bernomor, tanpa tombol Aktifkan', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await render();
    expect(sheet()).toBeNull(); // tidak ada popup otomatis
    expect(container.querySelector('[data-testid="ios-install-hint"]').textContent).toContain('Layar Utama');
    expect(button('Aktifkan notifikasi')).toBeUndefined();
    await openSheet();
    const dlg = sheet().querySelector('[role="dialog"]');
    expect(dlg.getAttribute('aria-modal')).toBe('true');
    expect(document.getElementById(dlg.getAttribute('aria-labelledby')).textContent).toBe('Pasang Mihan Store');
    expect(sheet().textContent).toContain('Akses lebih cepat langsung dari layar utama');
    const steps = sheet().querySelector('[data-testid="install-steps"]');
    expect(steps.getAttribute('data-platform')).toBe('ios');
    const items = [...steps.querySelectorAll('li')].map((li) => li.textContent);
    expect(items).toHaveLength(3);
    expect(items[0]).toContain('Bagikan');
    expect(items[1]).toContain('Tambah ke Layar Utama');
    expect(items[2]).toContain('Tambah');
    expect(steps.querySelector('[data-icon="share"]')).not.toBeNull();
    expect(steps.querySelector('[data-icon="add"]')).not.toBeNull();
    expect(steps.querySelector('strong').textContent).toBe('Bagikan');
    expect(sheet().textContent).toContain('hanya bisa diaktifkan dari aplikasi yang dibuka dari Layar Utama');
    expect(sheet().querySelector('[data-testid="install-open-safari"]')).toBeNull();
    expect(sheetButton('Tutup')).toBeDefined();
    expect(sheetButton('Pasang Sekarang')).toBeUndefined();
    // Fokus pindah ke sheet, scroll body dikunci.
    expect(document.activeElement).toBe(dlg);
    expect(document.body.style.overflow).toBe('hidden');
  });

  test('Chrome/Firefox di iOS: catatan buka lewat Safari', async () => {
    setNav('userAgent', UA.iphoneChrome);
    await render();
    await openSheet();
    expect(sheet().querySelector('[data-testid="install-open-safari"]').textContent).toContain('Safari');
  });

  test.each([
    ['tombol X', async () => click(sheet().querySelector('button[aria-label="Tutup"]'))],
    ['tombol Tutup', async () => click(sheetButton('Tutup'))],
    ['ketuk latar', async () => click(sheet().querySelector('[data-testid="install-sheet-backdrop"]'))],
    [
      'Escape',
      async () => {
        await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
        await flush();
      },
    ],
  ])('sheet ditutup lewat %s; fokus kembali ke pemicu, scroll dibuka', async (_, close) => {
    setNav('userAgent', UA.iphoneSafari);
    await render();
    installBtn().focus();
    await openSheet();
    await close();
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(installBtn());
    expect(document.body.style.overflow).toBe('');
  });

  test('ketuk di dalam sheet tidak menutup; Tab berputar di dalam sheet', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await render();
    await openSheet();
    await click(sheet().querySelector('[data-testid="install-steps"]'));
    expect(sheet()).not.toBeNull();
    const btns = [...sheet().querySelectorAll('button')];
    btns[btns.length - 1].focus();
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })));
    expect(document.activeElement).toBe(btns[0]);
  });

  test('Jangan tampilkan lagi: disimpan di localStorage, tombol hilang', async () => {
    setNav('userAgent', UA.iphoneSafari);
    installBrowser();
    await render();
    await openSheet();
    await click(sheetButton('Jangan tampilkan lagi'));
    expect(sheet()).toBeNull();
    expect(installBtn()).toBeNull();
    expect(window.localStorage.getItem(push.IOS_GUIDE_DISMISS_KEY)).toBe('1');
    act(() => root.unmount());
    container.remove();
    await render();
    expect(installBtn()).toBeNull();
  });

  test('sudah standalone (iPhone/Android): tidak ada tombol Pasang aplikasi; Aktifkan tampil', async () => {
    setNav('userAgent', UA.iphoneSafari);
    setNav('standalone', true);
    installBrowser();
    await render();
    expect(installBtn()).toBeNull();
    expect(container.querySelector('[data-testid="ios-install-hint"]')).toBeNull();
    expect(button('Aktifkan notifikasi')).toBeDefined();
    act(() => root.unmount());
    container.remove();
    setNav('standalone', undefined);
    setNav('userAgent', UA.android);
    standaloneMedia = true;
    await render();
    await fireBIP();
    expect(installBtn()).toBeNull();
  });

  test('desktop tanpa beforeinstallprompt: tidak ada tombol', async () => {
    installBrowser();
    await render();
    expect(installBtn()).toBeNull();
  });

  test('Android dengan beforeinstallprompt: sheet "Pasang Sekarang" memanggil prompt, lalu tombol hilang', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await render();
    const ev = await fireBIP();
    expect(ev.defaultPrevented).toBe(true);
    await openSheet();
    expect(ev.prompt).not.toHaveBeenCalled(); // tombol kartu hanya membuka sheet
    expect(sheet().querySelector('[data-testid="install-steps"]')).toBeNull();
    expect(sheet().textContent).toContain('notifikasi pesanan');
    expect(sheet().textContent).not.toMatch(/offline/i);
    expect(sheetButton('Nanti')).toBeDefined();
    await click(sheetButton('Pasang Sekarang'));
    expect(ev.prompt).toHaveBeenCalledTimes(1);
    expect(sheet()).toBeNull();
    expect(installBtn()).toBeNull();
  });

  test('Android: Nanti menutup sheet tanpa prompt', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await render();
    const ev = await fireBIP();
    await openSheet();
    await click(sheetButton('Nanti'));
    expect(sheet()).toBeNull();
    expect(ev.prompt).not.toHaveBeenCalled();
    expect(installBtn()).not.toBeNull();
  });

  test('Android: prompt ditolak -> tombol tetap, sheet berikutnya berisi langkah manual', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await render();
    const ev = await fireBIP();
    ev.userChoice = Promise.resolve({ outcome: 'dismissed' });
    await openSheet();
    await click(sheetButton('Pasang Sekarang'));
    expect(ev.prompt).toHaveBeenCalledTimes(1);
    expect(installBtn()).not.toBeNull();
    await openSheet();
    const steps = sheet().querySelector('[data-testid="install-steps"]');
    expect(steps.getAttribute('data-platform')).toBe('android');
    const items = [...steps.querySelectorAll('li')].map((li) => li.textContent);
    expect(items).toHaveLength(3);
    expect(items[0]).toContain('⋮');
    expect(items[1]).toContain('Pasang aplikasi');
    expect(items[1]).toContain('Tambahkan ke layar utama');
    expect(items[2]).toMatch(/Pasang.*Tambahkan/);
    expect(sheetButton('Tutup')).toBeDefined();
    expect(sheetButton('Pasang Sekarang')).toBeUndefined();
  });

  test('Android tanpa beforeinstallprompt: sheet langkah manual', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await render();
    await openSheet();
    expect(sheet().querySelector('[data-testid="install-steps"]').getAttribute('data-platform')).toBe('android');
    expect(sheet().querySelector('[data-icon="menu"]')).not.toBeNull();
  });
});

// ---------- Sheet otomatis setelah admin baru masuk ----------
// eslint-disable-next-line import/first
import AutoInstallSheet, { AUTO_SHEET_SESSION_KEY } from '../admin/AutoInstallSheet';
// eslint-disable-next-line import/first
import AdminLayout from '../admin/AdminLayout';
// eslint-disable-next-line import/first
import { MemoryRouter, Route, Routes, Link } from 'react-router-dom';

const autoSheet = () => document.querySelector('[data-testid="auto-install-sheet"]');
const renderNode = async (node) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(node));
  await flush();
};
const remount = async (node) => {
  act(() => root.unmount());
  container.remove();
  await renderNode(node);
};
const waitDelay = async (ms = 5) => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
  await flush();
};

describe('sheet Pasang otomatis setelah login admin', () => {
  beforeEach(() => {
    try {
      window.sessionStorage.clear();
    } catch {
      /* abaikan */
    }
  });

  test('iPhone Safari: muncul sekali per sesi tab (langkah iOS), tidak muncul lagi setelah muat ulang', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).not.toBeNull();
    expect(autoSheet().querySelector('[data-testid="install-steps"]').getAttribute('data-platform')).toBe('ios');
    expect(window.sessionStorage.getItem(AUTO_SHEET_SESSION_KEY)).toBe('1');
    // Tutup (X) hanya untuk sesi ini: tidak menyimpan "jangan tampilkan lagi".
    await click(autoSheet().querySelector('button[aria-label="Tutup"]'));
    expect(autoSheet()).toBeNull();
    expect(window.localStorage.getItem(push.IOS_GUIDE_DISMISS_KEY)).toBeNull();
    await remount(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).toBeNull();
  });

  test('sesi baru (sessionStorage kosong) memunculkannya lagi; "Jangan tampilkan lagi" menghentikannya', async () => {
    setNav('userAgent', UA.iphoneChrome);
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet().querySelector('[data-testid="install-open-safari"]')).not.toBeNull();
    await click([...autoSheet().querySelectorAll('button')].find((b) => b.textContent.trim() === 'Jangan tampilkan lagi'));
    expect(window.localStorage.getItem(push.IOS_GUIDE_DISMISS_KEY)).toBe('1');
    window.sessionStorage.clear();
    await remount(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).toBeNull();
  });

  test.each([
    ['standalone iPhone', () => { setNav('userAgent', UA.iphoneSafari); setNav('standalone', true); }],
    ['standalone Android', () => { setNav('userAgent', UA.android); standaloneMedia = true; }],
    ['sudah pilih Jangan tampilkan lagi', () => { setNav('userAgent', UA.iphoneSafari); window.localStorage.setItem(push.IOS_GUIDE_DISMISS_KEY, '1'); }],
    ['desktop', () => {}],
    ['Mac (bukan iPad)', () => { setNav('userAgent', UA.ipadOS); setNav('platform', 'MacIntel'); }],
  ])('tidak muncul: %s', async (_, arrange) => {
    arrange();
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).toBeNull();
    expect(window.sessionStorage.getItem(AUTO_SHEET_SESSION_KEY)).toBeNull();
  });

  test('desktop dengan beforeinstallprompt pun tidak otomatis', async () => {
    await fireBIP();
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).toBeNull();
  });

  test('Android: tanpa prompt = langkah manual; dengan prompt = Pasang Sekarang / Nanti', async () => {
    setNav('userAgent', UA.android);
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet().querySelector('[data-testid="install-steps"]').getAttribute('data-platform')).toBe('android');
    // Event datang setelah sheet terbuka -> berganti ke mode prompt.
    const ev = await fireBIP();
    const btn = (l) => [...autoSheet().querySelectorAll('button')].find((b) => b.textContent.trim() === l);
    expect(btn('Pasang Sekarang')).toBeDefined();
    await click(btn('Pasang Sekarang'));
    expect(ev.prompt).toHaveBeenCalledTimes(1);
    expect(autoSheet()).toBeNull();
  });

  test('tidak menimpa dialog lain yang sedang terbuka', async () => {
    setNav('userAgent', UA.iphoneSafari);
    const other = document.createElement('div');
    other.setAttribute('role', 'dialog');
    other.setAttribute('aria-modal', 'true');
    document.body.appendChild(other);
    await renderNode(<AutoInstallSheet delayMs={0} />);
    await waitDelay();
    expect(autoSheet()).toBeNull();
    expect(window.sessionStorage.getItem(AUTO_SHEET_SESSION_KEY)).toBeNull(); // dicoba lagi lain kali
    other.remove();
  });

  test('di AdminLayout: muncul setelah jeda, tidak muncul ulang saat pindah halaman', async () => {
    setNav('userAgent', UA.iphoneSafari);
    jest.useFakeTimers();
    try {
      await renderNode(
        <MemoryRouter initialEntries={['/admin']}>
          <AdminLayout me={{ email: 'pemilik@example.com' }}>
            <Routes>
              <Route path="/admin" element={<Link to="/admin/orders">ke pesanan</Link>} />
              <Route path="/admin/orders" element={<p>Halaman pesanan</p>} />
            </Routes>
          </AdminLayout>
        </MemoryRouter>
      );
      expect(autoSheet()).toBeNull(); // tidak langsung menangkap fokus sebelum halaman siap
      await act(async () => {
        jest.advanceTimersByTime(900);
      });
      expect(autoSheet()).not.toBeNull();
      expect(document.activeElement).toBe(autoSheet().querySelector('[role="dialog"]'));
      await act(async () => autoSheet().querySelector('button[aria-label="Tutup"]').click());
      expect(autoSheet()).toBeNull();
      await act(async () => [...container.querySelectorAll('a')].find((a) => a.textContent === 'ke pesanan').click());
      await act(async () => {
        jest.advanceTimersByTime(2000);
      });
      expect(container.textContent).toContain('Halaman pesanan');
      expect(autoSheet()).toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });
});
