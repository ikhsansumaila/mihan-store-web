import React, { useEffect, useState } from 'react';
import { NavLink, Route, Routes } from 'react-router-dom';
import { adminFetch } from './api';
import { ErrorBox } from './ui';
import Products from './Products';
import Categories from './Categories';
import Activity, { LogTable } from './Activity';
import InvoiceCreate from '../components/InvoiceCreate';

// Halaman admin. Akses dijaga Cloudflare Access (di tepi) + backend (ADMIN_EMAILS, role/status DB).
// Tautan dari luar ke /admin HARUS anchor biasa (muat ulang penuh) agar dicegat Cloudflare Access.

const Stat = ({ label, value, sub }) => (
  <div className="bg-white rounded-xl shadow p-5">
    <div className="text-sm text-gray-500">{label}</div>
    <div className="text-3xl font-bold text-purple-700 mt-1">{value}</div>
    {sub && <div className="text-xs text-gray-500 mt-1">{sub}</div>}
  </div>
);

const Dashboard = () => {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    adminFetch('/summary').then(setData).catch(setError);
  }, []);
  if (error) return <ErrorBox error={error} />;
  if (!data) return <p className="text-gray-500">Memuat...</p>;
  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-800 mb-4">Ringkasan</h1>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-8">
        <Stat label="Produk" value={data.products?.total ?? 0} sub={`${data.products?.active ?? 0} aktif · ${data.products?.inactive ?? 0} nonaktif`} />
        <Stat label="Kategori" value={data.categories ?? 0} />
      </div>
      <h2 className="text-lg font-semibold text-gray-800 mb-3">10 aktivitas terakhir</h2>
      <LogTable items={data.recentActivity || []} compact />
    </div>
  );
};

const tabClass = ({ isActive }) =>
  `px-3 py-2 rounded-lg text-sm font-medium whitespace-nowrap ${
    isActive ? 'bg-purple-700 text-white' : 'text-gray-700 hover:bg-purple-100'
  }`;

const AdminApp = () => {
  const [me, setMe] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    adminFetch('/me')
      .then((r) => setMe(r.user))
      .catch(setError);
  }, []);

  if (error) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-10">
        <h1 className="text-2xl font-bold text-gray-800 mb-4">Admin</h1>
        <ErrorBox error={error} />
        {!error.sessionExpired && (
          <p className="mt-4 text-sm text-gray-600">
            Halaman admin hanya untuk pengelola toko. <a href="/" className="text-purple-700 underline">Kembali ke toko</a>
          </p>
        )}
      </div>
    );
  }
  if (!me) return <p className="max-w-7xl mx-auto px-4 py-10 text-gray-500">Memeriksa akses admin...</p>;

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
        <nav className="flex gap-2 overflow-x-auto">
          <NavLink end to="/admin" className={tabClass}>
            Ringkasan
          </NavLink>
          <NavLink to="/admin/products" className={tabClass}>
            Produk
          </NavLink>
          <NavLink to="/admin/categories" className={tabClass}>
            Kategori
          </NavLink>
          <NavLink to="/admin/invoice" className={tabClass}>
            Invoice
          </NavLink>
          <NavLink to="/admin/activity" className={tabClass}>
            Log aktivitas
          </NavLink>
        </nav>
        <div className="text-sm text-gray-600">
          Masuk sebagai <span className="font-semibold">{me.email}</span>
        </div>
      </div>
      <Routes>
        <Route index element={<Dashboard />} />
        <Route path="products" element={<Products />} />
        <Route path="categories" element={<Categories />} />
        <Route path="invoice" element={<InvoiceCreate />} />
        <Route path="activity" element={<Activity />} />
        <Route path="*" element={<p className="text-gray-600">Halaman admin tidak ditemukan.</p>} />
      </Routes>
    </div>
  );
};

export default AdminApp;
