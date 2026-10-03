import React, { useState, useRef } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import axios from 'axios';
import { Turnstile } from '@marsidev/react-turnstile';
import { normalizePhone, phoneError } from '../phone';
import { API_BASE_URL, TURNSTILE_SITE_KEY, saveSession, errorMessage, takeReturnTo } from '../auth';
import GoogleSignIn from './GoogleSignIn';

const USERNAME_RE = /^[a-z0-9_]{3,30}$/;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// Validasi ringan di sisi klien (validasi utama tetap di server).
const validate = ({ name, username, email, phone, password }) => {
  if (!name.trim()) return 'Nama wajib diisi';
  if (name.trim().length > 100) return 'Nama maksimal 100 karakter';
  if (!USERNAME_RE.test(username.trim().toLowerCase()))
    return 'Username harus 3–30 karakter, hanya huruf kecil, angka, atau garis bawah (_)';
  if (!EMAIL_RE.test(email.trim()) || email.trim().length > 254) return 'Format email tidak valid';
  if (phoneError(phone)) return phoneError(phone);
  if (password.length < 8 || password.length > 128) return 'Password harus 8–128 karakter';
  return '';
};

const Register = ({ onRegisterSuccess }) => {
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState(null);
  const turnstileRef = useRef(null);

  const resetTurnstile = () => {
    setTurnstileToken(null);
    if (turnstileRef.current) turnstileRef.current.reset();
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');

    const msg = validate({ name, username, email, phone, password });
    if (msg) {
      setError(msg);
      return;
    }
    if (!turnstileToken) {
      setError('Harap selesaikan verifikasi keamanan');
      return;
    }

    setLoading(true);
    try {
      const response = await axios.post(`${API_BASE_URL}/auth/register`, {
        username: username.trim().toLowerCase(),
        email: email.trim(),
        phone: normalizePhone(phone).phone,
        name: name.trim(),
        password,
        turnstileToken,
      });

      if (response.data.success) {
        if (response.data.token) {
          saveSession(response.data.token, response.data.user);
          onRegisterSuccess(response.data.user);
          navigate(takeReturnTo());
        } else {
          navigate('/login');
        }
      } else {
        setError(response.data.message || 'Registrasi gagal');
        resetTurnstile();
      }
    } catch (err) {
      setError(errorMessage(err, 'Terjadi kesalahan saat registrasi'));
      resetTurnstile();
    } finally {
      setLoading(false);
    }
  };

  const inputClass =
    'w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition';

  return (
    <div className="min-h-[calc(100vh-72px)] flex items-center justify-center px-4 py-12 bg-gray-50">
      <div className="w-full max-w-md bg-white rounded-2xl shadow-xl p-8">
        <h2 className="text-3xl font-bold text-center text-gray-800 mb-8">Daftar</h2>

        {error && (
          <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-5">
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Nama Lengkap</label>
            <input
              type="text"
              autoComplete="name"
              maxLength={100}
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Username</label>
            <input
              type="text"
              autoComplete="username"
              autoCapitalize="none"
              spellCheck="false"
              maxLength={30}
              className={inputClass}
              value={username}
              onChange={(e) => setUsername(e.target.value.toLowerCase())}
              required
            />
            <p className="mt-1 text-xs text-gray-500">3–30 karakter: huruf kecil, angka, atau garis bawah (_).</p>
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Email</label>
            <input
              type="email"
              autoComplete="email"
              maxLength={254}
              className={inputClass}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">
              No. Telepon/WhatsApp <span className="font-normal text-gray-500">(opsional)</span>
            </label>
            <input
              type="tel"
              autoComplete="tel"
              inputMode="tel"
              maxLength={32}
              placeholder="08123456789"
              className={inputClass}
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
            />
            <p className="mt-1 text-xs text-gray-500">Format: 08xx, 628xx, atau +628xx. Boleh dikosongkan.</p>
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Password</label>
            <input
              type="password"
              autoComplete="new-password"
              minLength={8}
              maxLength={128}
              className={inputClass}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            <p className="mt-1 text-xs text-gray-500">Minimal 8 karakter.</p>
          </div>

          <div className="flex justify-center my-4">
            {TURNSTILE_SITE_KEY ? (
              <Turnstile
                ref={turnstileRef}
                siteKey={TURNSTILE_SITE_KEY}
                options={{ language: 'id' }}
                onSuccess={(token) => setTurnstileToken(token)}
                onError={() => setError('Verifikasi keamanan gagal dimuat, coba muat ulang halaman')}
                onExpire={() => setTurnstileToken(null)}
              />
            ) : (
              <div className="w-full rounded-lg border border-yellow-200 bg-yellow-50 px-4 py-3 text-sm text-yellow-800">
                Verifikasi keamanan belum dikonfigurasi. Pendaftaran sementara tidak tersedia.
              </div>
            )}
          </div>

          <button
            type="submit"
            className="w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-60 disabled:cursor-not-allowed transition"
            disabled={loading || !TURNSTILE_SITE_KEY}
          >
            {loading ? 'Sedang mendaftar...' : 'Daftar'}
          </button>
        </form>

        <GoogleSignIn onLoginSuccess={onRegisterSuccess} />

        <div className="mt-6 text-center text-sm text-gray-600">
          Sudah punya akun?{' '}
          <Link to="/login" className="font-semibold text-purple-700 hover:text-purple-900">
            Login di sini
          </Link>
        </div>
      </div>
    </div>
  );
};

export default Register;
