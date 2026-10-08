// Tes notifikasi push PELANGGAN: menu gear Pengaturan (status, Aktifkan/Matikan, Pasang aplikasi, Logout),
// titik indikator, logout melepas langganan, sheet otomatis setelah login pelanggan, navbar.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

const TEST_KEY = 'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U';
const mockState = { calls: [], config: { enabled: true, publicKey: TEST_KEY }, keep: false, failDelete: false, hangDelete: false, me: null };

jest.mock('axios', () => {
  const respond = (method, url, body, cfg) => {
    mockState.calls.push({ method, url, body, cfg });
    if (url.endsWith('/auth/me')) return { data: { user: mockState.me } };
    if (url.endsWith('/api/products')) return { data: [] };
    if (url.endsWith('/api/categories')) return { data: [] };
    if (url.endsWith('/api/cart')) return { data: { items: [] } };
    if (url.endsWith('/api/push/public-key')) return { data: mockState.config };
    if (url.endsWith('/api/push/subscribe') && method === 'delete') {
      if (mockState.hangDelete) return new Promise(() => {});
      if (mockState.failDelete) throw new Error('jaringan');
      return { data: { success: true, removed: true, keepBrowserSubscription: mockState.keep } };
    }
    return { data: { success: true } };
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
      get: wrap((url, cfg) => respond('get', url, undefined, cfg)),
      post: wrap((url, body, cfg) => respond('post', url, body, cfg)),
      put: wrap((url, body, cfg) => respond('put', url, body, cfg)),
      delete: wrap((url, cfg) => respond('delete', url, cfg?.data, cfg)),
    },
  };
});
jest.mock('jspdf', () => ({ __esModule: true, default: function MockPdf() {} }));
jest.mock('@marsidev/react-turnstile', () => ({ __esModule: true, Turnstile: () => null }), { virtual: true });

const App = require('../App').default;
const SettingsMenu = require('../shop/SettingsMenu').default;
const pushApi = require('../shop/pushApi');
const push = require('../admin/push');
const { AUTO_SHEET_SESSION_KEY } = require('../admin/AutoInstallSheet');

global.IS_REACT_ACT_ENVIRONMENT = true;

const UA = {
  iphoneSafari: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
  android: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36',
  desktop: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36',
};
const setNav = (prop, value) => Object.defineProperty(window.navigator, prop, { value, configurable: true, writable: true });
const USER = { id: 'pub-1', name: 'Budi', username: 'budi', role: 'customer' };

let standaloneMedia = false;
const installBrowser = ({ permission = 'default', grant = 'granted', existing = false } = {}) => {
  const state = { permission, sub: null };
  const makeSub = (endpoint) => ({
    endpoint,
    options: { applicationServerKey: push.urlBase64ToUint8Array(mockState.config.publicKey).buffer },
    toJSON: () => ({ endpoint, expirationTime: null, keys: { p256dh: 'p', auth: 'a' } }),
    unsubscribe: jest.fn(() => {
      state.sub = null;
      return Promise.resolve(true);
    }),
  });
  if (existing) state.sub = makeSub('https://fcm.googleapis.com/fcm/send/lama');
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
    getRegistration: jest.fn(() => Promise.resolve(reg)),
  });
  return { state, pushManager, Notification };
};

let container;
let root;
const flush = async () => {
  for (let i = 0; i < 6; i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
};
const renderEl = async (el) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(el));
  await flush();
};
const renderAppAt = async (path, user = null) => {
  if (user) {
    localStorage.setItem('token', 'tes');
    localStorage.setItem('user', JSON.stringify(user));
  }
  mockState.me = user;
  window.history.replaceState({}, '', path);
  await renderEl(<App />);
};
const click = async (el) => {
  await act(async () => el.click());
  await flush();
};
const gear = () => container.querySelector('[data-testid="settings-gear"]');
const sheet = (id = 'settings-sheet') => document.querySelector(`[data-testid="${id}"]`);
const sheetBtn = (label, id) => [...(sheet(id)?.querySelectorAll('button') || [])].find((b) => b.textContent.trim() === label);
const pushCalls = (method) => mockState.calls.filter((c) => c.method === method && c.url.includes('/api/push/'));

