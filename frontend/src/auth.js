import axios from 'axios';

export const API_BASE_URL = '/api';
export const TURNSTILE_SITE_KEY = process.env.REACT_APP_TURNSTILE_SITE_KEY || '';

// Token disimpan di localStorage seperti sebelumnya. Jangan pernah mencetaknya ke console.
export const getToken = () => localStorage.getItem('token');

export const saveSession = (token, user) => {
  if (token) localStorage.setItem('token', token);
  localStorage.setItem('user', JSON.stringify(user));
};

export const clearSession = () => {
  localStorage.removeItem('token');
  localStorage.removeItem('user');
};

export const getStoredUser = () => {
  try {
    const raw = localStorage.getItem('user');
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
};

export const authHeader = (token = getToken()) =>
  token ? { Authorization: `Bearer ${token}` } : {};

// Periksa token ke server. Mengembalikan:
//  { valid: true, user }  -> sesi aktif
//  { valid: false }       -> token ditolak (401), sesi lokal harus dihapus
//  { valid: null }        -> server tidak bisa dihubungi, jangan hapus sesi lokal
export const verifySession = async () => {
  const token = getToken();
  if (!token) return { valid: false };
  try {
    const res = await axios.post(`${API_BASE_URL}/auth/verify`, null, { headers: authHeader(token) });
    return { valid: !!res.data?.valid, user: res.data?.user };
  } catch (err) {
    if (err.response?.status === 401) return { valid: false };
    return { valid: null };
  }
};

export const logoutRequest = async () => {
  const token = getToken();
  if (!token) return;
  try {
    await axios.post(`${API_BASE_URL}/auth/logout`, null, { headers: authHeader(token) });
  } catch {
    // Abaikan: sesi lokal tetap dihapus.
  }
};

// Pesan error yang ramah untuk ditampilkan.
export const errorMessage = (err, fallback) => {
  const status = err.response?.status;
  if (err.response?.data?.error) return err.response.data.error;
  if (status === 429) return 'Terlalu banyak percobaan. Coba lagi beberapa saat lagi.';
  if (status === 413) return 'Data yang dikirim terlalu besar.';
  if (!err.response) return 'Tidak dapat terhubung ke server. Periksa koneksi internet Anda.';
  return fallback;
};
