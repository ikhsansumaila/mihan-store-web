import React, { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { adminFetch, qs, rupiah, perUnit, fmtTime } from './api';
import { ErrorBox, Modal, cardClass, Pagination, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';
import { STATUS, buildAdminSummaryText, formatFullAddress, statusLabel, waLink } from '../shop/format';
import { generateInvoicePdf, orderToInvoice } from '../invoicePdf';
import MoneyInput from '../components/MoneyInput';

// Batas server (backend/order_logic.go): ongkir maks. Rp 10.000.000, subtotal maks. Rp 2.000.000.000.
const SHIPPING_MAX = 10000000;
const AMOUNT_DIGITS = 10;

export const OrderStatusBadge = ({ status }) => (
  <span className={`inline-block text-xs font-semibold border rounded-full px-2.5 py-0.5 whitespace-nowrap ${STATUS[status]?.cls || 'bg-gray-100'}`}>
    {statusLabel(status)}
  </span>
);

const emptyFilters = { status: '', from: '', to: '', q: '' };

export const OrdersList = () => {
  // Filter status awal boleh dari URL (?status=..., dipakai pintasan Dashboard).
  const [searchParams] = useSearchParams();
  const [initial] = useState(() => {
    const st = searchParams.get('status');
    return st && Object.prototype.hasOwnProperty.call(STATUS, st) ? { ...emptyFilters, status: st } : emptyFilters;
  });
  const [filters, setFilters] = useState(initial);
  const [draft, setDraft] = useState(initial);
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0, perPage: 20 });
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await adminFetch(`/orders${qs({ ...filters, page, per_page: 20 })}`));
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [filters, page]);

  useEffect(() => {
    load();
  }, [load]);

  const apply = (e) => {
    e.preventDefault();
    setPage(1);
    setFilters({ ...draft, q: draft.q.trim() });
  };
  const set = (k) => (e) => setDraft({ ...draft, [k]: e.target.value });

  return (
    <div>
      <h1 className="text-xl sm:text-2xl font-semibold text-gray-900 mb-4">Pesanan</h1>
      <form onSubmit={apply} className="bg-white rounded-lg border border-gray-200 shadow-sm p-4 mb-4 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3 items-end">
        <label className="block lg:col-span-2">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Cari (no. pesanan, nama, telepon)</span>
          <input className={inputClass} value={draft.q} onChange={set('q')} maxLength={100} placeholder="MS-261002-0001" />
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Status</span>
          <select className={inputClass} value={draft.status} onChange={set('status')} aria-label="Filter status">
            <option value="">Semua status</option>
            {Object.keys(STATUS).map((s) => (
              <option key={s} value={s}>
                {statusLabel(s)}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Dari tanggal</span>
          <input type="date" className={inputClass} value={draft.from} onChange={set('from')} />
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Sampai tanggal</span>
          <input type="date" className={inputClass} value={draft.to} onChange={set('to')} />
        </label>
        <div className="flex gap-2 sm:col-span-2 lg:col-span-5 justify-end">
          <button
            type="button"
            className={btnSecondary}
            onClick={() => {
              setDraft(emptyFilters);
              setFilters(emptyFilters);
              setPage(1);
            }}
          >
            Reset
          </button>
          <button type="submit" className={btnPrimary}>
            Terapkan
          </button>
        </div>
      </form>
      <ErrorBox error={error} />
      <div className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-x-auto">
        <table className="w-full min-w-[720px] text-sm">
          <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
            <tr>
              <th className="p-3">No. pesanan</th>
              <th className="p-3">Tanggal</th>
              <th className="p-3">Pemesan / penerima</th>
              <th className="p-3 text-right">Item</th>
              <th className="p-3 text-right">Total</th>
              <th className="p-3">Status</th>
            </tr>
          </thead>
          <tbody>
            {loading && data.items.length === 0 ? (
              <tr>
                <td colSpan="6" className="p-6 text-center text-gray-500">
                  Memuat...
                </td>
              </tr>
            ) : data.items.length === 0 ? (
              <tr>
                <td colSpan="6" className="p-6 text-center text-gray-500">
                  Belum ada pesanan.
                </td>
              </tr>
            ) : (
              data.items.map((o) => (
                <tr key={o.id} className="border-t border-gray-100 hover:bg-purple-50">
                  <td className="p-3 font-semibold whitespace-nowrap">
                    <Link to={`/admin/orders/${o.id}`} className="text-purple-700 hover:underline">
                      {o.orderNo}
                    </Link>
                  </td>
                  <td className="p-3 whitespace-nowrap">{fmtTime(o.createdAt)}</td>
                  <td className="p-3 min-w-[16rem] [overflow-wrap:anywhere]">
                    <div>{o.customerName}</div>
                    <div className="text-xs text-gray-500">
                      → {o.recipientName}, {o.city}
                    </div>
                  </td>
                  <td className="p-3 text-right">{o.itemCount}</td>
                  <td className="p-3 text-right font-semibold whitespace-nowrap">{rupiah(o.total)}</td>
                  <td className="p-3">
                    <OrderStatusBadge status={o.status} />
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <Pagination page={page} perPage={data.perPage} total={data.total} onPage={setPage} />
    </div>
  );
};

const digits = (v) => String(v ?? '').replace(/[^0-9]/g, '');

const PricingForm = ({ order, onSaved }) => {
  const [discount, setDiscount] = useState(String(order.discount || 0));
  const [note, setNote] = useState(order.discountNote || '');
  const [shipping, setShipping] = useState(String(order.shippingFee || 0));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const d = Number(digits(discount) || 0);
  const s = Number(digits(shipping) || 0);
  const preview = order.subtotal - d + s;

  const save = async (e) => {
    e.preventDefault();
    setError(null);
    if (d > order.subtotal) return setError(new Error('Diskon tidak boleh melebihi subtotal'));
    if (s > SHIPPING_MAX) return setError(new Error('Ongkir maksimal Rp 10.000.000'));
    setSaving(true);
    try {
      onSaved(await adminFetch(`/orders/${order.id}/pricing`, { method: 'PATCH', body: { discount: d, discountNote: note, shippingFee: s } }));
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={save} className="space-y-3">
      <ErrorBox error={error} />
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Diskon (Rp)</span>
          <MoneyInput className={inputClass} value={discount} onValueChange={(v) => setDiscount(v)} maxDigits={AMOUNT_DIGITS} min={0} max={order.subtotal} />
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Ongkir (Rp)</span>
          <MoneyInput className={inputClass} value={shipping} onValueChange={(v) => setShipping(v)} maxDigits={AMOUNT_DIGITS} min={0} max={SHIPPING_MAX} />
        </label>
      </div>
      <label className="block">
        <span className="block text-xs font-semibold text-gray-600 mb-1">Keterangan diskon (opsional)</span>
        <input className={inputClass} maxLength={255} value={note} onChange={(e) => setNote(e.target.value)} />
      </label>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-sm text-gray-600">
          Total baru: <strong className={preview < 0 ? 'text-red-600' : 'text-purple-700'}>{rupiah(Math.max(preview, 0))}</strong>
        </span>
        <button type="submit" className={btnPrimary} disabled={saving}>
          {saving ? 'Menyimpan...' : 'Simpan diskon & ongkir'}
        </button>
      </div>
    </form>
  );
};

const StatusAction = ({ order, to, onClose, onSaved }) => {
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const needReason = to === 'cancelled' && order.status === 'paid';
  const titles = { paid: 'Tandai dibayar', completed: 'Tandai selesai', cancelled: 'Batalkan pesanan' };
  const label = to === 'paid' ? 'Catatan pembayaran (opsional)' : to === 'cancelled' ? `Alasan pembatalan${needReason ? ' (wajib)' : ' (opsional)'}` : 'Catatan (opsional)';

  const submit = async (e) => {
    e.preventDefault();
    setError(null);
    if (needReason && !text.trim()) return setError(new Error('Alasan pembatalan wajib diisi untuk pesanan yang sudah dibayar'));
    const body = { from: order.status, to };
    if (to === 'paid') body.paymentNote = text.trim();
    else if (to === 'cancelled') body.reason = text.trim();
    else body.note = text.trim();
    setSaving(true);
    try {
      onSaved(await adminFetch(`/orders/${order.id}/status`, { method: 'PATCH', body }));
    } catch (err) {
      setError(err);
      setSaving(false);
    }
  };

  return (
    <Modal title={`${titles[to]} – ${order.orderNo}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <ErrorBox error={error} />
        <p className="text-sm text-gray-700">
          Status: <strong>{statusLabel(order.status)}</strong> → <strong>{statusLabel(to)}</strong>
          {to === 'paid' && ' (diskon & ongkir akan terkunci)'}
        </p>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">{label}</span>
          <input className={inputClass} maxLength={255} value={text} onChange={(e) => setText(e.target.value)} />
        </label>
        <div className="flex justify-end gap-2">
          <button type="button" className={btnSecondary} onClick={onClose}>
            Batal
          </button>
          <button type="submit" className={to === 'cancelled' ? btnDanger : btnPrimary} disabled={saving}>
            {saving ? 'Menyimpan...' : titles[to]}
          </button>
        </div>
      </form>
    </Modal>
  );
};

const AdminNote = ({ order, onSaved }) => {
  const [note, setNote] = useState(order.adminNote || '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [ok, setOk] = useState(false);
  const save = async () => {
    setSaving(true);
    setError(null);
    setOk(false);
    try {
      onSaved(await adminFetch(`/orders/${order.id}/note`, { method: 'PATCH', body: { adminNote: note } }));
      setOk(true);
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="space-y-2">
      <ErrorBox error={error} />
      <textarea className={inputClass} rows={3} maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Hanya terlihat oleh admin" />
      <div className="flex items-center justify-end gap-3">
        {ok && <span className="text-sm text-green-700">Tersimpan</span>}
        <button type="button" className={btnSecondary} onClick={save} disabled={saving}>
          {saving ? 'Menyimpan...' : 'Simpan catatan'}
        </button>
      </div>
    </div>
  );
};

const Card = ({ title, children }) => (
  <section className="bg-white rounded-lg border border-gray-200 shadow-sm p-4">
    <h2 className="font-semibold text-gray-800 mb-3">{title}</h2>
    {children}
  </section>
);

export const AdminOrderDetail = () => {
  const { id } = useParams();
  const [order, setOrder] = useState(null);
  const [settings, setSettings] = useState({});
  const [error, setError] = useState(null);
  const [action, setAction] = useState(null);
  const [printing, setPrinting] = useState(false);

  useEffect(() => {
    setError(null);
    adminFetch(`/orders/${id}`).then(setOrder).catch(setError);
    adminFetch('/settings')
      .then((r) => setSettings(r.settings || {}))
      .catch(() => setSettings({}));
  }, [id]);

  if (error && !order) return <ErrorBox error={error} />;
  if (!order) return <p className="text-gray-500">Memuat...</p>;

  const r = order.recipient || {};
  const wa = waLink(r.phone, buildAdminSummaryText(order, settings));
  const printInvoice = async () => {
    setPrinting(true);
    try {
      await generateInvoicePdf(orderToInvoice(order, settings));
    } finally {
      setPrinting(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link to="/admin/orders" className="text-sm text-purple-700 hover:underline">
            ‹ Semua pesanan
          </Link>
          <h1 className="text-xl sm:text-2xl font-semibold text-gray-900 flex flex-wrap items-center gap-3">
            {order.orderNo} <OrderStatusBadge status={order.status} />
          </h1>
          <div className="text-sm text-gray-500">Dibuat {fmtTime(order.createdAt)}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          {order.status !== 'cancelled' && (
            <button type="button" className={btnSecondary} onClick={printInvoice} disabled={printing}>
              {printing ? 'Menyiapkan...' : 'Cetak invoice'}
            </button>
          )}
          {wa ? (
            <a href={wa} target="_blank" rel="noopener noreferrer" className="bg-green-500 text-white px-4 py-2 rounded-lg text-sm font-semibold hover:bg-green-600">
              Kirim ringkasan ke WhatsApp pelanggan
            </a>
          ) : (
            <span className="text-xs text-gray-500 self-center">Nomor penerima tidak valid untuk WhatsApp</span>
          )}
        </div>
      </div>
      <ErrorBox error={error} />

      {order.allowedNext?.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {order.allowedNext.includes('paid') && (
            <button className={btnPrimary} onClick={() => setAction('paid')}>
              Tandai Dibayar
            </button>
          )}
          {order.allowedNext.includes('completed') && (
            <button className={btnPrimary} onClick={() => setAction('completed')}>
              Tandai Selesai
            </button>
          )}
          {order.allowedNext.includes('cancelled') && (
            <button className={btnDanger} onClick={() => setAction('cancelled')}>
              Batalkan
            </button>
          )}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2 space-y-4">
          <Card title="Item">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[420px] text-sm">
                <thead className="text-left text-gray-600">
                  <tr>
                    <th className="py-2">Produk</th>
                    <th className="py-2 text-right">Qty</th>
                    <th className="py-2 text-right">Harga</th>
                    <th className="py-2 text-right">Subtotal</th>
                  </tr>
                </thead>
                <tbody>
                  {order.items.map((it, i) => (
                    <tr key={i} className="border-t">
                      <td className="py-2">
                        {it.name}
                        {it.tierMinQty ? (
                          <span className="ml-1 inline-block rounded bg-green-100 px-1.5 text-xs font-semibold text-green-800">
                            harga grosir (min. {it.tierMinQty})
                          </span>
                        ) : null}
                        {it.tierMinQty && it.baseUnitPrice ? (
                          <div className="text-xs text-gray-500">Harga dasar saat pesan: {perUnit(it.baseUnitPrice, it.unit)}</div>
                        ) : null}
                      </td>
                      <td className="py-2 text-right whitespace-nowrap">
                        {it.qty}
                        {it.unit ? ` ${it.unit}` : ''}
                      </td>
                      <td className="py-2 text-right whitespace-nowrap">{perUnit(it.unitPrice, it.unit)}</td>
                      <td className="py-2 text-right whitespace-nowrap">{rupiah(it.lineTotal)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="border-t mt-2 pt-2 text-sm space-y-1">
              <div className="flex justify-between">
                <span>Subtotal</span>
                <span>{rupiah(order.subtotal)}</span>
              </div>
              <div className="flex justify-between">
                <span>Diskon{order.discountNote ? ` (${order.discountNote})` : ''}</span>
                <span>-{rupiah(order.discount)}</span>
              </div>
              <div className="flex justify-between">
                <span>Ongkir</span>
                <span>{rupiah(order.shippingFee)}</span>
              </div>
              <div className="flex justify-between font-bold text-purple-700 text-base">
                <span>Total</span>
                <span>{rupiah(order.total)}</span>
              </div>
            </div>
          </Card>

          <Card title="Diskon & ongkir">
            {order.pricingLocked ? (
              <p className="text-sm text-gray-600">Terkunci: diskon dan ongkir hanya bisa diubah saat pesanan menunggu pembayaran.</p>
            ) : (
              <PricingForm key={order.updatedAt} order={order} onSaved={setOrder} />
            )}
          </Card>

          <Card title="Riwayat status">
            <ol className="border-l-2 border-purple-200 ml-2 space-y-3 text-sm">
              {order.history.map((h, i) => (
                <li key={i} className="pl-4 relative">
                  <span className="absolute -left-[7px] top-1 w-3 h-3 rounded-full bg-purple-500" />
                  <div className="font-medium">
                    {h.from ? `${statusLabel(h.from)} → ` : ''}
                    {h.toLabel}
                  </div>
                  <div className="text-gray-500">
                    {fmtTime(h.createdAt)} · {h.actor}
                  </div>
                  {h.note && <div className="text-gray-700">{h.note}</div>}
                </li>
              ))}
            </ol>
          </Card>
        </div>

        <div className="space-y-4">
          <Card title="Penerima">
            <div className="text-sm space-y-0.5">
              <div className="font-medium">{r.name}</div>
              <div>{r.phone}</div>
              <div className="break-words" data-testid="admin-full-address">
                {formatFullAddress(r)}
              </div>
              {order.customerNote && <div className="mt-2 text-gray-600">Catatan pelanggan: {order.customerNote}</div>}
            </div>
          </Card>
          <Card title="Akun pemesan">
            <div className="text-sm">
              <div className="font-medium">{order.customer?.name}</div>
              <div className="text-gray-600">@{order.customer?.username}</div>
              <div className="text-gray-600 break-all">{order.customer?.email}</div>
            </div>
          </Card>
          <Card title="Pembayaran">
            <div className="text-sm space-y-1 text-gray-700">
              <div>Metode: transfer bank manual</div>
              {order.paidAt && (
                <div>
                  Dibayar {fmtTime(order.paidAt)} {order.paidBy ? `(ditandai ${order.paidBy})` : ''}
                </div>
              )}
              {order.paymentNote && <div>Catatan: {order.paymentNote}</div>}
              {order.completedAt && <div>Selesai {fmtTime(order.completedAt)}</div>}
              {order.cancelledAt && (
                <div className="text-red-700">
                  Dibatalkan {fmtTime(order.cancelledAt)} {order.cancelledBy ? `oleh ${order.cancelledBy}` : ''}
                  {order.cancelReason ? ` – ${order.cancelReason}` : ''}
                </div>
              )}
            </div>
          </Card>
          <Card title="Catatan admin">
            <AdminNote key={order.id} order={order} onSaved={setOrder} />
          </Card>
        </div>
      </div>

      {action && (
        <StatusAction
          order={order}
          to={action}
          onClose={() => setAction(null)}
          onSaved={(o) => {
            setOrder(o);
            setAction(null);
          }}
        />
      )}
    </div>
  );
};

// /admin/orders/:id menerima id numerik ATAU nomor pesanan (mis. dari tautan di WhatsApp konfirmasi pelanggan).
// Nomor pesanan dicari lewat GET /api/admin/orders?q=<nomor> lalu dicocokkan PERSIS (pencarian backend memakai
// LIKE, jadi MS-261002-0001 juga bisa mengenai MS-261002-00010); URL lalu diganti ke /admin/orders/<id>.
export const ORDER_NO_RE = /^MS-\d{6}-\d+$/i;

const OrderNoResolver = ({ orderNo }) => {
  const navigate = useNavigate();
  const [state, setState] = useState({ status: 'loading', error: null });

  useEffect(() => {
    let alive = true;
    setState({ status: 'loading', error: null });
    adminFetch(`/orders${qs({ q: orderNo, per_page: 100 })}`)
      .then((r) => {
        if (!alive) return;
        const want = orderNo.toUpperCase();
        const hit = ((r && r.items) || []).find((o) => String(o.orderNo || '').toUpperCase() === want);
        if (hit && hit.id !== undefined && hit.id !== null) navigate(`/admin/orders/${hit.id}`, { replace: true });
        else setState({ status: 'notfound', error: null });
      })
      .catch((err) => alive && setState({ status: 'error', error: err }));
    return () => {
      alive = false;
    };
  }, [orderNo, navigate]);

  if (state.status === 'error') return <ErrorBox error={state.error} />;
  if (state.status === 'notfound') {
    return (
      <div className={`${cardClass} p-5`} data-testid="order-not-found">
        <p className="font-semibold text-gray-900">Pesanan {orderNo.toUpperCase()} tidak ditemukan</p>
        <p className="mt-1 text-sm text-gray-600">Nomor pesanan mungkin salah ketik atau pesanan sudah dihapus.</p>
        <Link to="/admin/orders" className="mt-3 inline-block text-sm font-medium text-purple-700 underline">
          Kembali ke daftar pesanan
        </Link>
      </div>
    );
  }
  return <p className="text-gray-500">Mencari pesanan {orderNo.toUpperCase()}...</p>;
};

export const AdminOrderRoute = () => {
  const { id } = useParams();
  if (ORDER_NO_RE.test(id || '')) return <OrderNoResolver key={id} orderNo={id} />;
  return <AdminOrderDetail />;
};