beforeEach(() => {
  mockState.calls = [];
  mockState.keep = false;
  mockState.failDelete = false;
  mockState.hangDelete = false;
  mockState.config = { enabled: true, publicKey: TEST_KEY };
  standaloneMedia = false;
  window.matchMedia = (q) => ({ matches: q === '(display-mode: standalone)' ? standaloneMedia : false, addEventListener() {}, removeEventListener() {} });
  setNav('userAgent', UA.desktop);
  setNav('platform', 'Win32');
  setNav('maxTouchPoints', 0);
  setNav('standalone', undefined);
  localStorage.clear();
  sessionStorage.clear();
  push.__resetInstallForTests();
});

afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
  root = null;
  container = null;
  delete window.Notification;
  delete window.PushManager;
  delete window.navigator.serviceWorker;
});

describe('menu gear Pengaturan', () => {
  test('aktifkan: izin setelah ketukan, subscribe, simpan ke /api/push/subscribe dengan sesi Bearer; titik hilang', async () => {
    const b = installBrowser();
    localStorage.setItem('token', 'tes-token');
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    expect(gear().getAttribute('aria-label')).toBe('Pengaturan (notifikasi belum aktif)');
    expect(container.querySelector('[data-testid="settings-dot"]')).not.toBeNull();
    expect(b.Notification.requestPermission).not.toHaveBeenCalled();
    await click(gear());
    const dlg = sheet().querySelector('[role="dialog"]');
    expect(document.getElementById(dlg.getAttribute('aria-labelledby')).textContent).toBe('Pengaturan');
    expect(sheet().textContent).toContain('Perangkat ini:Belum aktif');
    await click(sheetBtn('Aktifkan notifikasi'));
    expect(b.Notification.requestPermission).toHaveBeenCalledTimes(1);
    expect(b.pushManager.subscribe).toHaveBeenCalledTimes(1);
    const post = pushCalls('post')[0];
    expect(post.url).toBe('/api/push/subscribe');
    expect(post.body.endpoint).toBe('https://fcm.googleapis.com/fcm/send/baru');
    expect(post.cfg.headers.Authorization).toBe('Bearer tes-token');
    expect(localStorage.getItem(pushApi.CUSTOMER_PUSH_FLAG)).toBe('pub-1');
    expect(sheet().textContent).toContain('Perangkat ini:Aktif');
    expect(container.querySelector('[data-testid="settings-dot"]')).toBeNull();
    // Tidak memakai rute admin.
    expect(mockState.calls.some((c) => c.url.includes('/api/admin/'))).toBe(false);
  });

  test('matikan: DELETE endpoint; browser tetap berlangganan bila panel admin masih memakai', async () => {
    const b = installBrowser({ permission: 'granted', existing: true });
    localStorage.setItem(pushApi.CUSTOMER_PUSH_FLAG, 'pub-1');
    const sub = b.state.sub;
    mockState.keep = true;
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    expect(container.querySelector('[data-testid="settings-dot"]')).toBeNull();
    // Sinkron diam-diam (upsert) karena pelanggan ini yang mengaktifkan.
    expect(pushCalls('post')).toHaveLength(1);
    await click(gear());
    await click(sheetBtn('Matikan notifikasi'));
    const del = pushCalls('delete')[0];
    expect(del.body).toEqual({ endpoint: 'https://fcm.googleapis.com/fcm/send/lama' });
    expect(sub.unsubscribe).not.toHaveBeenCalled();
    expect(localStorage.getItem(pushApi.CUSTOMER_PUSH_FLAG)).toBeNull();
    expect(sheet().textContent).toContain('Perangkat ini:Belum aktif');
  });

  test('langganan browser milik panel admin (tanpa penanda pelanggan) = belum aktif, tanpa sinkron diam-diam', async () => {
    installBrowser({ permission: 'granted', existing: true });
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    expect(pushCalls('post')).toHaveLength(0);
    expect(container.querySelector('[data-testid="settings-dot"]')).not.toBeNull();
  });

  test.each([
    ['tidak didukung', () => {}],
    ['izin diblokir', () => installBrowser({ permission: 'denied' })],
    ['server nonaktif', () => { installBrowser(); mockState.config = { enabled: false, publicKey: '' }; }],
  ])('tanpa titik: %s', async (_, arrange) => {
    arrange();
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    expect(gear()).not.toBeNull();
    expect(container.querySelector('[data-testid="settings-dot"]')).toBeNull();
    expect(gear().getAttribute('aria-label')).toBe('Pengaturan');
  });

  test('iPhone Safari belum dipasang: tidak didukung + petunjuk; Pasang aplikasi membuka sheet langkah iOS (manfaat pelanggan)', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    expect(container.querySelector('[data-testid="settings-dot"]')).toBeNull();
    await click(gear());
    expect(sheet().textContent).toContain('Tidak didukung di browser ini');
    expect(sheet().textContent).toContain('Layar Utama');
    expect(sheetBtn('Aktifkan notifikasi')).toBeUndefined();
    await click([...sheet().querySelectorAll('button')].find((x) => x.textContent.includes('Pasang aplikasi')));
    expect(sheet()).toBeNull();
    const inst = sheet('customer-install-sheet');
    expect(inst.querySelector('[data-testid="install-steps"]').getAttribute('data-platform')).toBe('ios');
    expect(inst.textContent).not.toContain('panel admin');
    await click(sheetBtn('Tutup', 'customer-install-sheet'));
    expect(sheet('customer-install-sheet')).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  test('Android dengan prompt: Pasang Sekarang memanggil prompt; teks manfaat pelanggan', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await renderEl(<SettingsMenu user={USER} onLogout={() => {}} />);
    const ev = new Event('beforeinstallprompt', { cancelable: true });
    ev.prompt = jest.fn();
    ev.userChoice = Promise.resolve({ outcome: 'accepted' });
    await act(async () => window.dispatchEvent(ev));
    await click(gear());
    await click([...sheet().querySelectorAll('button')].find((x) => x.textContent.includes('Pasang aplikasi')));
    const inst = sheet('customer-install-sheet');
    expect(inst.textContent).toContain('kabar status pesanan');
    await click(sheetBtn('Pasang Sekarang', 'customer-install-sheet'));
    expect(ev.prompt).toHaveBeenCalledTimes(1);
  });

  test('standalone: tanpa item Pasang aplikasi; Logout memanggil onLogout; Escape menutup', async () => {
    standaloneMedia = true;
    setNav('userAgent', UA.android);
    installBrowser();
    const onLogout = jest.fn();
    await renderEl(<SettingsMenu user={USER} onLogout={onLogout} />);
    await click(gear());
    expect([...sheet().querySelectorAll('button')].some((x) => x.textContent.includes('Pasang aplikasi'))).toBe(false);
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    await flush();
    expect(sheet()).toBeNull();
    await click(gear());
    await click(sheetBtn('Logout'));
    expect(onLogout).toHaveBeenCalledTimes(1);
    expect(sheet()).toBeNull();
  });
});

