// Notifikasi push PELANGGAN (status pesanan miliknya sendiri). Rute /api/push/* dengan sesi Bearer.
// Logika browser (service worker, subscribe) memakai ulang admin/push.js.
import axios from 'axios';
import { API_BASE_URL, authHeader } from '../auth';
import { currentSubscription, isPushSupported, storageGet, storageSet, unsubscribeDevice } from '../admin/push';

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
export const setCustomerFlag = (user) => storageSet(CUSTOMER_PUSH_FLAG, customerFlagFor(user));
export const clearCustomerFlag = () => {
  try {
    window.localStorage.removeItem(CUSTOMER_PUSH_FLAG);
  } catch {
    /* abaikan */
  }
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
