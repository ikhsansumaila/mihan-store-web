import React, { useEffect, useState } from 'react';
import { Route, Routes, useLocation } from 'react-router-dom';
import { adminFetch } from './api';
import { ErrorBox, cardClass } from './ui';
import AdminLayout from './AdminLayout';
import Dashboard from './Dashboard';
import Products from './Products';
import Categories from './Categories';
import Activity from './Activity';
import InvoiceCreate from '../components/InvoiceCreate';
import { OrdersList, AdminOrderRoute } from './Orders';
import Settings from './Settings';
import Pricelist from './Pricelist';
import Regions from './Regions';

// Halaman admin. Akses dijaga Cloudflare Access (di tepi) + backend (ADMIN_EMAILS, role/status DB).
// Tautan dari luar ke /admin HARUS anchor biasa (muat ulang penuh) agar dicegat Cloudflare Access.

// Daftar pesanan dipasang ulang bila query (?status=) berubah agar filter awal dari URL berlaku.
const OrdersListRoute = () => {
  const { search } = useLocation();
  return <OrdersList key={search} />;
};

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
      <div className="min-h-screen bg-gray-100 px-4 py-10">
        <div className={`${cardClass} mx-auto max-w-xl p-6`}>
          <h1 className="mb-4 text-xl font-semibold text-gray-900">Mihan Store Admin</h1>
          <ErrorBox error={error} />
          {!error.sessionExpired && (
            <p className="mt-4 text-sm text-gray-600">
              Halaman admin hanya untuk pengelola toko.{' '}
              <a href="/" className="text-purple-700 underline">
                Kembali ke toko
              </a>
            </p>
          )}
        </div>
      </div>
    );
  }
  if (!me) return <p className="min-h-screen bg-gray-100 px-4 py-10 text-center text-gray-500">Memeriksa akses admin...</p>;

  return (
    <AdminLayout me={me}>
      <Routes>
        <Route index element={<Dashboard />} />
        <Route path="orders" element={<OrdersListRoute />} />
        <Route path="orders/:id" element={<AdminOrderRoute />} />
        <Route path="settings" element={<Settings />} />
        <Route path="regions" element={<Regions />} />
        <Route path="products" element={<Products />} />
        <Route path="categories" element={<Categories />} />
        <Route path="invoice" element={<InvoiceCreate />} />
        <Route path="pricelist" element={<Pricelist />} />
        <Route path="activity" element={<Activity />} />
        <Route path="*" element={<p className="text-gray-600">Halaman admin tidak ditemukan.</p>} />
      </Routes>
    </AdminLayout>
  );
};

export default AdminApp;
