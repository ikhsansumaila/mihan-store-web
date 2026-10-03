import React, { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { ErrorBox, cardClass } from './ui';
import { LogTable } from './Activity';
import { Icon } from './icons';
import { useAdminSummary } from './AdminLayout';

// Dashboard bergaya cPanel: kartu statistik ber-ikon, pintasan cepat, 10 aktivitas terakhir.
// Data dari /api/admin/summary (sama seperti Ringkasan sebelumnya).

const TONES = {
  purple: 'bg-purple-100 text-purple-700',
  blue: 'bg-sky-100 text-sky-700',
  amber: 'bg-amber-100 text-amber-700',
  green: 'bg-emerald-100 text-emerald-700',
  slate: 'bg-slate-100 text-slate-700',
};

const StatCard = ({ icon, tone, label, value, sub, to }) => (
  <Link
    to={to}
    className={`${cardClass} flex flex-col items-start gap-3 p-4 transition sm:flex-row sm:gap-4 hover:border-purple-300 hover:shadow focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500`}
  >
    <span className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-lg ${TONES[tone]}`}>
      <Icon name={icon} className="h-6 w-6" />
    </span>
    <span className="min-w-0">
      <span className="block text-[11px] font-medium uppercase tracking-wide text-gray-500 sm:text-xs">{label}</span>
      <span className="mt-0.5 block text-2xl font-bold text-gray-900">{value}</span>
      {sub && <span className="mt-0.5 block text-xs text-gray-500">{sub}</span>}
    </span>
  </Link>
);

const SHORTCUTS = [
  { to: '/admin/products?tambah=1', label: 'Tambah Produk', desc: 'Buat produk baru di katalog', icon: 'plus' },
  { to: '/admin/orders?status=pending_payment', label: 'Pesanan baru', desc: 'Pesanan menunggu pembayaran', icon: 'cart' },
  { to: '/admin/customers', label: 'Pelanggan', desc: 'Daftar pelanggan & alias', icon: 'users' },
  { to: '/admin/pricelist', label: 'Buat Pricelist', desc: 'Gambar daftar harga untuk WhatsApp', icon: 'pricelist' },
  { to: '/admin/settings', label: 'Pengaturan Toko', desc: 'WhatsApp & rekening bank', icon: 'settings' },
  { to: '/admin/activity', label: 'Log Aktivitas', desc: 'Riwayat perubahan & akses', icon: 'clock' },
];

const Dashboard = () => {
  const { summary: data, error, refresh } = useAdminSummary();

  // Muat ulang setiap kali Dashboard dibuka (permintaan ganda saat mount digabung oleh refresh()).
  useEffect(() => {
    refresh();
  }, [refresh]);

  if (error && !data) return <ErrorBox error={error} />;
  if (!data) return <p className="text-gray-500">Memuat...</p>;
  const o = data.orders || {};
  const p = data.products || {};
  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold text-gray-900 sm:text-2xl">Dashboard</h1>
      {error && <ErrorBox error={error} />}

      <section aria-label="Statistik">
        <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-3 2xl:grid-cols-6">
          <StatCard icon="box" tone="purple" label="Produk aktif" value={p.active ?? 0} sub={`${p.total ?? 0} total · ${p.inactive ?? 0} nonaktif`} to="/admin/products" />
          <StatCard icon="tag" tone="blue" label="Kategori" value={data.categories ?? 0} to="/admin/categories" />
          <StatCard icon="alert" tone="amber" label="Menunggu pembayaran" value={o.pendingPayment ?? 0} sub="perlu dicek transfernya" to="/admin/orders?status=pending_payment" />
          <StatCard icon="check" tone="green" label="Dibayar" value={o.paid ?? 0} sub="siap dikirim / diselesaikan" to="/admin/orders?status=paid" />
          <StatCard icon="chart" tone="slate" label="Pesanan 7 hari terakhir" value={o.last7Days ?? 0} to="/admin/orders" />
          <StatCard icon="users" tone="blue" label="Pelanggan" value={data.customers ?? 0} sub="akun pelanggan terdaftar" to="/admin/customers" />
        </div>
      </section>

      <section aria-labelledby="pintasan-cepat">
        <h2 id="pintasan-cepat" className="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-500">
          Pintasan cepat
        </h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
          {SHORTCUTS.map((s) => (
            <Link
              key={s.label}
              to={s.to}
              className={`${cardClass} group flex items-center gap-3 p-3 transition hover:border-purple-300 hover:bg-purple-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500`}
            >
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-purple-700 text-white">
                <Icon name={s.icon} className="h-5 w-5" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-semibold text-gray-800">{s.label}</span>
                <span className="block truncate text-xs text-gray-500">{s.desc}</span>
              </span>
              <Icon name="chevron" className="h-4 w-4 text-gray-400 group-hover:text-purple-700" />
            </Link>
          ))}
        </div>
      </section>

      <section aria-labelledby="aktivitas-terakhir">
        <div className="mb-3 flex items-center justify-between gap-2">
          <h2 id="aktivitas-terakhir" className="text-sm font-semibold uppercase tracking-wide text-gray-500">
            10 aktivitas terakhir
          </h2>
          <Link to="/admin/activity" className="text-sm text-purple-700 hover:underline">
            Lihat semua ›
          </Link>
        </div>
        <LogTable items={data.recentActivity || []} compact />
      </section>
    </div>
  );
};

export default Dashboard;
