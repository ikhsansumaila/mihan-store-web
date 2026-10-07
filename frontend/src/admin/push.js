// Web Push admin: deteksi dukungan/platform, service worker, langganan push.
// Service worker (public/sw.js) HANYA menampilkan notifikasi; tidak meng-cache apa pun.
import { adminFetch } from './api';

export const SW_URL = '/sw.js';
export const IOS_GUIDE_DISMISS_KEY = 'mihan.iosInstallGuide.dismissed';
// Dikirim ke window saat "Jangan tampilkan lagi" dipilih, agar semua tombol/sheet pasang ikut tersembunyi.
export const INSTALL_DISMISSED_EVENT = 'mihan-install-dismissed';

const nav = () => (typeof navigator !== 'undefined' ? navigator : {});

// ---------- Deteksi ----------

export const isPushSupported = () =>
  typeof window !== 'undefined' &&
  'serviceWorker' in nav() &&
  'PushManager' in window &&
  'Notification' in window;

export const notificationPermission = () =>
  typeof window !== 'undefined' && 'Notification' in window ? window.Notification.permission : 'unsupported';

// iPhone/iPad (termasuk iPadOS yang mengaku "Macintosh" tetapi layar sentuh).
export const isIOS = (ua = nav().userAgent || '', platform = nav().platform || '', touch = nav().maxTouchPoints || 0) =>
  /iPad|iPhone|iPod/.test(ua) || (platform === 'MacIntel' && touch > 1) || (/Macintosh/.test(ua) && touch > 1);

export const isAndroid = (ua = nav().userAgent || '') => /Android/i.test(ua);

// Browser lain di iOS (Chrome, Firefox, Edge, Opera). Panduan menyarankan Safari.
export const isIOSOtherBrowser = (ua = nav().userAgent || '') => /CriOS|FxiOS|EdgiOS|OPiOS|GSA\//.test(ua);

export const isStandalone = () => {
  if (nav().standalone === true) return true;
  try {
    return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(display-mode: standalone)').matches;
  } catch {
    return false;
  }
};

// iOS di tab browser biasa: Web Push hanya tersedia setelah dipasang ke Layar Utama (iOS 16.4+).
export const needsIOSInstall = () => isIOS() && !isStandalone();

// ---------- localStorage aman ----------

export const storageGet = (k) => {
  try {
    return window.localStorage.getItem(k);
  } catch {
    return null;
  }
};

export const storageSet = (k, v) => {
  try {
    window.localStorage.setItem(k, v);
  } catch {
    /* mode privat / penyimpanan diblokir: abaikan */
  }
};

export const sessionGet = (k) => {
  try {
    return window.sessionStorage.getItem(k);
  } catch {
    return null;
  }
};

export const sessionSet = (k, v) => {
  try {
    window.sessionStorage.setItem(k, v);
  } catch {
    /* penyimpanan diblokir: abaikan */
  }
};

// ---------- beforeinstallprompt (Android Chrome/Edge) ----------
// Event bisa muncul sebelum komponen dipasang, jadi ditangkap sejak modul dimuat.

let deferredInstall = null;
const installListeners = new Set();

export const onInstallAvailable = (fn) => {
  installListeners.add(fn);
  return () => installListeners.delete(fn);
};
export const installAvailable = () => deferredInstall !== null;

export const handleBeforeInstallPrompt = (e) => {
  e.preventDefault();
  deferredInstall = e;
  installListeners.forEach((fn) => fn(true));
};

export const promptInstall = async () => {
  const e = deferredInstall;
  if (!e) return null;
  deferredInstall = null;
  installListeners.forEach((fn) => fn(false));
  e.prompt();
  try {
    const choice = await e.userChoice;
    return choice?.outcome || null;
  } catch {
    return null;
  }
};

if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
  window.addEventListener('beforeinstallprompt', handleBeforeInstallPrompt);
  window.addEventListener('appinstalled', () => {
    deferredInstall = null;
    installListeners.forEach((fn) => fn(false));
  });
}

// KHUSUS TES: kosongkan status prompt pemasangan.
export const __resetInstallForTests = () => {
  deferredInstall = null;
  installListeners.clear();
};

// ---------- Service worker & langganan ----------

export const urlBase64ToUint8Array = (b64) => {
  const pad = '='.repeat((4 - (b64.length % 4)) % 4);
  const raw = window.atob((b64 + pad).replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) out[i] = raw.charCodeAt(i);
  return out;
};

const sameKey = (a, b) => {
  if (!a || !b) return false;
  const x = new Uint8Array(a);
  const y = new Uint8Array(b);
  if (x.length !== y.length) return false;
  for (let i = 0; i < x.length; i += 1) if (x[i] !== y[i]) return false;
  return true;
};

export const getRegistration = () => navigator.serviceWorker.getRegistration('/');

export const registerSW = () => navigator.serviceWorker.register(SW_URL, { scope: '/', updateViaCache: 'none' });

export const currentSubscription = async () => {
  const reg = await getRegistration();
  if (!reg || !reg.pushManager) return null;
  return reg.pushManager.getSubscription();
};

// API langganan per audiens. Admin: /api/admin/push/* (Cloudflare Access + CSRF, lewat adminFetch).
// Pelanggan: /api/push/* (sesi Bearer), lihat shop/pushApi.js. Bentuk sama: {fetchConfig, save, remove}.
export const adminPushApi = {
  fetchConfig: () => adminFetch('/push/public-key'),
  save: (body) => adminFetch('/push/subscribe', { method: 'POST', body }),
  remove: (endpoint) => adminFetch('/push/subscribe', { method: 'DELETE', body: { endpoint } }),
};

export const fetchPushConfig = () => adminPushApi.fetchConfig();

const subJSON = (sub) => (sub && typeof sub.toJSON === 'function' ? sub.toJSON() : sub);

export const sendSubscription = (sub, api = adminPushApi) => api.save(subJSON(sub));

// subscribeDevice: dipanggil SETELAH izin "granted". Mendaftarkan service worker, berlangganan dengan
// kunci publik server (langganan lama dengan kunci lain diganti), lalu menyimpan di server (audiens api).
export const subscribeDevice = async (publicKey, api = adminPushApi) => {
  const reg = await registerSW();
  const ready = (await navigator.serviceWorker.ready) || reg;
  const key = urlBase64ToUint8Array(publicKey);
  let sub = await ready.pushManager.getSubscription();
  if (sub && sub.options && sub.options.applicationServerKey && !sameKey(sub.options.applicationServerKey, key)) {
    await sub.unsubscribe();
    sub = null;
  }
  if (!sub) sub = await ready.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
  await sendSubscription(sub, api);
  return sub;
};

// unsubscribeDevice: hapus langganan audiens ini di server, lalu berhenti berlangganan di browser KECUALI
// server menyatakan endpoint yang sama masih dipakai audiens lain (admin & pelanggan di browser yang sama).
// Bila server gagal dihubungi, browser tetap berhenti berlangganan (lebih aman), lalu galat dilempar.
export const unsubscribeDevice = async (api = adminPushApi) => {
  const sub = await currentSubscription();
  if (!sub) return false;
  const { endpoint } = sub;
  let keep = false;
  let error = null;
  try {
    const r = await api.remove(endpoint);
    keep = !!(r && r.keepBrowserSubscription);
  } catch (e) {
    error = e;
  }
  if (!keep) await sub.unsubscribe();
  if (error) throw error;
  return true;
};

export const sendTestPush = () => adminFetch('/push/test', { method: 'POST', body: {} });