describe('logout melepas langganan pelanggan (best-effort)', () => {
  test('DELETE + unsubscribe browser, penanda dihapus', async () => {
    const b = installBrowser({ permission: 'granted', existing: true });
    localStorage.setItem(pushApi.CUSTOMER_PUSH_FLAG, 'pub-1');
    const sub = b.state.sub;
    await pushApi.cleanupCustomerPush();
    expect(pushCalls('delete')[0].body).toEqual({ endpoint: 'https://fcm.googleapis.com/fcm/send/lama' });
    expect(sub.unsubscribe).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(pushApi.CUSTOMER_PUSH_FLAG)).toBeNull();
  });

  test('server gagal: browser tetap berhenti berlangganan, tidak melempar galat', async () => {
    const b = installBrowser({ permission: 'granted', existing: true });
    const sub = b.state.sub;
    mockState.failDelete = true;
    await expect(pushApi.cleanupCustomerPush()).resolves.toBeUndefined();
    expect(sub.unsubscribe).toHaveBeenCalledTimes(1);
  });

  test('server menggantung: dibatasi waktu, logout tidak tertahan', async () => {
    installBrowser({ permission: 'granted', existing: true });
    mockState.hangDelete = true;
    const t0 = Date.now();
    await pushApi.cleanupCustomerPush(50);
    expect(Date.now() - t0).toBeLessThan(1000);
  });

  test('tanpa dukungan push: langsung selesai', async () => {
    await expect(pushApi.cleanupCustomerPush()).resolves.toBeUndefined();
    expect(pushCalls('delete')).toHaveLength(0);
  });

  test('logout dari navbar: lepas langganan SEBELUM /auth/logout', async () => {
    installBrowser({ permission: 'granted', existing: true });
    localStorage.setItem(pushApi.CUSTOMER_PUSH_FLAG, 'pub-1');
    await renderAppAt('/', USER);
    const logoutBtn = [...container.querySelectorAll('nav button')].find((x) => x.textContent.trim() === 'Logout');
    await click(logoutBtn);
    const idxDel = mockState.calls.findIndex((c) => c.method === 'delete' && c.url.endsWith('/api/push/subscribe'));
    const idxLogout = mockState.calls.findIndex((c) => c.url.endsWith('/auth/logout'));
    expect(idxDel).toBeGreaterThan(-1);
    expect(idxLogout).toBeGreaterThan(idxDel);
    expect(localStorage.getItem('token')).toBeNull();
  });
});

