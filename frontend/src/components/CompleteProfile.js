import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import axios from 'axios';
import { API_BASE_URL, saveSession, errorMessage } from '../auth';

const USERNAME_RE = /^[a-z0-9_]{3,30}$/;

// Layar "Lengkapi profil" setelah login Google pertama kali.
const CompleteProfile = ({ onLoginSuccess }) => {
  const navigate = useNavigate();
  const { state } = useLocation();
  const profileToken = state?.profileToken;
  const profile = state?.profile || {};
  const [username, setUsername] = useState(state?.suggestedUsername || '');
  const [phone, setPhone] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const inputClass =
    'w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition';

  if (!profileToken) {
    return (
      <div className="min-h-[calc(100vh-72px)] flex items-center justify-center px-4 py-12 bg-gray-50">
        <div className="w-full max-w-md bg-white rounded-2xl shadow-xl p-8 text-center">
          <h2 className="text-2xl font-bold text-gray-800 mb-4">Lengkapi profil</h2>
          <p className="text-gray-600 mb-6">Sesi pengisian profil tidak ditemukan atau sudah berakhir.</p>
          <Link to="/login" className="font-semibold text-purple-700 hover:text-purple-900">
            Kembali ke halaman login
          </Link>
        </div>
      </div>
    );
  }

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    const u = username.trim().toLowerCase();
    if (!USERNAME_RE.test(u)) {
      setError('Username harus 3–30 karakter, hanya huruf kecil, angka, atau garis bawah (_)');
      return;
    }
    if (phone.trim()) {
      const p = phone.replace(/[\s\-.()]/g, '');
      if (!/^(\+62|62|0)8\d{5,12}$/.test(p)) {
        setError('Nomor telepon tidak valid. Gunakan format 08xx, 628xx, atau +628xx');
        return;
      }
    }
    setLoading(true);
    try {
      const res = await axios.post(`${API_BASE_URL}/auth/google/complete`, {
        profileToken,
        username: u,
        phone: phone.trim(),
      });
      if (res.data?.success && res.data.token) {
        saveSession(res.data.token, res.data.user);
        onLoginSuccess?.(res.data.user);
        navigate('/', { replace: true });
      } else {
        setError('Pendaftaran gagal, coba lagi.');
      }
    } catch (err) {
      setError(errorMessage(err, 'Pendaftaran gagal, coba lagi.'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-[calc(100vh-72px)] flex items-center justify-center px-4 py-12 bg-gray-50">
      <div className="w-full max-w-md bg-white rounded-2xl shadow-xl p-8">
        <h2 className="text-3xl font-bold text-center text-gray-800 mb-2">Lengkapi profil</h2>
        <p className="text-center text-sm text-gray-600 mb-6">Satu langkah lagi untuk membuat akun Mihan Store.</p>

        <div className="flex items-center gap-3 rounded-lg bg-gray-50 border border-gray-200 p-4 mb-6">
          {profile.avatarUrl ? (
            <img src={profile.avatarUrl} alt="" referrerPolicy="no-referrer" className="h-12 w-12 rounded-full" />
          ) : (
            <div className="h-12 w-12 rounded-full bg-purple-200" />
          )}
          <div className="min-w-0">
            <div className="font-semibold text-gray-800 truncate">{profile.name || '(tanpa nama)'}</div>
            <div className="text-sm text-gray-600 truncate">{profile.email}</div>
          </div>
        </div>

        {error && (
          <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        )}

        <form onSubmit={handleSubmit} className="space-y-5">
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Nama</label>
            <input type="text" className={`${inputClass} bg-gray-100`} value={profile.name || ''} readOnly />
          </div>
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Email</label>
            <input type="email" className={`${inputClass} bg-gray-100`} value={profile.email || ''} readOnly />
          </div>
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Username</label>
            <input
              type="text"
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
            <label className="block text-sm font-semibold text-gray-700 mb-2">
              No. Telepon/WhatsApp <span className="font-normal text-gray-500">(opsional)</span>
            </label>
            <input
              type="tel"
              inputMode="tel"
              maxLength={20}
              placeholder="08123456789"
              className={inputClass}
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
            />
          </div>
          <button
            type="submit"
            disabled={loading}
            className="w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-60 transition"
          >
            {loading ? 'Menyimpan...' : 'Simpan dan lanjutkan'}
          </button>
        </form>
        <p className="mt-4 text-center text-xs text-gray-500">
          Dengan melanjutkan, Anda menyetujui <Link to="/syarat" className="underline">Syarat &amp; Ketentuan</Link> dan{' '}
          <Link to="/privasi" className="underline">Kebijakan Privasi</Link>.
        </p>
      </div>
    </div>
  );
};

export default CompleteProfile;
