import React, { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { adminFetch, qs, rupiah, fmtTime } from './api';
import { ErrorBox, Pagination, cardClass, inputClass, btnPrimary, btnSecondary } from './ui';
import { Icon } from './icons';
import { AliasEditModal, AliasText } from './AliasEditModal';
import { OrderStatusBadge } from './Orders';

// Menu admin "Pelanggan": daftar akun pelanggan (role customer) beserta statistik pesanan, detail
// pelanggan, dan ALIAS (nama panggilan internal, hanya terlihat admin; tidak pernah dikirim ke pelanggan).

export const SORTS = [
  { value: 'newest', label: 'Terbaru daftar' },
  { value: 'orders', label: 'Jumlah pesanan terbanyak' },
  { value: 'spent', label: 'Total belanja terbesar' },
];

const fmtDate = (iso) => {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleDateString('id-ID', { dateStyle: 'medium' });
  } catch {
    return iso;
  }
};

export const AccountStatus = ({ status }) =>
  status === 'active' ? (
    <span className="inline-block whitespace-nowrap rounded-full border border-green-200 bg-green-50 px-2 py-0.5 text-xs font-semibold text-green-700">Aktif</span>
  ) : (
    <span className="inline-block whitespace-nowrap rounded-full border border-red-200 bg-red-50 px-2 py-0.5 text-xs font-semibold text-red-700">Ditangguhkan</span>
  );

