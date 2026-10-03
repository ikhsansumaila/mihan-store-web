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

// Unggah berkas (multipart/form-data, field "file") ke /api/admin{path} dengan kemajuan unggah.
// XMLHttpRequest dipakai karena fetch belum menyediakan kemajuan unggah. Galat sama seperti adminFetch:
// sesi Access berakhir -> AdminSessionError; galat server -> Error(pesan server) dengan .status.
export const adminUpload = (path, blob, { filename = 'foto.jpg', onProgress, method = 'POST' } = {}) =>
  new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, `/api/admin${path}`);
    xhr.withCredentials = true;
    xhr.setRequestHeader('X-Requested-With', ADMIN_HEADER['X-Requested-With']);
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.responseType = 'text';
    if (onProgress && xhr.upload) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && e.total > 0) onProgress(Math.min(100, Math.round((e.loaded / e.total) * 100)));
      };
    }
    // Pengalihan lintas domain ke login Cloudflare Access berakhir sebagai galat jaringan.
    xhr.onerror = () => reject(new AdminSessionError());
    xhr.ontimeout = () => reject(new Error('Unggahan terlalu lama. Periksa koneksi lalu coba lagi.'));
    xhr.timeout = 120000;
    xhr.onload = () => {
      const ct = xhr.getResponseHeader('content-type') || '';
      if (xhr.status === 0 || !ct.includes('application/json')) {
        reject(xhr.status === 413 ? Object.assign(new Error('Ukuran foto melebihi 2 MB.'), { status: 413 }) : new AdminSessionError());
        return;
      }
      let data = null;
      try {
        data = JSON.parse(xhr.responseText);
      } catch {
        data = null;
      }
      if (xhr.status === 401) {
        reject(new AdminSessionError());
        return;
      }
      if (xhr.status < 200 || xhr.status >= 300) {
        const err = new Error(data?.error || `Unggahan gagal (${xhr.status})`);
        err.status = xhr.status;
        reject(err);
        return;
      }
      resolve(data);
    };
    const fd = new FormData();
    fd.append('file', blob, filename);
    xhr.send(fd);
  });
