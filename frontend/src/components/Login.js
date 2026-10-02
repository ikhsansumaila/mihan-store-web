import React, { useState, useRef } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import axios from 'axios';
import { Turnstile } from '@marsidev/react-turnstile';
import { API_BASE_URL, TURNSTILE_SITE_KEY, saveSession, errorMessage, takeReturnTo } from '../auth';
import GoogleSignIn from './GoogleSignIn';

const Login = ({ onLoginSuccess }) => {
  const navigate = useNavigate();
  const [identifier, setIdentifier] = useState('');
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

    if (!identifier.trim() || !password) {
      setError('Email/username dan password wajib diisi');
      return;
    }
    if (!turnstileToken) {
      setError('Harap selesaikan verifikasi keamanan');
      return;
    }

    setLoading(true);
    try {
      const response = await axios.post(`${API_BASE_URL}/auth/login`, {
        identifier: identifier.trim(),
        password,
        turnstileToken,
      });

      if (response.data.success) {
        saveSession(response.data.token, response.data.user);
        onLoginSuccess(response.data.user);
        navigate(takeReturnTo());
      } else {
        setError(response.data.message || 'Login gagal');
        resetTurnstile();
      }
    } catch (err) {
      setError(errorMessage(err, 'Terjadi kesalahan saat login'));
      resetTurnstile();
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-[calc(100vh-72px)] flex items-center justify-center px-4 py-12 bg-gray-50">
      <div className="w-full max-w-md bg-white rounded-2xl shadow-xl p-8">
        <h2 className="text-3xl font-bold text-center text-gray-800 mb-8">Login</h2>

        {error && (
          <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-5">
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Email atau username</label>
            <input
              type="text"
              autoComplete="username"
              autoCapitalize="none"
              spellCheck="false"
              maxLength={254}
              className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Password</label>
            <input
              type="password"
              autoComplete="current-password"
              maxLength={128}
              className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
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
                Verifikasi keamanan belum dikonfigurasi. Login sementara tidak tersedia.
              </div>
            )}
          </div>

          <button
            type="submit"
            className="w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-60 disabled:cursor-not-allowed transition"
            disabled={loading || !TURNSTILE_SITE_KEY}
          >
            {loading ? 'Sedang login...' : 'Login'}
          </button>
        </form>

        <GoogleSignIn onLoginSuccess={onLoginSuccess} />

        <div className="mt-6 text-center text-sm text-gray-600">
          Belum punya akun?{' '}
          <Link to="/register" className="font-semibold text-purple-700 hover:text-purple-900">
            Daftar di sini
          </Link>
        </div>
      </div>
    </div>
  );
};

export default Login;