export const CustomersList = () => {
  const [draft, setDraft] = useState('');
  const [q, setQ] = useState('');
  const [sort, setSort] = useState('newest');
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0, perPage: 20 });
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await adminFetch(`/customers${qs({ q, sort, page, per_page: 20 })}`));
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [q, sort, page]);

  useEffect(() => {
    load();
  }, [load]);

  const apply = (e) => {
    e.preventDefault();
    setPage(1);
    setQ(draft.trim());
  };

  const updateAlias = (id, alias) =>
    setData((d) => ({ ...d, items: d.items.map((c) => (c.id === id ? { ...c, alias } : c)) }));

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold text-gray-900 sm:text-2xl">Pelanggan</h1>
      <form onSubmit={apply} className={`${cardClass} mb-4 grid grid-cols-1 items-end gap-3 p-4 sm:grid-cols-2 lg:grid-cols-4`}>
        <label className="block lg:col-span-2">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Cari (nama, username, email, telepon, alias)</span>
          <input className={inputClass} type="search" value={draft} onChange={(e) => setDraft(e.target.value)} maxLength={100} placeholder="mis. Siti / 0812..." />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Urutkan</span>
          <select
            className={inputClass}
            value={sort}
            aria-label="Urutkan pelanggan"
            onChange={(e) => {
              setSort(e.target.value);
              setPage(1);
            }}
          >
            {SORTS.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            className={btnSecondary}
            onClick={() => {
              setDraft('');
              setQ('');
              setPage(1);
            }}
          >
            Reset
          </button>
          <button type="submit" className={btnPrimary}>
            Cari
          </button>
        </div>
      </form>
      <ErrorBox error={error} />
      <div className={`${cardClass} relative overflow-x-auto`}>
        <table className="w-full min-w-[860px] text-sm">
          <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
            <tr>
              <th className="p-3">Pelanggan</th>
              <th className="p-3">Kontak</th>
              <th className="p-3 text-right">Pesanan</th>
              <th className="p-3 text-right">Total belanja</th>
              <th className="p-3">Daftar</th>
              <th className="p-3">Status</th>
              <th className="p-3">
                <span className="sr-only">Aksi</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {loading && data.items.length === 0 ? (
              <tr>
                <td colSpan="7" className="p-6 text-center text-gray-500">
                  Memuat...
                </td>
              </tr>
            ) : data.items.length === 0 ? (
              <tr>
                <td colSpan="7" className="p-6 text-center text-gray-500">
                  {q ? 'Tidak ada pelanggan yang cocok.' : 'Belum ada pelanggan.'}
                </td>
              </tr>
            ) : (
              data.items.map((c) => (
                <tr key={c.id} className="border-t border-gray-100 hover:bg-purple-50" data-customer-row={c.id}>
                  <td className="min-w-[14rem] p-3 [overflow-wrap:anywhere]">
                    <Link to={`/admin/customers/${c.id}`} className="block hover:underline">
                      <AliasText alias={c.alias} className="block" />
                      <span className={c.alias ? 'block text-xs text-gray-600' : 'block font-medium text-gray-900'}>{c.name}</span>
                    </Link>
                    <span className="text-xs text-gray-500">@{c.username}</span>
                  </td>
                  <td className="min-w-[12rem] p-3 text-xs text-gray-600 [overflow-wrap:anywhere]">
                    <div>{c.email}</div>
                    <div>{c.phone || '-'}</div>
                  </td>
                  <td className="p-3 text-right">{c.orderCount}</td>
                  <td className="whitespace-nowrap p-3 text-right font-semibold">{rupiah(c.totalSpent)}</td>
                  <td className="whitespace-nowrap p-3">{fmtDate(c.createdAt)}</td>
                  <td className="p-3">
                    <AccountStatus status={c.status} />
                  </td>
                  <td className="p-3 text-right">
                    <button
                      type="button"
                      className={`${btnSecondary} !px-2.5 !py-1.5`}
                      onClick={() => setEditing(c)}
                      aria-label={`Ubah alias ${c.name}`}
                      title="Ubah alias"
                    >
                      <Icon name="pencil" className="h-4 w-4" />
                      <span className="hidden sm:inline">Alias</span>
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <Pagination page={page} perPage={data.perPage} total={data.total} onPage={setPage} />
      <p className="mt-2 text-xs text-gray-500">Total belanja = jumlah total pesanan berstatus Dibayar atau Selesai.</p>
      {editing && (
        <AliasEditModal
          customer={editing}
          onClose={() => setEditing(null)}
          onSaved={(alias) => {
            updateAlias(editing.id, alias);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
};

const Stat = ({ label, value }) => (
  <div className={`${cardClass} p-4`}>
    <div className="text-[11px] font-medium uppercase tracking-wide text-gray-500 sm:text-xs">{label}</div>
    <div className="mt-1 text-lg font-bold text-gray-900 sm:text-xl">{value}</div>
  </div>
);

export const CustomerDetail = () => {
  const { id } = useParams();
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    setError(null);
    setData(null);
    adminFetch(`/customers/${id}`).then(setData).catch(setError);
  }, [id]);

  if (error && !data) {
    return (
      <div className="space-y-3">
        <Link to="/admin/customers" className="text-sm text-purple-700 hover:underline">
          ‹ Semua pelanggan
        </Link>
        <ErrorBox error={error} />
      </div>
    );
  }
  if (!data) return <p className="text-gray-500">Memuat...</p>;
  const c = data.customer || {};
  const orders = data.recentOrders || [];

  return (
    <div className="space-y-4">
      <div>
        <Link to="/admin/customers" className="text-sm text-purple-700 hover:underline">
          ‹ Semua pelanggan
        </Link>
        <h1 className="flex flex-wrap items-center gap-2 text-xl font-semibold text-gray-900 sm:text-2xl [overflow-wrap:anywhere]">
          {c.alias || c.name} <AccountStatus status={c.status} />
        </h1>
        {c.alias && <div className="text-sm text-gray-500">Nama akun: {c.name}</div>}
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <section className={`${cardClass} p-4 lg:col-span-1`}>
          <h2 className="mb-3 font-semibold text-gray-800">Data akun</h2>
          <dl className="space-y-2 text-sm [overflow-wrap:anywhere]">
            <div>
              <dt className="text-xs text-gray-500">Alias</dt>
              <dd className="flex flex-wrap items-center gap-2">
                <AliasText alias={c.alias} />
                <button type="button" className={`${btnSecondary} !px-2.5 !py-1`} onClick={() => setEditing(true)}>
                  <Icon name="pencil" className="h-4 w-4" />
                  {c.alias ? 'Ubah alias' : 'Beri alias'}
                </button>
              </dd>
            </div>
            <div>
              <dt className="text-xs text-gray-500">Nama akun</dt>
              <dd className="font-medium">{c.name}</dd>
            </div>
            <div>
              <dt className="text-xs text-gray-500">Username</dt>
              <dd>@{c.username}</dd>
            </div>
            <div>
              <dt className="text-xs text-gray-500">Email</dt>
              <dd>{c.email}</dd>
            </div>
            <div>
              <dt className="text-xs text-gray-500">Telepon</dt>
              <dd>{c.phone || '-'}</dd>
            </div>
            <div>
              <dt className="text-xs text-gray-500">Terdaftar</dt>
              <dd>{fmtTime(c.createdAt)}</dd>
            </div>
          </dl>
        </section>

        <div className="space-y-4 lg:col-span-2">
          <section aria-label="Statistik pelanggan" className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Stat label="Jumlah pesanan" value={c.orderCount ?? 0} />
            <Stat label="Total belanja" value={rupiah(c.totalSpent || 0)} />
            <Stat label="Pesanan terakhir" value={c.lastOrderAt ? fmtDate(c.lastOrderAt) : '-'} />
          </section>
          <section className={`${cardClass} p-4`}>
            <h2 className="mb-3 font-semibold text-gray-800">10 pesanan terakhir</h2>
            {orders.length === 0 ? (
              <p className="text-sm text-gray-500">Belum ada pesanan.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[480px] text-sm">
                  <thead className="text-left text-xs uppercase tracking-wide text-gray-500">
                    <tr>
                      <th className="py-2">No. pesanan</th>
                      <th className="py-2">Status</th>
                      <th className="py-2 text-right">Total</th>
                      <th className="py-2">Tanggal</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orders.map((o) => (
                      <tr key={o.id} className="border-t border-gray-100">
                        <td className="whitespace-nowrap py-2 font-semibold">
                          <Link to={`/admin/orders/${o.orderNo}`} className="text-purple-700 hover:underline">
                            {o.orderNo}
                          </Link>
                        </td>
                        <td className="py-2">
                          <OrderStatusBadge status={o.status} />
                        </td>
                        <td className="whitespace-nowrap py-2 text-right">{rupiah(o.total)}</td>
                        <td className="whitespace-nowrap py-2">{fmtTime(o.createdAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <p className="mt-2 text-xs text-gray-500">Total belanja = jumlah total pesanan berstatus Dibayar atau Selesai.</p>
          </section>
        </div>
      </div>

      {editing && (
        <AliasEditModal
          customer={c}
          onClose={() => setEditing(false)}
          onSaved={(alias) => {
            setData((d) => ({ ...d, customer: { ...d.customer, alias } }));
            setEditing(false);
          }}
        />
      )}
    </div>
  );
};
