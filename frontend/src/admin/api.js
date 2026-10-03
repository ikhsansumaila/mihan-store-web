// Panggilan API admin. Autentikasi dilakukan Cloudflare Access (cookie CF_Authorization
// ditambahkan header Cf-Access-Jwt-Assertion oleh Cloudflare), jadi tidak ada token di sini.
// Semua permintaan menyertakan X-Requested-With: mihanstore-admin (perlindungan CSRF backend).

export const ADMIN_HEADER = { 'X-Requested-With': 'mihanstore-admin' };

export class AdminSessionError extends Error {
  constructor(msg) {
    super(msg || 'Sesi admin berakhir, muat ulang halaman');
    this.sessionExpired = true;
  }
}

export async function adminFetch(path, { method = 'GET', body } = {}) {
  const opts = {
    method,
    credentials: 'same-origin',
    redirect: 'manual', // redirect login Cloudflare Access = sesi berakhir
    headers: { ...ADMIN_HEADER, Accept: 'application/json' },
  };
  if (method !== 'GET') {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body ?? {});
  }
  let res;
  try {
    res = await fetch(`/api/admin${path}`, opts);
  } catch {
    // Redirect lintas domain ke halaman login Cloudflare juga berakhir di sini.
    throw new AdminSessionError();
  }
  if (res.type === 'opaqueredirect' || res.status === 0) throw new AdminSessionError();
  const ct = res.headers.get('content-type') || '';
  if (!ct.includes('application/json')) throw new AdminSessionError();
  const data = await res.json().catch(() => null);
  if (res.status === 401) throw new AdminSessionError();
  if (!res.ok) {
    const err = new Error(data?.error || `Permintaan gagal (${res.status})`);
    err.status = res.status;
    err.data = data; // mis. tierErrors (422) untuk ditampilkan per baris jenjang
    throw err;
  }
  return data;
}

export const qs = (params) => {
  const u = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') u.set(k, v);
  });
  const s = u.toString();
  return s ? `?${s}` : '';
};

// Format rupiah bersama (satu tempat: shop/format.js).
export { rupiah, perUnit } from '../shop/format';

export const fmtTime = (iso) => {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short' });
  } catch {
    return iso;
  }
};
