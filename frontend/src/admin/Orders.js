import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { ADMIN_HEADER, adminFetch, qs, rupiah, perUnit, fmtTime } from './api';
import { ErrorBox, Modal, cardClass, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';
import { InfiniteFooter, peekListSnapshot, useInfiniteList } from './infiniteList';
import { STATUS, buildAdminSummaryText, formatFullAddress, statusLabel, waLink } from '../shop/format';
import { generateInvoicePdf, orderToInvoice } from '../invoicePdf';
import MoneyInput from '../components/MoneyInput';
import BottomSheet from '../components/BottomSheet';
import { AliasEditModal } from './AliasEditModal';
import { useAdminSummary, useSummaryPolling } from './AdminLayout';

// Batas server (backend/order_logic.go): ongkir maks. Rp 10.000.000, subtotal maks. Rp 2.000.000.000.
const SHIPPING_MAX = 10000000;
const AMOUNT_DIGITS = 10;

// Penanda "Bukti" di daftar: pesanan menunggu pembayaran yang sudah punya bukti transfer.
export const ProofBadge = ({ o }) =>
  o.status === 'pending_payment' && o.hasPaymentProof ? (
    <span
      className="inline-block whitespace-nowrap rounded-full border border-blue-300 bg-blue-50 px-2 py-0.5 text-xs font-semibold text-blue-800"
      data-testid="admin-proof-badge"
      title="Pelanggan sudah mengunggah bukti transfer"
    >
      Bukti
    </span>
  ) : null;

export const OrderStatusBadge = ({ status }) => (
  <span className={`inline-block text-xs font-semibold border rounded-full px-2.5 py-0.5 whitespace-nowrap ${STATUS[status]?.cls || 'bg-gray-100'}`}>
    {statusLabel(status)}
  </span>
);

const emptyFilters = { from: '', to: '', q: '' };

// Tab status daftar pesanan: "Semua" + satu tab per status (urutan STATUS di shop/format.js:
// Menunggu konfirmasi, Menunggu pembayaran, Dibayar, Selesai, Dibatalkan).
export const ORDER_TABS = [{ value: '', label: 'Semua' }, ...Object.keys(STATUS).map((s) => ({ value: s, label: statusLabel(s) }))];

// Tab bawaan tanpa ?status= : "Menunggu konfirmasi" (pesanan yang butuh tindakan admin).
// Tab "Semua" memakai ?status=all agar tetap bisa dibagikan/dipulihkan.
export const DEFAULT_ORDER_TAB = 'pending_confirmation';
export const ALL_TAB_PARAM = 'all';

// ?status=... dari URL; nilai tidak dikenal (mis. "constructor") dianggap "Semua".
export const tabFromSearch = (searchParams) => {
  const st = searchParams.get('status');
  if (st === null) return DEFAULT_ORDER_TAB;
  return st && Object.prototype.hasOwnProperty.call(STATUS, st) ? st : '';
};

// Jumlah per tab hanya dari data yang sudah ada (/api/admin/summary: pendingPayment & paid, seluruh pesanan,
// tidak terpengaruh pencarian/tanggal). Tab lain tanpa angka agar tidak perlu API baru.
const SUMMARY_COUNT_KEY = { pending_confirmation: 'pendingConfirmation', pending_payment: 'pendingPayment', paid: 'paid' };

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
  // (?status=pending_confirmation) langsung membuka tabnya. Tanpa ?status = tab "Menunggu konfirmasi";
  // tab "Semua" = ?status=all.
  const [searchParams, setSearchParams] = useSearchParams();
  const status = tabFromSearch(searchParams);
  const { summary, refresh: refreshSummary } = useAdminSummary();
  // Kembali dari detail: filter pencarian/tanggal ikut dipulihkan dari snapshot daftar.
  const [initialFilters] = useState(() => peekListSnapshot(LIST_CACHE_ID)?.extra || emptyFilters);
  const [filters, setFilters] = useState(initialFilters);
  const [draft, setDraft] = useState(initialFilters);
  const topRef = useRef(null);
  // Gulir tanpa batas: ganti tab/pencarian/tanggal = mulai lagi dari halaman 1 (urutan parameter tetap).
  const list = useInfiniteList('/orders', { status, ...filters }, { cacheId: LIST_CACHE_ID, topRef });
  // Angka tab & lencana ikut segar: saat daftar dibuka (termasuk kembali dari detail / daftar dipulihkan),
  // saat tab atau filter berganti, dan polling ringan selama daftar terbuka & terlihat.
  const filterKey = JSON.stringify(filters);
  useEffect(() => {
    refreshSummary();
  }, [status, filterKey, refreshSummary]);
  useSummaryPolling();

  const selectTab = (value) => {
    if (value === status) return;
    const next = new URLSearchParams(searchParams);
    if (value === DEFAULT_ORDER_TAB) next.delete('status');
    else next.set('status', value || ALL_TAB_PARAM);
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
                    <span className="flex flex-wrap items-center gap-1">
                      <ProofBadge o={o} />
                      <OrderStatusBadge status={o.status} />
                    </span>
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
                        <span className="flex flex-wrap items-center gap-1">
                          <OrderStatusBadge status={o.status} />
                          <ProofBadge o={o} />
                        </span>
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

// ---------- Saran ongkir (konfirmasi pesanan) ----------
const SUGGEST_TIMEOUT_MS = 3000;

export const relativeDays = (iso, now = new Date()) => {
  if (!iso) return '';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '';
  const day = (x) => Math.floor((x.getTime() + 7 * 3600 * 1000) / 86400000); // hari menurut WIB
  const n = day(now) - day(t);
  if (n <= 0) return 'hari ini';
  if (n === 1) return 'kemarin';
  if (n < 30) return `${n} hari lalu`;
  return fmtTime(iso);
};

const SOURCE_SHORT = { default: 'default kecamatan', district: 'terakhir ke kecamatan', regency: 'terakhir ke kota/kab.', customer: 'terakhir pelanggan ini' };

export const suggestionLabel = (sg) => {
  switch (sg.source) {
    case 'default':
      return `Default kecamatan ${sg.regionLabel || ''}`.trim();
    case 'district':
      return `Terakhir ke kecamatan ${sg.regionLabel || ''}`.trim();
    case 'regency':
      return `Terakhir ke ${sg.regionLabel || 'kota/kabupaten ini'}`;
    default:
      return 'Terakhir pelanggan ini';
  }
};

const suggestionDetail = (sg) => {
  const parts = [];
  if (sg.orderNo) parts.push(sg.orderNo);
  if (sg.orderNo && sg.date) parts.push(relativeDays(sg.date));
  const extra = (sg.sources || []).filter((x) => x !== sg.source).map((x) => SOURCE_SHORT[x] || x);
  if (extra.length) parts.push(`sama dengan ${extra.join(', ')}`);
  return parts.join(' · ');
};

// Kartu "Diskon & ongkir" (ringkasan baca-saja + tombol) dan bottom sheet isian harga.
// Mode "confirm" (pending_confirmation): POST /confirm, saran ongkir inline, ringkasan selalu tampil di sheet.
// Mode "edit" (pending_payment): PATCH /pricing; tanpa perubahan = tombol simpan nonaktif (tidak dikirim).

const CloseX = ({ onClick }) => (
  <button
    type="button"
    onClick={onClick}
    aria-label="Tutup"
    className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
  >
    <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
      <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
    </svg>
  </button>
);

// Gulirkan kolom ke tengah sheet saat difokus (keyboard HP muncul).
const keepVisible = (e) => {
  const el = e.target;
  setTimeout(() => {
    if (typeof el?.scrollIntoView === 'function') el.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }, 300);
};

// Alur dua langkah dalam SATU BottomSheet (hanya satu sheet terbuka; konten berganti tanpa kedip):
//   1. "edit"   — "Isi ongkir & diskon" / "Ubah diskon & ongkir": kolom Ongkir (+ saran inline saat konfirmasi),
//                 Diskon, Keterangan, Subtotal/Total sementara pasif. Batal | Lanjut.
//   2. "review" — "Konfirmasi pesanan <nomor>" / "Konfirmasi perubahan harga": ringkasan lama -> baru, Total baru,
//                 kalimat notifikasi, galat server. Kembali (isian tetap) | Ya, konfirmasi / Ya, simpan.
// Menutup lewat X / latar / Escape (di langkah mana pun) atau Batal = seluruh alur ditutup & draf dibuang.
const PricingPanel = ({ order, onSaved }) => {
  const confirmMode = !!order.canConfirm;
  const [step, setStep] = useState(null); // null | 'edit' | 'review'
  const [discount, setDiscount] = useState('');
  const [note, setNote] = useState('');
  const [shipping, setShipping] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [sugg, setSugg] = useState(null); // respons /shipping-suggestions (mode konfirmasi)
  const savingRef = useRef(false);
  const flowRef = useRef(0); // id alur; respons dari alur lama (basi) diabaikan
  const mounted = useRef(true);
  const shippingRef = useRef(null);
  const discountRef = useRef(null);
  const reviewTitleRef = useRef(null);
  const d = Number(digits(discount) || 0);
  const s = Number(digits(shipping) || 0);
  const ch = pricingChanges(order, d, s, note);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      flowRef.current += 1;
    };
  }, []);

  // Fokus saat langkah berganti (setelah BottomSheet memindahkan fokus ke panel): judul sheet 2; saat
  // pertama dibuka kolom ongkir (konfirmasi) / diskon (ubah); saat "Kembali" dari sheet 2 kolom ongkir.
  const prevStep = useRef(null);
  useEffect(() => {
    const from = prevStep.current;
    prevStep.current = step;
    if (!step) return undefined;
    const t = setTimeout(() => {
      if (step === 'review') reviewTitleRef.current?.focus();
      else (confirmMode || from === 'review' ? shippingRef : discountRef).current?.focus();
    }, 60);
    return () => clearTimeout(t);
  }, [step, confirmMode]);

  const open = () => {
    const flow = flowRef.current + 1;
    flowRef.current = flow;
    setDiscount(String(order.discount || 0));
    setNote(order.discountNote || '');
    setShipping(String(order.shippingFee || 0));
    setError(null);
    setSugg(null);
    setStep('edit');
    if (confirmMode) {
      let timedOut = false;
      setTimeout(() => {
        timedOut = true; // saran yang terlambat (> 3 dtk) diabaikan, tanpa galat
      }, SUGGEST_TIMEOUT_MS);
      adminFetch(`/orders/${order.id}/shipping-suggestions`)
        .then((r) => {
          if (mounted.current && flowRef.current === flow && !timedOut && r && Array.isArray(r.suggestions)) setSugg(r);
        })
        .catch(() => {});
    }
  };

  const closeAll = () => {
    if (savingRef.current) return;
    flowRef.current += 1;
    setStep(null); // draf dibuang (diisi ulang dari pesanan saat dibuka lagi)
  };

  const invalid =
    d > order.subtotal ? 'Diskon tidak boleh melebihi subtotal' : s > SHIPPING_MAX ? 'Ongkir maksimal Rp 10.000.000' : null;
  const noChange = !confirmMode && !ch.any;

  const next = (e) => {
    e?.preventDefault();
    if (invalid || noChange) return;
    setError(null);
    setStep('review');
  };

  const submit = async () => {
    if (invalid || noChange || savingRef.current) return; // cegah klik ganda / kirim tanpa perubahan
    const flow = flowRef.current;
    savingRef.current = true;
    setSaving(true);
    setError(null);
    try {
      const body = { discount: d, discountNote: note, shippingFee: s };
      const res = confirmMode
        ? await adminFetch(`/orders/${order.id}/confirm`, { method: 'POST', body })
        : await adminFetch(`/orders/${order.id}/pricing`, { method: 'PATCH', body });
      if (!mounted.current || flowRef.current !== flow) return;
      // Setelah KONFIRMASI sukses: tawarkan "jadikan default ongkir wilayah" bila punya kecamatan, ongkir > 0,
      // dan berbeda dari default yang berlaku (info dari saran yang sudah diambil).
      const fee = Number(res?.shippingFee ?? s);
      const current = sugg?.currentDefault?.fee ?? null;
      const offer =
        confirmMode && sugg?.canSetDefault && fee > 0 && (current === null || Number(current) !== fee)
          ? { orderId: order.id, orderNo: order.orderNo, fee, districtName: sugg.region?.districtName || '', current: current === null ? null : Number(current) }
          : null;
      savingRef.current = false;
      setStep(null);
      onSaved(res, offer ? { defaultOffer: offer } : undefined);
    } catch (err) {
      if (mounted.current && flowRef.current === flow) setError(err);
    } finally {
      savingRef.current = false;
      if (mounted.current) setSaving(false);
    }
  };

  const options = sugg
    ? [
        ...sugg.suggestions.map((sg, i) => ({ key: `s${i}`, title: suggestionLabel(sg), fee: Number(sg.fee), detail: suggestionDetail(sg), source: sg.source })),
        { key: 'zero', title: 'Rp 0 (kurir dipesan pembeli)', fee: 0, detail: '', source: 'zero' },
      ]
    : [];
  const inputCls = `${inputClass} text-base`;
  const shippingText = confirmMode && !order.shippingFee ? 'belum diisi' : rupiah(order.shippingFee);

  const editStep = (
    <form onSubmit={next} noValidate>
      <div className="flex items-start justify-between gap-3">
        <h2 id="price-sheet-title" className="text-lg font-bold text-gray-900">
          {confirmMode ? 'Isi ongkir & diskon' : 'Ubah diskon & ongkir'}
        </h2>
        <CloseX onClick={closeAll} />
      </div>
      {confirmMode && <p className="text-sm text-gray-500">Pesanan {order.orderNo}</p>}

      <label className="mt-3 block">
        <span className="mb-1 block text-xs font-semibold text-gray-600">Ongkir (Rp)</span>
        <MoneyInput
          ref={shippingRef}
          className={inputCls}
          value={shipping}
          onValueChange={(v) => setShipping(v)}
          onFocus={keepVisible}
          maxDigits={AMOUNT_DIGITS}
          min={0}
          max={SHIPPING_MAX}
        />
      </label>
      {options.length > 1 && (
        <div className="mt-2" data-testid="shipping-options" role="group" aria-label="Saran ongkir">
          <div className="flex flex-col gap-1.5">
            {options.map((opt) => {
              const active = s === opt.fee && digits(shipping) !== '';
              return (
                <button
                  key={opt.key}
                  type="button"
                  data-option={opt.source}
                  aria-pressed={active}
                  onClick={() => setShipping(String(opt.fee))}
                  className={`flex min-h-[2.75rem] w-full items-center justify-between gap-3 rounded-lg border px-3 py-2 text-left text-sm transition ${
                    active ? 'border-purple-500 bg-purple-50 ring-1 ring-purple-500' : 'border-gray-200 hover:bg-gray-50'
                  }`}
                >
                  <span className="min-w-0">
                    <span className="block font-medium text-gray-900">{opt.title}</span>
                    {opt.detail && <span className="block text-xs text-gray-500">{opt.detail}</span>}
                  </span>
                  {opt.source !== 'zero' && <span className="shrink-0 font-bold text-purple-800">{rupiah(opt.fee)}</span>}
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="block">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Diskon (Rp)</span>
          <MoneyInput
            ref={discountRef}
            className={inputCls}
            value={discount}
            onValueChange={(v) => setDiscount(v)}
            onFocus={keepVisible}
            maxDigits={AMOUNT_DIGITS}
            min={0}
            max={order.subtotal}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Keterangan diskon (opsional)</span>
          <input className={inputCls} maxLength={255} value={note} onChange={(e) => setNote(e.target.value)} onFocus={keepVisible} />
        </label>
      </div>
      {invalid && (
        <p role="alert" className="mt-2 text-sm text-red-700" data-testid="price-invalid">
          {invalid}
        </p>
      )}
      <div className="mt-3 space-y-0.5 rounded-lg bg-gray-50 px-3 py-2 text-sm text-gray-700" data-testid="price-draft-total">
        <div className="flex justify-between gap-3">
          <span>Subtotal</span>
          <span>{rupiah(order.subtotal)}</span>
        </div>
        <div className="flex justify-between gap-3 font-semibold">
          <span>Total sementara</span>
          <span className={ch.total < 0 ? 'text-red-600' : 'text-purple-700'}>{rupiah(Math.max(ch.total, 0))}</span>
        </div>
      </div>
      {noChange && !invalid && (
        <p className="mt-2 text-sm text-gray-500" data-testid="price-nochange">
          Belum ada perubahan.
        </p>
      )}
      <div className="mt-4 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" className={btnSecondary} onClick={closeAll}>
          Batal
        </button>
        <button type="submit" className={btnPrimary} disabled={!!invalid || noChange}>
          Lanjut
        </button>
      </div>
    </form>
  );

  const reviewStep = (
    <div>
      <div className="flex items-start justify-between gap-3">
        <h2 id="price-review-title" ref={reviewTitleRef} tabIndex={-1} className="text-lg font-bold text-gray-900 focus:outline-none">
          {confirmMode ? `Konfirmasi pesanan ${order.orderNo}` : 'Konfirmasi perubahan harga'}
        </h2>
        <CloseX onClick={closeAll} />
      </div>
      <p className="mt-2 text-sm text-gray-700">
        {confirmMode ? 'Anda akan mengonfirmasi pesanan ' : 'Anda akan mengubah harga pesanan '}
        <strong>{order.orderNo}</strong>:
      </p>
      <div className="mt-3 divide-y divide-gray-100 rounded-xl bg-purple-50 px-4 py-2 text-sm" data-testid="price-summary">
        <ChangeRow label="Ongkir" from={ch.oldS} to={s} testId="confirm-shipping" />
        <ChangeRow label="Diskon" from={ch.oldD} to={d} testId="confirm-discount" />
        {(ch.newNote || ch.noteChanged) && (
          <div className="flex items-baseline justify-between gap-3 py-1.5" data-testid="confirm-note">
            <span className="text-gray-600">Keterangan diskon</span>
            <span className="min-w-0 text-right font-medium text-gray-900 [overflow-wrap:anywhere]">{ch.newNote || '(dikosongkan)'}</span>
          </div>
        )}
        <div className="flex items-baseline justify-between gap-3 py-2" data-testid="confirm-total">
          <span className="font-semibold text-gray-900">Total baru</span>
          <strong className="text-base text-purple-800">{rupiah(ch.total)}</strong>
        </div>
      </div>
      {confirmMode ? (
        <p className="mt-3 text-sm text-gray-600" data-testid="confirm-notify">
          Pelanggan akan dinotifikasi dan diminta membayar.
        </p>
      ) : (
        ch.amountsChanged && (
          <p className="mt-3 text-sm text-gray-600" data-testid="confirm-notify">
            Pelanggan akan menerima notifikasi.
          </p>
        )
      )}
      {error && (
        <div className="mt-3">
          <ErrorBox error={error} />
        </div>
      )}
      <div className="mt-4 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" className={btnSecondary} onClick={() => setStep('edit')} disabled={saving}>
          Kembali
        </button>
        <button type="button" className={btnPrimary} onClick={submit} disabled={saving}>
          {saving ? 'Menyimpan...' : confirmMode ? 'Ya, konfirmasi' : 'Ya, simpan'}
        </button>
      </div>
    </div>
  );

  return (
    <div>
      <div className="space-y-1 text-sm" data-testid="pricing-summary">
        <div className="flex justify-between gap-3">
          <span className="text-gray-600">Subtotal</span>
          <span>{rupiah(order.subtotal)}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-gray-600">Diskon{order.discountNote ? ` (${order.discountNote})` : ''}</span>
          <span>{order.discount > 0 ? `-${rupiah(order.discount)}` : rupiah(0)}</span>
        </div>
        <div className="flex justify-between gap-3">
          <span className="text-gray-600">Ongkir</span>
          <span data-testid="pricing-shipping">{shippingText}</span>
        </div>
        <div className="flex justify-between gap-3 border-t border-gray-100 pt-1 font-semibold">
          <span>Total</span>
          <span className="text-purple-700">{rupiah(order.total)}</span>
        </div>
      </div>
      <div className="mt-3 flex justify-end">
        <button type="button" className={btnPrimary} onClick={open}>
          {confirmMode ? 'Konfirmasi pesanan' : 'Ubah diskon & ongkir'}
        </button>
      </div>
      <BottomSheet
        open={!!step}
        onClose={closeAll}
        labelledBy={step === 'review' ? 'price-review-title' : 'price-sheet-title'}
        testId={step === 'review' ? 'price-review' : 'price-sheet'}
      >
        {step === 'review' ? reviewStep : editStep}
      </BottomSheet>
    </div>
  );
};

// ---------- Bukti pembayaran (admin) ----------
// Gambar diambil dengan permintaan admin berotorisasi (cookie Cloudflare Access) -> blob URL; tidak ada URL
// publik/unduhan statis. Blob URL dibebaskan saat kartu dilepas atau bukti berganti.
export const useAdminProofImage = (orderId, version) => {
  const [state, setState] = useState({ url: '', error: false });
  useEffect(() => {
    if (!version) {
      setState({ url: '', error: false });
      return undefined;
    }
    let alive = true;
    let url = '';
    fetch(`/api/admin/orders/${orderId}/payment-proof`, { credentials: 'same-origin', redirect: 'manual', headers: { ...ADMIN_HEADER } })
      .then((res) => {
        if (!res.ok || !(res.headers.get('content-type') || '').startsWith('image/')) throw new Error('gagal');
        return res.blob();
      })
      .then((blob) => {
        if (!alive) return;
        url = URL.createObjectURL(blob);
        setState({ url, error: false });
      })
      .catch(() => alive && setState({ url: '', error: true }));
    return () => {
      alive = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [orderId, version]);
  return state;
};

const AdminProofCard = ({ order }) => {
  const p = order.paymentProof;
  const { url, error } = useAdminProofImage(order.id, p?.uploadedAt || '');
  const [open, setOpen] = useState(false);
  if (!p) return <p className="text-sm text-gray-500" data-testid="admin-proof-empty">Belum ada bukti.</p>;
  const thumb = (cls) =>
    url ? (
      <img src={url} alt="Bukti pembayaran" className={`${cls} rounded-lg border border-gray-200 object-contain bg-gray-50`} data-testid="admin-proof-img" />
    ) : (
      <span className={`${cls} flex items-center justify-center rounded-lg border border-dashed border-gray-300 text-xs text-gray-400`}>
        {error ? 'Gagal memuat bukti' : 'Memuat...'}
      </span>
    );
  return (
    <div className="flex items-center gap-3" data-testid="admin-proof">
      <button type="button" onClick={() => setOpen(true)} aria-label="Perbesar bukti pembayaran" className="shrink-0">
        {thumb('h-24 w-24')}
      </button>
      <div className="text-sm text-gray-700">
        <div>Diunggah {fmtTime(p.uploadedAt)}</div>
        <button type="button" className="mt-1 text-purple-700 underline" onClick={() => setOpen(true)}>
          Lihat bukti
        </button>
      </div>
      <BottomSheet open={open} onClose={() => setOpen(false)} labelledBy="admin-proof-title" testId="admin-proof-viewer">
        <div className="flex items-start justify-between gap-3">
          <h2 id="admin-proof-title" className="text-lg font-bold text-gray-900">
            Bukti pembayaran {order.orderNo}
          </h2>
          <CloseX onClick={() => setOpen(false)} />
        </div>
        <div className="mt-3">{thumb('w-full max-h-[75vh] min-h-[8rem]')}</div>
      </BottomSheet>
    </div>
  );
};

// Popup setelah "Konfirmasi pesanan" sukses: jadikan ongkir pesanan ini default kecamatannya?
// Nominal TIDAK dikirim: backend membaca ongkir & kecamatan dari pesanan (POST /shipping-default).
export const DefaultOfferSheet = ({ offer, onClose, onDone }) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const busyRef = useRef(false);
  const accept = async () => {
    if (busyRef.current) return; // cegah klik ganda
    busyRef.current = true;
    setBusy(true);
    setError(null);
    try {
      const r = await adminFetch(`/orders/${offer.orderId}/shipping-default`, { method: 'POST', body: {} });
      onDone(r || {});
    } catch (err) {
      setError(err);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };
  const close = () => {
    if (!busyRef.current) onClose();
  };
  const kec = offer.districtName ? `Kec. ${offer.districtName}` : 'kecamatan ini';
  return (
    <BottomSheet open onClose={close} labelledBy="default-offer-title" testId="default-offer">
      <div className="flex items-start justify-between gap-3">
        <h2 id="default-offer-title" className="text-lg font-bold text-gray-900">
          Jadikan default ongkir wilayah?
        </h2>
        <button
          type="button"
          onClick={close}
          aria-label="Tutup"
          className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <p className="mt-2 text-sm text-gray-700">
        Ongkir <strong>{rupiah(offer.fee)}</strong> untuk {kec} baru saja dikonfirmasi. Jadikan <strong>{rupiah(offer.fee)}</strong> sebagai default
        ongkir untuk kecamatan ini? Pesanan berikutnya ke kecamatan ini akan mendapat saran ongkir ini.
      </p>
      {offer.current !== null && (
        <p className="mt-2 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900" data-testid="default-offer-replace">
          Ini akan mengganti default sekarang ({rupiah(offer.current)}).
        </p>
      )}
      {error && (
        <div className="mt-3">
          <ErrorBox error={error} />
        </div>
      )}
      <div className="mt-4 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" className={btnSecondary} onClick={close} disabled={busy}>
          {error ? 'Tutup' : 'Tidak'}
        </button>
        <button type="button" className={btnPrimary} onClick={accept} disabled={busy}>
          {busy ? 'Menyimpan...' : error ? 'Coba lagi' : 'Ya, jadikan default'}
        </button>
      </div>
    </BottomSheet>
  );
};

// Konfirmasi "Tandai Dibayar" (satu sheet: konfirmasi bukti + catatan pembayaran opsional yang sudah ada).
// Tanpa bukti: "Pelanggan belum melampirkan bukti pembayaran, yakin?". Dengan bukti: "Bukti transfer sudah benar?"
// + thumbnail bukti (fetch admin -> blob; gagal memuat tidak menghalangi konfirmasi).
const PaidConfirmSheet = ({ order, onClose, onSaved }) => {
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const savingRef = useRef(false);
  const hasProof = !!order.paymentProof;
  const { url, error: imgError } = useAdminProofImage(order.id, order.paymentProof?.uploadedAt || '');

  const submit = async (e) => {
    e?.preventDefault();
    if (savingRef.current) return; // cegah klik ganda
    savingRef.current = true;
    setSaving(true);
    setError(null);
    try {
      const res = await adminFetch(`/orders/${order.id}/status`, { method: 'PATCH', body: { from: order.status, to: 'paid', paymentNote: note.trim() } });
      savingRef.current = false;
      onSaved(res);
    } catch (err) {
      savingRef.current = false;
      setError(err);
      setSaving(false);
    }
  };
  const close = () => {
    if (!savingRef.current) onClose();
  };

  return (
    <BottomSheet open onClose={close} labelledBy="paid-confirm-title" testId="paid-confirm">
      <form onSubmit={submit} noValidate>
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 id="paid-confirm-title" className="text-lg font-bold text-gray-900">
              Tandai sebagai dibayar?
            </h2>
            <p className="text-sm text-gray-500">Pesanan {order.orderNo}</p>
          </div>
          <CloseX onClick={close} />
        </div>
        {hasProof ? (
          <div className="mt-3 flex items-center gap-3 rounded-xl bg-blue-50 p-3" data-testid="paid-confirm-proof">
            {url ? (
              <img src={url} alt="Bukti pembayaran" className="h-24 w-24 shrink-0 rounded-lg border border-gray-200 bg-white object-contain" data-testid="paid-confirm-img" />
            ) : (
              <span className="flex h-24 w-24 shrink-0 items-center justify-center rounded-lg border border-dashed border-gray-300 text-center text-xs text-gray-400">
                {imgError ? 'Gagal memuat bukti' : 'Memuat...'}
              </span>
            )}
            <p className="text-sm font-semibold text-blue-900" data-testid="paid-confirm-text">
              Bukti transfer sudah benar?
            </p>
          </div>
        ) : (
          <p className="mt-3 rounded-xl bg-amber-50 p-3 text-sm font-semibold text-amber-900" data-testid="paid-confirm-text">
            Pelanggan belum melampirkan bukti pembayaran, yakin?
          </p>
        )}
        <p className="mt-3 text-sm text-gray-700">
          Status: <strong>{statusLabel(order.status)}</strong> → <strong>{statusLabel('paid')}</strong> (diskon & ongkir akan terkunci)
        </p>
        <label className="mt-3 block">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Catatan pembayaran (opsional)</span>
          <input className={`${inputClass} text-base`} maxLength={255} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
        {error && (
          <div className="mt-3">
            <ErrorBox error={error} />
          </div>
        )}
        <div className="mt-4 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button type="button" className={btnSecondary} onClick={close} disabled={saving}>
            {hasProof ? 'Belum' : 'Batal'}
          </button>
          <button type="submit" className={btnPrimary} disabled={saving}>
            {saving ? 'Menyimpan...' : hasProof ? 'Ya, sudah benar' : 'Ya, tandai dibayar'}
          </button>
        </div>
      </form>
    </BottomSheet>
  );
};

const StatusAction = ({ order, to, onClose, onSaved }) => {
  if (to === 'paid') return <PaidConfirmSheet order={order} onClose={onClose} onSaved={onSaved} />;
  return <StatusActionModal order={order} to={to} onClose={onClose} onSaved={onSaved} />;
};

const StatusActionModal = ({ order, to, onClose, onSaved }) => {
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
    const sp = new URLSearchParams(raw);
    if (!sp.has('status')) return ''; // tab bawaan (Menunggu konfirmasi)
    const st = tabFromSearch(sp);
    return `?status=${st || ALL_TAB_PARAM}`;
  })();
  const [order, setOrder] = useState(null);
  const { refresh: refreshSummary } = useAdminSummary();
  // Setiap perubahan pesanan yang berhasil (konfirmasi, harga, status, catatan) -> ringkasan disegarkan.
  const [defaultOffer, setDefaultOffer] = useState(null); // popup sekali per konfirmasi (tidak disimpan)
  const [notice, setNotice] = useState('');
  useEffect(() => {
    if (!notice) return undefined;
    const t = setTimeout(() => setNotice(''), 6000);
    return () => clearTimeout(t);
  }, [notice]);
  const savedOrder = (o, meta) => {
    setOrder(o);
    refreshSummary();
    if (meta?.defaultOffer) setDefaultOffer(meta.defaultOffer);
  };
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
  // Pelanggan sudah mengirim bukti transfer: tombol "Kirim ringkasan ke WhatsApp pelanggan" tidak perlu lagi.
  const wa = order.paymentProof ? '' : waLink(r.phone, buildAdminSummaryText(order, settings));
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
          {order.paymentProof ? null : wa ? (
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
            {order.canConfirm && (
              <p className="mb-3 rounded-md bg-orange-50 px-3 py-2 text-sm text-orange-900" data-testid="confirm-hint">
                Isi ongkir lalu konfirmasi agar pelanggan bisa membayar.
              </p>
            )}
            {order.pricingLocked ? (
              <p className="text-sm text-gray-600">Terkunci: diskon dan ongkir hanya bisa diubah saat pesanan menunggu pembayaran.</p>
            ) : (
              <PricingPanel key={order.updatedAt} order={order} onSaved={savedOrder} />
            )}
          </Card>

          <Card title="Bukti pembayaran">
            <AdminProofCard order={order} />
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
            <AdminNote key={order.id} order={order} onSaved={savedOrder} />
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

      {defaultOffer && (
        <DefaultOfferSheet
          offer={defaultOffer}
          onClose={() => setDefaultOffer(null)}
          onDone={(r) => {
            const name = r.districtName || defaultOffer.districtName;
            setDefaultOffer(null);
            setNotice(`Default ongkir ${name ? `Kec. ${name}` : 'kecamatan'} disimpan: ${rupiah(r.shippingFee ?? defaultOffer.fee)}`);
          }}
        />
      )}
      {notice && (
        <div
          role="status"
          data-testid="default-offer-notice"
          className="fixed inset-x-4 bottom-4 z-40 mx-auto flex max-w-md items-start justify-between gap-3 rounded-lg bg-gray-900 px-4 py-3 text-sm text-white shadow-lg"
        >
          <span>{notice}</span>
          <button type="button" className="text-xs underline" onClick={() => setNotice('')}>
            Tutup
          </button>
        </div>
      )}
      {action && (
        <StatusAction
          order={order}
          to={action}
          onClose={() => setAction(null)}
          onSaved={(o) => {
            savedOrder(o);
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
