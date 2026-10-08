// Notifikasi push PELANGGAN (status pesanan miliknya sendiri). Rute /api/push/* dengan sesi Bearer.
// Logika browser (service worker, subscribe) memakai ulang admin/push.js.
import axios from 'axios';
import { API_BASE_URL, authHeader } from '../auth';
import { currentSubscription, isPushSupported, storageGet, storageSet, subscribeDevice, unsubscribeDevice } from '../admin/push';

export const customerPushApi = {
  fetchConfig: () => axios.get(`${API_BASE_URL}/push/public-key`, { headers: authHeader() }).then((r) => r.data),
  save: (body) => axios.post(`${API_BASE_URL}/push/subscribe`, body, { headers: authHeader() }).then((r) => r.data),
  remove: (endpoint) => axios.delete(`${API_BASE_URL}/push/subscribe`, { headers: authHeader(), data: { endpoint } }).then((r) => r.data),
};

// Penanda lokal "notifikasi pelanggan aktif di perangkat ini untuk akun <id>". Satu browser punya satu
// langganan push yang bisa juga dipakai panel admin, jadi status pelanggan dicatat terpisah.
export const CUSTOMER_PUSH_FLAG = 'mihan.push.customer';

export const customerFlagFor = (user) => (user && (user.id || user.username) ? String(user.id || user.username) : '');
export const isCustomerFlagged = (user) => !!customerFlagFor(user) && storageGet(CUSTOMER_PUSH_FLAG) === customerFlagFor(user);
// Dikirim ke window setiap status notifikasi pelanggan berubah (menu gear & sheet ajakan tetap sinkron).
export const CUSTOMER_PUSH_EVENT = 'mihan-customer-push-changed';
const announce = () => {
  try {
    window.dispatchEvent(new Event(CUSTOMER_PUSH_EVENT));
  } catch {
    /* abaikan */
  }
};
export const setCustomerFlag = (user) => {
  storageSet(CUSTOMER_PUSH_FLAG, customerFlagFor(user));
  announce();
};
export const clearCustomerFlag = () => {
  try {
    window.localStorage.removeItem(CUSTOMER_PUSH_FLAG);
  } catch {
    /* abaikan */
  }
  announce();
};

// enableCustomerPush: alur aktifkan yang sama untuk menu gear dan sheet ajakan. HARUS dipanggil langsung
// dari ketukan pengguna (izin diminta pertama kali, sebelum await lain). Hasil:
//   {ok: true} | {ok: false, permission: 'denied'|'default'} | {ok: false, disabled: true}; galat lain dilempar.
export const enableCustomerPush = async (user, knownConfig) => {
  const permission = await window.Notification.requestPermission();
  if (permission !== 'granted') return { ok: false, permission };
  const cfg = knownConfig?.publicKey ? knownConfig : await customerPushApi.fetchConfig();
  if (!cfg?.enabled || !cfg.publicKey) return { ok: false, permission, disabled: true, config: cfg };
  await subscribeDevice(cfg.publicKey, customerPushApi);
  setCustomerFlag(user);
  return { ok: true, permission };
};

const withTimeout = (p, ms) =>
  Promise.race([p, new Promise((_, reject) => setTimeout(() => reject(new Error('waktu habis')), ms))]);

// Dipanggil SEBELUM logout (token masih sah): hapus langganan pelanggan perangkat ini di server dan
// berhenti berlangganan di browser (kecuali endpoint masih dipakai panel admin). Best-effort: tidak pernah
// melempar galat dan dibatasi waktu agar logout tidak tertahan.
export const cleanupCustomerPush = async (timeoutMs = 3000) => {
  clearCustomerFlag();
  if (!isPushSupported()) return;
  try {
    await withTimeout(
      (async () => {
        const sub = await currentSubscription();
        if (sub) await unsubscribeDevice(customerPushApi);
      })(),
      timeoutMs
    );
  } catch {
    /* best-effort: logout tetap jalan */
  }
};
