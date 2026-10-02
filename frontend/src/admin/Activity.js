import React, { useCallback, useEffect, useState } from 'react';
import { adminFetch, qs, fmtTime } from './api';
import { ErrorBox, Modal, Pagination, inputClass, btnSecondary, btnDanger } from './ui';

export const ACTION_LABELS = {
  'auth.register': 'Pendaftaran',
  'auth.login': 'Login',
  'auth.login_failed': 'Login gagal',
  'auth.locked': 'Akun dikunci',
  'auth.logout': 'Logout',
  'auth.google_login': 'Login Google',
  'auth.google_link': 'Sambung Google',
  'user.role_change': 'Ubah role',
  'admin.access_denied': 'Akses admin ditolak',
  'product.create': 'Produk ditambah',
  'product.update': 'Produk diubah',
  'product.delete': 'Produk dihapus',
  'product.activate': 'Produk diaktifkan',
  'product.deactivate': 'Produk dinonaktifkan',
  'category.create': 'Kategori ditambah',
  'category.update': 'Kategori diubah',
  'category.delete': 'Kategori dihapus',
  'activity_log.purge': 'Hapus log lama',
};

export const actorOf = (l) => l.userEmail || l.actorLabel || l.username || '-';

export const LogTable = ({ items, compact }) => (
  <div className="overflow-x-auto bg-white rounded-lg border border-gray-200 shadow-sm">
    <table className="min-w-full text-sm">
      <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
        <tr>
          <th className="px-3 py-2">Waktu</th>
          <th className="px-3 py-2">Aksi</th>
          <th className="px-3 py-2">Pelaku</th>
          <th className="px-3 py-2">Ringkasan</th>
          {!compact && <th className="px-3 py-2">IP / Perangkat</th>}
        </tr>
      </thead>
      <tbody>
        {items.length === 0 ? (
          <tr>
            <td colSpan={compact ? 4 : 5} className="px-3 py-6 text-center text-gray-500">
              Belum ada aktivitas.
            </td>
          </tr>
        ) : (
          items.map((l) => (
            <tr key={l.id} className="border-t border-gray-100 align-top hover:bg-gray-50">
              <td className="px-3 py-2 whitespace-nowrap text-gray-600">{fmtTime(l.createdAt)}</td>
              <td className="px-3 py-2 whitespace-nowrap">
                <span className="font-medium text-gray-800">{ACTION_LABELS[l.action] || l.action}</span>
                <div className="text-xs text-gray-400 font-mono">{l.action}</div>
              </td>
              <td className="px-3 py-2 break-all">{actorOf(l)}</td>
              <td className="px-3 py-2 min-w-[14rem]">
                <div>{l.summary}</div>
                {!compact && l.details && (
                  <details className="mt-1">
                    <summary className="cursor-pointer text-xs text-purple-700">Detail</summary>
                    <pre className="mt-1 max-w-md overflow-x-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 text-xs text-gray-700">
                      {JSON.stringify(l.details, null, 2)}
                    </pre>
                  </details>
                )}
              </td>
              {!compact && (
                <td className="px-3 py-2 text-xs text-gray-500 max-w-[14rem]">
                  <div>{l.ip || '-'}</div>
                  <div className="truncate" title={l.userAgent || ''}>
                    {l.userAgent || ''}
                  </div>
                </td>
              )}
            </tr>
          ))
        )}
      </tbody>
    </table>
  </div>
);

const Activity = () => {
  const [filters, setFilters] = useState({ action: '', actor: '', entity_type: '', from: '', to: '' });
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0, perPage: 25 });
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [confirm, setConfirm] = useState(false);
  const [purging, setPurging] = useState(false);
  const [purgeResult, setPurgeResult] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await adminFetch(`/activity-logs${qs({ ...filters, page, per_page: 25 })}`);
      setData(r);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [filters, page]);

  useEffect(() => {
    load();
  }, [load]);

  const setFilter = (k) => (e) => {
    setPage(1);
    setFilters({ ...filters, [k]: e.target.value });
  };

  const purge = async () => {
    setPurging(true);
    try {
      const r = await adminFetch('/activity-logs/purge', { method: 'POST', body: {} });
      setPurgeResult(r.deleted ?? 0);
      setConfirm(false);
      await load();
    } catch (err) {
      setError(err);
      setConfirm(false);
    } finally {
      setPurging(false);
    }
  };

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <h1 className="text-xl sm:text-2xl font-semibold text-gray-900">Log aktivitas</h1>
        <button className={btnDanger} onClick={() => setConfirm(true)}>
          Hapus log lebih dari 6 bulan
        </button>
      </div>

      {purgeResult !== null && (
        <div className="mb-4 rounded-lg border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800">
          {purgeResult} log lebih dari 6 bulan dihapus.
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3 mb-4">
        <select className={inputClass} value={filters.action} onChange={setFilter('action')}>
          <option value="">Semua aksi</option>
          {Object.entries(ACTION_LABELS).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
        <input className={inputClass} placeholder="Email / username pelaku" value={filters.actor} onChange={setFilter('actor')} />
        <select className={inputClass} value={filters.entity_type} onChange={setFilter('entity_type')}>
          <option value="">Semua entitas</option>
          <option value="product">Produk</option>
          <option value="category">Kategori</option>
          <option value="user">Pengguna</option>
          <option value="activity_log">Log</option>
        </select>
        <label className="text-xs text-gray-600">
          Dari
          <input type="date" className={inputClass} value={filters.from} onChange={setFilter('from')} />
        </label>
        <label className="text-xs text-gray-600">
          Sampai
          <input type="date" className={inputClass} value={filters.to} onChange={setFilter('to')} />
        </label>
      </div>

      <ErrorBox error={error} />
      <div className="mt-3">
        {loading ? <p className="text-gray-500">Memuat...</p> : <LogTable items={data.items || []} />}
      </div>
      <Pagination page={page} perPage={data.perPage} total={data.total} onPage={setPage} />

      {confirm && (
        <Modal title="Hapus log lama?" onClose={() => setConfirm(false)}>
          <p className="text-gray-700">
            Semua log aktivitas yang <strong>lebih tua dari 6 bulan (180 hari)</strong> akan dihapus permanen. Log yang
            lebih baru tidak terpengaruh. Tindakan ini juga dicatat.
          </p>
          <div className="flex justify-end gap-2 mt-6">
            <button className={btnSecondary} onClick={() => setConfirm(false)}>
              Batal
            </button>
            <button className={btnDanger} onClick={purge} disabled={purging}>
              {purging ? 'Menghapus...' : 'Ya, hapus'}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

export default Activity;