describe('navbar & sheet otomatis pelanggan', () => {
  test('navbar: gear untuk pelanggan login; tombol Logout navbar hanya md ke atas', async () => {
    await renderAppAt('/', USER);
    expect(gear()).not.toBeNull();
    const logoutBtn = [...container.querySelectorAll('nav button')].find((x) => x.textContent.trim() === 'Logout');
    expect(logoutBtn.className.split(/\s+/)).toEqual(expect.arrayContaining(['hidden', 'md:inline-block']));
  });

  test('belum login: tanpa gear dan tanpa sheet otomatis', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await renderAppAt('/');
    expect(gear()).toBeNull();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 1000));
    });
    expect(sheet('auto-install-sheet')).toBeNull();
  });

  test('iPhone: muncul sekali setelah sesi terverifikasi, teks pelanggan; tidak muncul ulang setelah muat ulang', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await renderAppAt('/', USER);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 1000));
    });
    const s = sheet('auto-install-sheet');
    expect(s).not.toBeNull();
    expect(s.querySelector('[data-testid="install-steps"]').getAttribute('data-platform')).toBe('ios');
    expect(sessionStorage.getItem(AUTO_SHEET_SESSION_KEY)).toBe('1');
    await click(s.querySelector('button[aria-label="Tutup"]'));
    act(() => root.unmount());
    container.remove();
    await renderAppAt('/pesanan', USER);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 1000));
    });
    expect(sheet('auto-install-sheet')).toBeNull();
  });

  test('halaman login: tidak muncul', async () => {
    setNav('userAgent', UA.iphoneSafari);
    await renderAppAt('/login', USER);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 1000));
    });
    expect(sheet('auto-install-sheet')).toBeNull();
  });

  test('sesi tidak terverifikasi (token ditolak): tidak muncul', async () => {
    setNav('userAgent', UA.iphoneSafari);
    localStorage.setItem('token', 'tes');
    localStorage.setItem('user', JSON.stringify(USER));
    mockState.me = null; // /auth/me tanpa user -> sesi tidak sah
    window.history.replaceState({}, '', '/');
    await renderEl(<App />);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 1000));
    });
    expect(sheet('auto-install-sheet')).toBeNull();
  });
});

