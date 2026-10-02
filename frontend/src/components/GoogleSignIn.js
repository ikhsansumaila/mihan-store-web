import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { API_BASE_URL, GOOGLE_CLIENT_ID, saveSession, errorMessage } from '../auth';

// Skrip Google Identity Services hanya dimuat bila REACT_APP_GOOGLE_CLIENT_ID terisi.
let gisPromise = null;
const loadGis = () => {
  if (window.google?.accounts?.id) return Promise.resolve();
  if (!gisPromise) {
    gisPromise = new Promise((resolve, reject) => {
      const s = document.createElement('script');
      s.src = 'https://accounts.google.com/gsi/client';
      s.async = true;
      s.defer = true;
      s.onload = () => resolve();
      s.onerror = () => {
        gisPromise = null;
        reject(new Error('gagal memuat'));
      };
      document.head.appendChild(s);
    });
  }
  return gisPromise;
};

// Tombol "Masuk dengan Google". Tidak dirender sama sekali bila client ID kosong.
const GoogleSignIn = ({ onLoginSuccess }) => {
  const navigate = useNavigate();
  const btnRef = useRef(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const callbackRef = useRef(null);

  callbackRef.current = async (resp) => {
    setError('');
    setBusy(true);
    try {
      const res = await axios.post(`${API_BASE_URL}/auth/google`, { credential: resp.credential });
      const data = res.data || {};
      if (data.needsProfile && data.profileToken) {
        // profileToken hanya disimpan di memori (state navigasi), tidak di localStorage.
        navigate('/lengkapi-profil', {
          state: { profileToken: data.profileToken, profile: data.profile, suggestedUsername: data.suggestedUsername },
        });
        return;
      }
      if (data.success && data.token) {
        saveSession(data.token, data.user);
        onLoginSuccess?.(data.user);
        navigate('/');
        return;
      }
      setError('Login Google gagal, coba lagi.');
    } catch (err) {
      setError(errorMessage(err, 'Login Google gagal, coba lagi.'));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    if (!GOOGLE_CLIENT_ID) return undefined;
    let cancelled = false;
    loadGis()
      .then(() => {
        if (cancelled || !btnRef.current) return;
        window.google.accounts.id.initialize({
          client_id: GOOGLE_CLIENT_ID,
          callback: (r) => callbackRef.current(r),
          ux_mode: 'popup',
          auto_select: false,
          cancel_on_tap_outside: true,
        });
        const width = Math.min(400, Math.max(200, btnRef.current.offsetWidth || 320));
        window.google.accounts.id.renderButton(btnRef.current, {
          type: 'standard',
          theme: 'outline',
          size: 'large',
          text: 'signin_with',
          shape: 'rectangular',
          locale: 'id',
          width,
        });
      })
      .catch(() => {
        if (!cancelled) setError('Tombol Google gagal dimuat. Periksa koneksi lalu muat ulang halaman.');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (!GOOGLE_CLIENT_ID) return null;

  return (
    <div className="mt-6">
      <div className="flex items-center gap-3 mb-4">
        <div className="h-px flex-1 bg-gray-200" />
        <span className="text-xs uppercase tracking-wide text-gray-500">atau</span>
        <div className="h-px flex-1 bg-gray-200" />
      </div>
      <p className="text-center text-sm font-semibold text-gray-700 mb-3">Masuk dengan Google</p>
      <div ref={btnRef} className="flex justify-center w-full min-h-[44px]" />
      {busy && <p className="mt-2 text-center text-sm text-gray-500">Memproses login Google...</p>}
      {error && (
        <div className="mt-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
      )}
    </div>
  );
};

export default GoogleSignIn;
