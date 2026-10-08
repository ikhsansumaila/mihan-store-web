import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { adminFetch, qs, rupiah, perUnit, fmtTime } from './api';
import { ErrorBox, Modal, cardClass, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';
import { InfiniteFooter, peekListSnapshot, useInfiniteList } from './infiniteList';
import { STATUS, buildAdminSummaryText, formatFullAddress, statusLabel, waLink } from '../shop/format';
import { generateInvoicePdf, orderToInvoice } from '../invoicePdf';
import MoneyInput from '../components/MoneyInput';
import BottomSheet from '../components/BottomSheet';
import { AliasEditModal } from './AliasEditModal';
import { useAdminSummary } from './AdminLayout';

// Batas server (backend/order_logic.go): ongkir maks. Rp 10.000.000, subtotal maks. Rp 2.000.000.000.
const SHIPPING_MAX = 10000000;
const AMOUNT_DIGITS = 10;

export const OrderStatusBadge = ({ status }) => (
  <span className={`inline-block text-xs font-semibold border rounded-full px-2.5 py-0.5 whitespace-nowrap ${STATUS[status]?.cls || 'bg-gray-100'}`}>
    {statusLabel(status)}
  </span>
);

const emptyFilters = { from: '', to: '', q: '' };

// Tab status daftar pesanan: "Semua" + satu tab per status (urutan STATUS di shop/format.js).
export const ORDER_TABS = [{ value: '', label: 'Semua' }, ...Object.keys(STATUS).map((s) => ({ value: s, label: statusLabel(s) }))];

// ?status=... dari URL; nilai tidak dikenal (mis. "constructor") dianggap "Semua".
export const tabFromSearch = (searchParams) => {
  const st = searchParams.get('status');
  return st && Object.prototype.hasOwnProperty.call(STATUS, st) ? st : '';
};

// Jumlah per tab hanya dari data yang sudah ada (/api/admin/summary: pendingPayment & paid, seluruh pesanan,
// tidak terpengaruh pencarian/tanggal). Tab lain tanpa angka agar tidak perlu API baru.
const SUMMARY_COUNT_KEY = { pending_payment: 'pendingPayment', paid: 'paid' };

const StatusTabs = ({ active, counts, onSelect }) => {
  const scrollerRef = useRef(null);
  const activeRef = useRef(null);
  // Di HP tab aktif (mis. dari ?status=cancelled) bisa berada di luar layar: geser baris tab (hanya
  // horizontal, halaman tidak ikut bergulir) agar tab aktif terlihat.
  useEffect(() => {
    const box = scrollerRef.current;
    const el = activeRef.current;
    if (!box || !el) return;
    if (el.offsetLeft < box.scrollLeft || el.offsetLeft + el.offsetWidth > box.scrollLeft + box.clientWidth) {
      box.scrollLeft = Math.max(0, el.offsetLeft - 12);
    }
  }, [active]);
  // Panah kiri/kanan berpindah tab (pola tablist).
  const onKeyDown = (e) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const i = ORDER_TABS.findIndex((t) => t.value === active);
    const next = ORDER_TABS[(i + (e.key === 'ArrowRight' ? 1 : ORDER_TABS.length - 1)) % ORDER_TABS.length];
    onSelect(next.value);
    e.currentTarget.parentElement?.querySelector(`[data-status="${next.value || 'all'}"]`)?.focus();
  };
  return (
    // Tepi atas kartu tabel: satu baris yang bisa di-scroll horizontal di HP (halaman tidak ikut melebar).
    // Garis bawah baris tab = batas atas tabel; tab aktif berlatar sama dengan judul kolom (gray-50) dan
    // menutup garis itu (-mb-px) sehingga menyambung langsung ke tabel.
    <div ref={scrollerRef} className="relative overflow-x-auto bg-white [scrollbar-width:thin]" data-testid="order-status-tabs">
      <div role="tablist" aria-label="Status pesanan" className="flex w-max min-w-full gap-1 border-b border-gray-200 px-2 pt-2">
        {ORDER_TABS.map((t) => {
          const selected = t.value === active;
          const n = counts[t.value];
          return (
            <button
              key={t.value || 'all'}
              ref={selected ? activeRef : undefined}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls="order-list-panel"
              data-status={t.value || 'all'}
              onClick={() => onSelect(t.value)}
              onKeyDown={onKeyDown}
              className={`-mb-px flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-t-md border px-3 py-2.5 text-sm font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-purple-400 ${
                selected
                  ? 'border-gray-200 border-b-gray-50 bg-gray-50 text-purple-700 shadow-[inset_0_2px_0_0_#9333ea]'
                  : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
            >
              {t.label}
              {Number.isFinite(n) && (
                <span
                  data-testid="tab-count"
                  className={`rounded-full px-2 py-0.5 text-[11px] font-bold leading-none ${selected ? 'bg-purple-100 text-purple-700' : 'bg-gray-100 text-gray-600'}`}
                >
                  {n}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
};

// Nama pemesan (alias bila ada) + penerima; dipakai baris tabel dan kartu HP.
const OrderParty = ({ o, aliasTestId }) => (
  <>
    {o.customer?.alias ? (
      <>
        <div className="font-semibold text-purple-800" data-testid={aliasTestId}>
          {o.customer.alias}
        </div>
        <div className="text-xs text-gray-600">{o.customerName}</div>
      </>
    ) : (
      <div>{o.customerName}</div>
    )}
    <div className="text-xs text-gray-500">
      → {o.recipientName}, {o.city}
    </div>
  </>
);

const LIST_CACHE_ID = 'admin-orders';

export const OrdersList = () => {
  // Tab status disimpan di URL (?status=...): refresh/kembali tetap di tab yang sama, pintasan Dashboard
  // (?status=pending_payment) langsung membuka tabnya. Tanpa ?status = tab "Semua".
  const [searchParams, setSearchParams] = useSearchParams();
  const status = tabFromSearch(searchParams);
  const { summary } = useAdminSummary();
  // Kembali dari detail: filter pencarian/tanggal ikut dipulihkan dari snapshot daftar.
  const [initialFilters] = useState(() => peekListSnapshot(LIST_CACHE_ID)?.extra || emptyFilters);
  const [filters, setFilters] = useState(initialFilters);
  const [draft, setDraft] = useState(initialFilters);
  const topRef = useRef(null);
  // Gulir tanpa batas: ganti tab/pencarian/tanggal = mulai lagi dari halaman 1 (urutan parameter tetap).
  const list = useInfiniteList('/orders', { status, ...filters }, { cacheId: LIST_CACHE_ID, topRef });

  const selectTab = (value) => {
    if (value === status) return;
    const next = new URLSearchParams(searchParams);
    if (value) next.set('status', value);
    else next.delete('status');
    setSearchParams(next, { replace: true });
  };

  const counts = {};
  Object.entries(SUMMARY_COUNT_KEY).forEach(([s, key]) => {
    const n = summary?.orders?.[key];
    if (Number.isFinite(n)) counts[s] = n;
  });

  const apply = (e) => {
    e.preventDefault();
    setFilters({ ...draft, q: draft.q.trim() });
  };
  const set = (k) => (e) => setDraft({ ...draft, [k]: e.target.value });
  // Diteruskan ke detail agar tautan "Semua pesanan" kembali ke tab yang sama.
  const listSearch = searchParams.toString() ? `?${searchParams}` : '';
  const detailLink = (o) => ({ to: `/admin/orders/${o.id}`, state: { ordersSearch: listSearch }, onClick: () => list.remember(filters) });
  const empty = !list.loading && !list.error && list.items.length === 0;
  const firstLoad = list.loading && list.items.length === 0;

  return (
    <div>
      <h1 className="text-xl sm:text-2xl font-semibold text-gray-900 mb-4">Pesanan</h1>
      <form onSubmit={apply} className="bg-white rounded-lg border border-gray-200 shadow-sm p-4 mb-4 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 items-end">
        <label className="block lg:col-span-2">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Cari (no. pesanan, nama, alias, telepon)</span>
          <input className={inputClass} value={draft.q} onChange={set('q')} maxLength={100} placeholder="MS-261002-0001" />
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Dari tanggal</span>
          <input type="date" className={inputClass} value={draft.from} onChange={set('from')} />
        </label>
        <label className="block">
          <span className="block text-xs font-semibold text-gray-600 mb-1">Sampai tanggal</span>
          <input type="date" className={inputClass} value={draft.to} onChange={set('to')} />
        </label>
        <div className="flex gap-2 sm:col-span-2 lg:col-span-4 justify-end">
          <button
            type="button"
            className={btnSecondary}
            onClick={() => {
              setDraft(emptyFilters);
              setFilters(emptyFilters);
            }}
          >
            Reset
          </button>
          <button type="submit" className={btnPrimary}>
            Terapkan
          </button>
        </div>
      </form>
      <ErrorBox error={list.error} />
      {/* Kartu daftar: tab status menempel di tepi atas, tepat di atas judul kolom tabel (md+) / kartu (HP). */}
      <div ref={topRef} className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden" data-testid="order-list-card">
        <StatusTabs active={status} counts={counts} onSelect={selectTab} />
        <div id="order-list-panel" role="tabpanel" aria-label={`Pesanan: ${ORDER_TABS.find((t) => t.value === status).label}`}>
          {/* HP (< md): kartu per pesanan, seluruh kartu bisa diketuk; tanpa geser horizontal. */}
          <ul className="space-y-2 bg-gray-50 p-2 md:hidden" data-testid="order-cards">
            {firstLoad && <li className="p-6 text-center text-sm text-gray-500">Memuat...</li>}
            {empty && <li className="p-6 text-center text-sm text-gray-500">Belum ada pesanan.</li>}
            {list.items.map((o) => (
              <li key={o.id}>
                <Link
                  {...detailLink(o)}
                  data-testid="order-card"
                  className="block rounded-lg border border-gray-200 bg-white p-3 text-sm shadow-sm active:bg-purple-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400"
                >
                  <div className="flex items-start justify-between gap-2">
                    <span className="font-semibold text-purple-700 [overflow-wrap:anywhere]">{o.orderNo}</span>
                    <OrderStatusBadge status={o.status} />
                  </div>
                  <div className="mt-1 min-w-0 text-gray-800 [overflow-wrap:anywhere]">
                    <OrderParty o={o} />
                  </div>
                  <div className="mt-2 flex items-end justify-between gap-2 border-t border-gray-100 pt-2">
                    <span className="text-xs text-gray-500">
                      {fmtTime(o.createdAt)} · {o.itemCount} item
                    </span>
                    <span className="whitespace-nowrap font-semibold text-gray-900">{rupiah(o.total)}</span>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
          {/* md ke atas: tabel. */}
          <div className="hidden overflow-x-auto md:block">
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
                {firstLoad ? (
                  <tr>
                    <td colSpan="6" className="p-6 text-center text-gray-500">
                      Memuat...
                    </td>
                  </tr>
                ) : empty ? (
                  <tr>
                    <td colSpan="6" className="p-6 text-center text-gray-500">
                      Belum ada pesanan.
                    </td>
                  </tr>
                ) : (
                  list.items.map((o) => (
                    <tr key={o.id} className="border-t border-gray-100 hover:bg-purple-50">
                      <td className="p-3 font-semibold whitespace-nowrap">
                        <Link {...detailLink(o)} className="text-purple-700 hover:underline">
                          {o.orderNo}
                        </Link>
                      </td>
                      <td className="p-3 whitespace-nowrap">{fmtTime(o.createdAt)}</td>
                      <td className="p-3 min-w-[16rem] [overflow-wrap:anywhere]">
                        <OrderParty o={o} aliasTestId="order-alias" />
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
        </div>
      </div>
      <InfiniteFooter list={list} noun="pesanan" />
    </div>
  );
};

const digits = (v) => String(v ?? '').replace(/[^0-9]/g, '');

// Ringkasan perubahan harga untuk dialog konfirmasi. amountsChanged mengikuti backend (admin_orders.go):
// diskon/ongkir/total berubah nilainya -> pelanggan menerima notifikasi "Ongkir sudah dikonfirmasi".
export const pricingChanges = (order, d, s, note) => {
  const oldD = Number(order.discount || 0);
  const oldS = Number(order.shippingFee || 0);
  const oldNote = (order.discountNote || '').trim();
  const newNote = (note || '').trim();
  const total = order.subtotal - d + s;
  const amountsChanged = oldD !== d || oldS !== s || Number(order.total) !== total;
  const noteChanged = oldNote !== newNote;
  return { oldD, oldS, total, amountsChanged, noteChanged, any: amountsChanged || noteChanged, newNote };
};

const ChangeRow = ({ label, from, to, testId }) => (
  <div className="flex items-baseline justify-between gap-3 py-1.5" data-testid={testId}>
    <span className="text-gray-600">{label}</span>
    <span className="text-right font-medium text-gray-900">
      {from === to ? (
        rupiah(to)
      ) : (
        <>
          <span className="text-gray-500 line-through decoration-gray-300">{rupiah(from)}</span>
          <span aria-hidden="true"> → </span>
          <span className="sr-only"> menjadi </span>
          {rupiah(to)}
        </>
      )}
    </span>
  </div>
);

const PricingForm = ({ order, onSaved }) => {
  const [discount, setDiscount] = useState(String(order.discount || 0));
  const [note, setNote] = useState(order.discountNote || '');
  const [shipping, setShipping] = useState(String(order.shippingFee || 0));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [confirm, setConfirm] = useState(null); // ringkasan perubahan saat dialog konfirmasi terbuka
  const [confirmError, setConfirmError] = useState(null);
  const savingRef = useRef(false);
  const d = Number(digits(discount) || 0);
  const s = Number(digits(shipping) || 0);
  const preview = order.subtotal - d + s;

  const doSave = async (inSheet) => {
    if (savingRef.current) return; // cegah klik ganda
    savingRef.current = true;
    setSaving(true);
    try {
      const res = await adminFetch(`/orders/${order.id}/pricing`, { method: 'PATCH', body: { discount: d, discountNote: note, shippingFee: s } });
      setConfirm(null);
      onSaved(res);
    } catch (err) {
      if (inSheet) setConfirmError(err);
      else setError(err);
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  const save = async (e) => {
    e.preventDefault();
    setError(null);
    if (d > order.subtotal) return setError(new Error('Diskon tidak boleh melebihi subtotal'));
    if (s > SHIPPING_MAX) return setError(new Error('Ongkir maksimal Rp 10.000.000'));
    const ch = pricingChanges(order, d, s, note);
    if (!ch.any) return doSave(false); // tanpa perubahan: perilaku lama (langsung simpan)
    setConfirmError(null);
    setConfirm(ch);
    return undefined;
  };

  const closeConfirm = () => {
    if (savingRef.current) return;
    setConfirm(null);
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

      <BottomSheet open={!!confirm} onClose={closeConfirm} labelledBy="pricing-confirm-title" testId="pricing-confirm">
        {confirm && (
          <>
            <div className="flex items-start justify-between gap-3">
              <h2 id="pricing-confirm-title" className="text-lg font-bold text-gray-900">
                Konfirmasi perubahan harga
              </h2>
              <button
                type="button"
                onClick={closeConfirm}
                aria-label="Tutup"
                className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            </div>
            <p className="mt-2 text-sm text-gray-700">
              Anda akan mengubah harga pesanan <strong>{order.orderNo}</strong>:
            </p>
            <div className="mt-3 divide-y divide-gray-100 rounded-xl bg-purple-50 px-4 py-2 text-sm">
              <ChangeRow label="Ongkir" from={confirm.oldS} to={s} testId="confirm-shipping" />
              <ChangeRow label="Diskon" from={confirm.oldD} to={d} testId="confirm-discount" />
              {(confirm.newNote || confirm.noteChanged) && (
                <div className="flex items-baseline justify-between gap-3 py-1.5" data-testid="confirm-note">
                  <span className="text-gray-600">Keterangan diskon</span>
                  <span className="min-w-0 text-right font-medium text-gray-900 [overflow-wrap:anywhere]">{confirm.newNote || '(dikosongkan)'}</span>
                </div>
              )}
              <div className="flex items-baseline justify-between gap-3 py-2" data-testid="confirm-total">
                <span className="font-semibold text-gray-900">Total baru</span>
                <strong className="text-base text-purple-800">{rupiah(confirm.total)}</strong>
              </div>
            </div>
            {confirm.amountsChanged && (
              <p className="mt-3 text-sm text-gray-600" data-testid="confirm-notify">
                Pelanggan akan menerima notifikasi.
              </p>
            )}
            {confirmError && (
              <div className="mt-3">
                <ErrorBox error={confirmError} />
              </div>
            )}
            <div className="mt-4 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <button type="button" className={btnSecondary} onClick={closeConfirm} disabled={saving}>
                Batal
              </button>
              <button type="button" className={btnPrimary} onClick={() => doSave(true)} disabled={saving}>
                {saving ? 'Menyimpan...' : 'Ya, simpan'}
              </button>
            </div>
          </>
        )}
      </BottomSheet>
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
  const { state: navState } = useLocation();
  // Kembali ke tab daftar asal (?status=...) bila datang dari daftar pesanan; hanya status yang dikenal.
  const backSearch = (() => {
    const raw = typeof navState?.ordersSearch === 'string' ? navState.ordersSearch : '';
    const st = tabFromSearch(new URLSearchParams(raw));
    return st ? `?status=${st}` : '';
  })();
  const [order, setOrder] = useState(null);
  const [settings, setSettings] = useState({});
  const [error, setError] = useState(null);
  const [action, setAction] = useState(null);
  const [printing, setPrinting] = useState(false);
  const [useAlias, setUseAlias] = useState(false);
  const [editAlias, setEditAlias] = useState(false);

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
  const alias = order.customer?.alias || '';
  const wa = waLink(r.phone, buildAdminSummaryText(order, settings));
  const printInvoice = async () => {
    setPrinting(true);
    try {
      await generateInvoicePdf(orderToInvoice(order, settings, { useAlias: useAlias && !!alias }));
    } finally {
      setPrinting(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link to={`/admin/orders${backSearch}`} className="text-sm text-purple-700 hover:underline">
            ‹ Semua pesanan
          </Link>
          <h1 className="text-xl sm:text-2xl font-semibold text-gray-900 flex flex-wrap items-center gap-3">
            {order.orderNo} <OrderStatusBadge status={order.status} />
          </h1>
          <div className="text-sm text-gray-500">Dibuat {fmtTime(order.createdAt)}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          {order.status !== 'cancelled' && (
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className={btnSecondary} onClick={printInvoice} disabled={printing}>
                {printing ? 'Menyiapkan...' : 'Cetak invoice'}
              </button>
              {alias && (
                <label className="inline-flex items-center gap-1.5 text-sm text-gray-700" title={`Nama di invoice: ${alias}`}>
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-gray-300 text-purple-700 focus:ring-purple-500"
                    checked={useAlias}
                    onChange={(e) => setUseAlias(e.target.checked)}
                    data-testid="invoice-use-alias"
                  />
                  Pakai nama alias
                </label>
              )}
            </div>
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
            <div className="text-sm space-y-0.5" data-testid="order-customer">
              <div className="text-xs text-gray-500">Alias (hanya admin)</div>
              <div className="flex flex-wrap items-center gap-2">
                {alias ? (
                  <span className="font-semibold text-purple-800" data-testid="order-detail-alias">
                    {alias}
                  </span>
                ) : (
                  <span className="text-xs italic text-gray-400">Belum ada alias</span>
                )}
                {order.customer?.id ? (
                  <button type="button" className="text-xs font-medium text-purple-700 underline" onClick={() => setEditAlias(true)}>
                    {alias ? 'Ubah alias' : 'Beri alias'}
                  </button>
                ) : null}
              </div>
              <div className="font-medium pt-1">{order.customer?.name}</div>
              <div className="text-gray-600">@{order.customer?.username}</div>
              <div className="text-gray-600 break-all">{order.customer?.email}</div>
              {order.customer?.id ? (
                <Link to={`/admin/customers/${order.customer.id}`} className="inline-block pt-1 text-xs font-medium text-purple-700 hover:underline">
                  Lihat halaman pelanggan ›
                </Link>
              ) : null}
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

      {editAlias && (
        <AliasEditModal
          customer={order.customer}
          onClose={() => setEditAlias(false)}
          onSaved={(a) => {
            setOrder((o) => ({ ...o, customer: { ...o.customer, alias: a } }));
            if (!a) setUseAlias(false);
            setEditAlias(false);
          }}
        />
      )}

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