// ---------- Sheet ajakan "Aktifkan notifikasi" (pelanggan) ----------
const AutoNotifySheet = require('../shop/AutoNotifySheet').default;
const { NOTIFY_SHEET_SESSION_KEY, NOTIFY_SHEET_DISMISS_KEY } = require('../shop/AutoNotifySheet');

const notifySheet = () => document.querySelector('[data-testid="auto-notify-sheet"]');
const wait = async (ms = 20) => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
  await flush();
};
// Tunggu sampai sheet muncul (maks. ~1 detik) — langkah asinkron (config + langganan) bisa lebih lambat.
const waitSheet = async () => {
  for (let i = 0; i < 50 && !notifySheet(); i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await wait(20);
  }
};
const standaloneAndroid = () => {
  setNav('userAgent', UA.android);
  standaloneMedia = true;
};

describe('sheet ajakan Aktifkan notifikasi', () => {
  test('aplikasi terpasang (standalone), belum berlangganan: muncul; Aktifkan -> izin setelah ketukan, simpan, tutup, toast', async () => {
    standaloneAndroid();
    const b = installBrowser();
    localStorage.setItem('token', 'tes-token');
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await waitSheet();
    const s = notifySheet();
    expect(s).not.toBeNull();
    expect(document.getElementById(s.querySelector('[role="dialog"]').getAttribute('aria-labelledby')).textContent).toBe('Aktifkan notifikasi');
    expect(s.textContent).toContain('ongkir dikonfirmasi, pembayaran diterima, dan pesanan selesai');
    expect(b.Notification.requestPermission).not.toHaveBeenCalled();
    expect(sessionStorage.getItem(NOTIFY_SHEET_SESSION_KEY)).toBe('1');
    await click([...s.querySelectorAll('button')].find((x) => x.textContent.trim() === 'Aktifkan notifikasi'));
    expect(b.Notification.requestPermission).toHaveBeenCalledTimes(1);
    expect(b.pushManager.subscribe).toHaveBeenCalledTimes(1);
    expect(pushCalls('post')[0].url).toBe('/api/push/subscribe');
    expect(localStorage.getItem(pushApi.CUSTOMER_PUSH_FLAG)).toBe('pub-1');
    expect(notifySheet()).toBeNull();
    expect(container.querySelector('[data-testid="notify-toast"]').textContent).toContain('Notifikasi aktif');
  });

  test('izin ditolak saat diminta: pesan jelas di sheet, Nanti menutup tanpa menyimpan "jangan tampilkan"', async () => {
    standaloneAndroid();
    installBrowser({ grant: 'denied' });
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await waitSheet();
    await click([...notifySheet().querySelectorAll('button')].find((x) => x.textContent.trim() === 'Aktifkan notifikasi'));
    expect(notifySheet().querySelector('[data-testid="notify-sheet-message"]').textContent).toContain('ditolak');
    await click([...notifySheet().querySelectorAll('button')].find((x) => x.textContent.trim() === 'Nanti'));
    expect(notifySheet()).toBeNull();
    expect(localStorage.getItem(NOTIFY_SHEET_DISMISS_KEY)).toBeNull();
  });

  test('Jangan tampilkan lagi: kunci terpisah dari sheet pasang; tidak muncul lagi walau sesi baru', async () => {
    standaloneAndroid();
    installBrowser();
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await waitSheet();
    await click([...notifySheet().querySelectorAll('button')].find((x) => x.textContent.trim() === 'Jangan tampilkan lagi'));
    expect(localStorage.getItem(NOTIFY_SHEET_DISMISS_KEY)).toBe('1');
    expect(localStorage.getItem(push.IOS_GUIDE_DISMISS_KEY)).toBeNull();
    sessionStorage.clear();
    act(() => root.unmount());
    container.remove();
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await wait();
    expect(notifySheet()).toBeNull();
  });

  test('sekali per sesi tab: tidak muncul lagi setelah muat ulang', async () => {
    standaloneAndroid();
    installBrowser();
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await waitSheet();
    await click([...notifySheet().querySelectorAll('button')].find((x) => x.textContent.trim() === 'Nanti'));
    act(() => root.unmount());
    container.remove();
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await wait();
    expect(notifySheet()).toBeNull();
  });

  test.each([
    ['push tidak didukung', () => standaloneAndroid()],
    ['iPhone Safari belum dipasang (pasang didahulukan)', () => { setNav('userAgent', UA.iphoneSafari); installBrowser(); }],
    ['Android belum dipasang (sheet pasang didahulukan)', () => { setNav('userAgent', UA.android); installBrowser(); }],
    ['izin denied', () => { standaloneAndroid(); installBrowser({ permission: 'denied' }); }],
    ['server push nonaktif', () => { standaloneAndroid(); installBrowser(); mockState.config = { enabled: false, publicKey: '' }; }],
    ['sudah berlangganan sebagai pelanggan ini', () => { standaloneAndroid(); installBrowser({ permission: 'granted', existing: true }); localStorage.setItem(pushApi.CUSTOMER_PUSH_FLAG, 'pub-1'); }],
    ['sheet pasang sudah tampil di sesi ini', () => { standaloneAndroid(); installBrowser(); sessionStorage.setItem(AUTO_SHEET_SESSION_KEY, '1'); }],
  ])('tidak muncul: %s', async (_, arrange) => {
    arrange();
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await wait();
    expect(notifySheet()).toBeNull();
  });

  test('langganan browser milik akun lain/admin (tanpa penanda pelanggan ini): tetap mengajak', async () => {
    standaloneAndroid();
    installBrowser({ permission: 'granted', existing: true });
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await waitSheet();
    expect(notifySheet()).not.toBeNull();
  });

  test('tidak menimpa dialog lain', async () => {
    standaloneAndroid();
    installBrowser();
    const other = document.createElement('div');
    other.setAttribute('role', 'dialog');
    other.setAttribute('aria-modal', 'true');
    document.body.appendChild(other);
    await renderEl(<AutoNotifySheet user={USER} delayMs={0} />);
    await wait();
    expect(notifySheet()).toBeNull();
    expect(sessionStorage.getItem(NOTIFY_SHEET_SESSION_KEY)).toBeNull();
    other.remove();
  });

  test('di App: standalone -> hanya sheet notifikasi; Aktifkan menghilangkan titik gear', async () => {
    standaloneAndroid();
    installBrowser();
    await renderAppAt('/', USER);
    await wait(900);
    await waitSheet();
    expect(document.querySelector('[data-testid="auto-install-sheet"]')).toBeNull();
    expect(notifySheet()).not.toBeNull();
    expect(container.querySelector('[data-testid="settings-dot"]')).not.toBeNull();
    await click([...notifySheet().querySelectorAll('button')].find((x) => x.textContent.trim() === 'Aktifkan notifikasi'));
    await wait();
    expect(container.querySelector('[data-testid="settings-dot"]')).toBeNull();
    expect(gear().getAttribute('aria-label')).toBe('Pengaturan');
  });

  test('di App: Android belum dipasang -> hanya sheet pasang (tidak dua sheet)', async () => {
    setNav('userAgent', UA.android);
    installBrowser();
    await renderAppAt('/', USER);
    await wait(1000);
    expect(document.querySelector('[data-testid="auto-install-sheet"]')).not.toBeNull();
    expect(notifySheet()).toBeNull();
    expect(document.querySelectorAll('[role="dialog"][aria-modal="true"]')).toHaveLength(1);
  });
});
