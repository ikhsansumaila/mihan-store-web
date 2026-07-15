import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import axios from 'axios';

const API_BASE_URL = '/api';

const Register = ({ onRegisterSuccess }) => {
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const response = await axios.post(`${API_BASE_URL}/auth/register`, {
        email,
        password,
        name,
      });

      if (response.data.success) {
        localStorage.setItem('user', JSON.stringify(response.data.user));
        localStorage.setItem('token', response.data.token);
        onRegisterSuccess(response.data.user);
        navigate('/');
      } else {
        setError(response.data.message || 'Registrasi gagal');
      }
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan saat registrasi');
    } finally {
      setLoading(false);
    }
  };

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
              className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Email</label>
            <input
              type="email"
              className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>

          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-2">Password</label>
            <input
              type="password"
              className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>

          <button
            type="submit"
            className="w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-60 disabled:cursor-not-allowed transition"
            disabled={loading}
          >
            {loading ? 'Sedang mendaftar...' : 'Daftar'}
          </button>
        </form>

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
