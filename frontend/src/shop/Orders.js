import React, { useCallback, useEffect, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { errorMessage } from '../auth';
import { cancelOrder, getOrder, getStoreInfo, isUnauthorized, listOrders } from './api';
import { useCart } from './CartContext';
import { Notice } from './Cart';
import { STATUS, buildCustomerConfirmText, fmtDateTime, perUnit, rupiah, statusLabel, tierNote, waLink } from './format';

export const StatusBadge = ({ status }) => (
  <span className={`inline-block text-xs font-semibold border rounded-full px-2.5 py-0.5 ${STATUS[status]?.cls || 'bg-gray-100'}`}>
    {statusLabel(status)}
  </span>
);

export const MyOrders = () => {
  const { onUnauthorized } = useCart();
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    setError('');
    listOrders(page)
      .then(setData)
      .catch((err) => (isUnauthorized(err) ? onUnauthorized?.() : setError(errorMessage(err, 'Gagal memuat pesanan'))));
  }, [page, onUnauthorized]);

  const pages = data ? Math.max(1, Math.ceil((data.total || 0) / (data.perPage || 10))) : 1;
  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <h1 className="text-2xl font-bold text-gray-800 mb-4">Pesanan Saya</h1>
      {error && <Notice kind="error">{error}</Notice>}
      {!data && !error && <p className="text-gray-600">Memuat...</p>}
      {data && data.items?.length === 0 && (
        <div className="bg-white rounded-xl shadow p-8 text-center text-gray-600">
          Belum ada pesanan. <Link to="/" className="text-purple-700 underline">Mulai belanja</Link>
        </div>
      )}
      {data?.items?.length > 0 && (
        <ul className="space-y-3">
          {data.items.map((o) => (
            <li key={o.orderNo}>
              <Link
                to={`/pesanan/${encodeURIComponent(o.orderNo)}`}
                className="block bg-white rounded-xl shadow p-4 hover:shadow-md transition"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="font-semibold text-gray-800">{o.orderNo}</span>
                  <StatusBadge status={o.status} />
                </div>
                <div className="text-sm text-gray-600 mt-1">
                  {o.firstItem}
                  {o.itemCount > 1 ? ` dan lainnya` : ''} · {o.itemCount} item
                </div>
                <div className="flex justify-between text-sm mt-1">
                  <span className="text-gray-500">{fmtDateTime(o.createdAt)}</span>
                  <span className="font-bold text-purple-700">{rupiah(o.total)}</span>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {data && pages > 1 && (
        <div className="flex justify-between items-center mt-4 text-sm">
          <button className="px-3 py-2 border rounded-lg disabled:opacity-50" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            ‹ Sebelumnya
          </button>
          <span>
            Halaman {page} dari {pages}
          </span>
          <button className="px-3 py-2 border rounded-lg disabled:opacity-50" disabled={page >= pages} onClick={() => setPage(page + 1)}>
            Berikutnya ›
          </button>
        </div>
      )}
    </div>
  );
};

const PaymentInfo = ({ info }) => {
  if (!info) return null;
  if (!info.paymentConfigured) {
    return (
      <Notice kind="warn">
        Info rekening toko belum diatur. Silakan tunggu konfirmasi dari Mihan Store
        {info.storeWhatsapp ? ' atau hubungi toko lewat WhatsApp' : ''} sebelum melakukan transfer.
      </Notice>
    );
  }
  return (
    <div className="rounded-lg border border-purple-200 bg-purple-50 p-4 text-sm text-gray-800">
      <div className="font-semibold text-purple-800 mb-1">Transfer ke rekening berikut</div>
      <div>
        Bank: <strong>{info.bankName}</strong>
      </div>
      <div>
        No. rekening: <strong className="tracking-wide">{info.bankAccountNumber}</strong>
      </div>
      <div>
        Atas nama: <strong>{info.bankAccountHolder}</strong>
      </div>
      {info.paymentNote && <p className="mt-2 whitespace-pre-line text-gray-700">{info.paymentNote}</p>}
    </div>
  );
};

export const OrderDetail = ({ user }) => {
  const { orderNo } = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const { onUnauthorized } = useCart();
  const isNew = new URLSearchParams(location.search).get('baru') === '1';
  const [order, setOrder] = useState(null);
  const [info, setInfo] = useState(null);
  const [error, setError] = useState('');
  const [notFound, setNotFound] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const handleErr = useCallback(
    (err, fallback) => {
      if (isUnauthorized(err)) return onUnauthorized?.();
      if (err.response?.status === 404) return setNotFound(true);
      return setError(errorMessage(err, fallback));
    },
    [onUnauthorized]
  );

  useEffect(() => {
    getOrder(orderNo)
      .then(setOrder)
      .catch((err) => handleErr(err, 'Gagal memuat pesanan'));
    getStoreInfo()
      .then(setInfo)
      .catch(() => setInfo(null));
  }, [orderNo, handleErr]);

  const doCancel = async () => {
    setBusy(true);
    setError('');
    try {
      setOrder(await cancelOrder(orderNo, reason.trim()));
      setConfirming(false);
    } catch (err) {
      handleErr(err, 'Gagal membatalkan pesanan');
    } finally {
      setBusy(false);
    }
  };

  if (notFound) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-10">
        <Notice kind="error">Pesanan tidak ditemukan.</Notice>
        <button className="mt-4 text-purple-700 underline" onClick={() => navigate('/pesanan')}>
          Lihat semua pesanan
        </button>
      </div>
    );
  }
  if (!order) {
    return <div className="max-w-3xl mx-auto px-4 py-10 text-gray-600">{error ? <Notice kind="error">{error}</Notice> : 'Memuat...'}</div>;
  }

  const wa = info?.storeWhatsapp ? waLink(info.storeWhatsapp, buildCustomerConfirmText(order, user?.name)) : '';

  return (
    <div className="max-w-3xl mx-auto px-4 py-8 space-y-4">
      {isNew && (
        <Notice kind="success">
          <strong>Pesanan berhasil dibuat!</strong> Nomor pesanan Anda <strong>{order.orderNo}</strong>. Silakan transfer
          sesuai total di bawah, lalu konfirmasi ke toko. Admin akan menambahkan ongkir (bila ada) dan memperbarui status.
        </Notice>
      )}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold text-gray-800">{order.orderNo}</h1>
        <StatusBadge status={order.status} />
      </div>
      {error && <Notice kind="error">{error}</Notice>}

      {order.status === 'pending_payment' && <PaymentInfo info={info} />}

      <div className="bg-white rounded-xl shadow p-4">
        <h2 className="font-semibold text-gray-800 mb-2">Item</h2>
        <ul className="divide-y text-sm">
          {order.items.map((it, i) => (
            <li key={i} className="py-2 flex justify-between gap-3">
              <span className="break-words">
                {it.name}
                <span className="text-gray-500">
                  {' '}
                  · {it.qty}
                  {it.unit ? ` ${it.unit}` : ''} x {perUnit(it.unitPrice, it.unit)}
                </span>
                {it.tierMinQty ? (
                  <span className="ml-1 inline-block rounded bg-green-100 px-1.5 text-xs font-semibold text-green-800">{tierNote(it.tierMinQty)}</span>
                ) : null}
              </span>
              <span className="font-medium whitespace-nowrap">{rupiah(it.lineTotal)}</span>
            </li>
          ))}
        </ul>
        <div className="border-t mt-2 pt-2 text-sm space-y-1">
          <div className="flex justify-between">
            <span>Subtotal</span>
            <span>{rupiah(order.subtotal)}</span>
          </div>
          {order.discount > 0 && (
            <div className="flex justify-between text-green-700">
              <span>Diskon{order.discountNote ? ` (${order.discountNote})` : ''}</span>
              <span>-{rupiah(order.discount)}</span>
            </div>
          )}
          <div className="flex justify-between">
            <span>Ongkir</span>
            <span>{order.shippingFee > 0 ? rupiah(order.shippingFee) : order.status === 'pending_payment' ? 'menunggu admin' : 'Rp 0'}</span>
          </div>
          <div className="flex justify-between text-base font-bold text-purple-700 pt-1">
            <span>Total</span>
            <span>{rupiah(order.total)}</span>
          </div>
        </div>
      </div>

      <div className="bg-white rounded-xl shadow p-4 text-sm">
        <h2 className="font-semibold text-gray-800 mb-2">Dikirim ke</h2>
        <div className="font-medium">{order.recipient.name}</div>
        <div>{order.recipient.phone}</div>
        <div className="whitespace-pre-line">{order.recipient.address}</div>
        <div>
          {order.recipient.city}
          {order.recipient.postalCode ? ` ${order.recipient.postalCode}` : ''}
        </div>
        {order.note && <div className="mt-2 text-gray-600">Catatan: {order.note}</div>}
      </div>

      <div className="bg-white rounded-xl shadow p-4 text-sm">
        <h2 className="font-semibold text-gray-800 mb-2">Riwayat status</h2>
        <ol className="border-l-2 border-purple-200 ml-2 space-y-3">
          {order.history.map((h, i) => (
            <li key={i} className="pl-4 relative">
              <span className="absolute -left-[7px] top-1 w-3 h-3 rounded-full bg-purple-500" />
              <div className="font-medium">{h.toLabel}</div>
              <div className="text-gray-500">
                {fmtDateTime(h.createdAt)} · oleh {h.actor}
              </div>
              {h.note && <div className="text-gray-700">Alasan: {h.note}</div>}
            </li>
          ))}
        </ol>
      </div>

      <div className="flex flex-col sm:flex-row gap-3">
        {wa && (
          <a
            href={wa}
            target="_blank"
            rel="noopener noreferrer"
            className="flex-1 text-center bg-green-500 text-white py-3 rounded-lg font-semibold hover:bg-green-600"
          >
            Konfirmasi via WhatsApp
          </a>
        )}
        {order.canCancel && !confirming && (
          <button
            type="button"
            className="flex-1 border border-red-300 text-red-700 py-3 rounded-lg font-semibold hover:bg-red-50"
            onClick={() => setConfirming(true)}
          >
            Batalkan pesanan
          </button>
        )}
      </div>
      {confirming && (
        <div className="bg-white rounded-xl shadow p-4 space-y-3">
          <p className="text-sm text-gray-700">Yakin membatalkan pesanan {order.orderNo}? Tindakan ini tidak bisa dibatalkan.</p>
          <input
            className="w-full px-3 py-2 border-2 border-gray-300 rounded-lg text-sm"
            placeholder="Alasan (opsional)"
            maxLength={255}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
          <div className="flex gap-2 justify-end">
            <button className="px-4 py-2 border rounded-lg text-sm" onClick={() => setConfirming(false)} disabled={busy}>
              Tidak
            </button>
            <button className="px-4 py-2 bg-red-600 text-white rounded-lg text-sm font-semibold disabled:opacity-60" onClick={doCancel} disabled={busy}>
              {busy ? 'Membatalkan...' : 'Ya, batalkan'}
            </button>
          </div>
        </div>
      )}
      <Link to="/pesanan" className="inline-block text-purple-700 underline text-sm">
        ‹ Semua pesanan
      </Link>
    </div>
  );
};
