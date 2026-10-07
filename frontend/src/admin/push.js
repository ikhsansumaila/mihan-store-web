// Web Push admin: deteksi dukungan/platform, service worker, langganan push.
// Service worker (public/sw.js) HANYA menampilkan notifikasi; tidak meng-cache apa pun.
import { adminFetch } from './api';

export const SW_URL = '/sw.js';
export const IOS_GUIDE_DISMISS_KEY = 'mihan.iosInstallGuide.dismissed';

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

export const fetchPushConfig = () => adminFetch('/push/public-key');

export const sendSubscription = (sub) => adminFetch('/push/subscribe', { method: 'POST', body: sub.toJSON ? sub.toJSON() : sub });

// subscribeDevice: dipanggil SETELAH izin "granted". Mendaftarkan service worker, berlangganan dengan
// kunci publik server (langganan lama dengan kunci lain diganti), lalu menyimpan di server.
export const subscribeDevice = async (publicKey) => {
  const reg = await registerSW();
  const ready = (await navigator.serviceWorker.ready) || reg;
  const key = urlBase64ToUint8Array(publicKey);
  let sub = await ready.pushManager.getSubscription();
  if (sub && sub.options && sub.options.applicationServerKey && !sameKey(sub.options.applicationServerKey, key)) {
    await sub.unsubscribe();
    sub = null;
  }
  if (!sub) sub = await ready.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
  await sendSubscription(sub);
  return sub;
};

// unsubscribeDevice: berhenti berlangganan di browser dan hapus dari server.
export const unsubscribeDevice = async () => {
  const sub = await currentSubscription();
  if (!sub) return false;
  const { endpoint } = sub;
  try {
    await sub.unsubscribe();
  } finally {
    await adminFetch('/push/subscribe', { method: 'DELETE', body: { endpoint } });
  }
  return true;
};

export const sendTestPush = () => adminFetch('/push/test', { method: 'POST', body: {} });
