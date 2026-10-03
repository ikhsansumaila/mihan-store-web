import React, { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { errorMessage } from '../auth';
import { ackCartPrices, removeCartItem, setCartQty, isUnauthorized } from './api';
import { useCart } from './CartContext';
import { perUnit, rupiah, tierNote } from './format';

export const Notice = ({ kind = 'info', children }) => {
  const cls = {
    info: 'border-blue-200 bg-blue-50 text-blue-800',
    warn: 'border-yellow-300 bg-yellow-50 text-yellow-900',
    error: 'border-red-200 bg-red-50 text-red-700',
    success: 'border-green-200 bg-green-50 text-green-800',
  }[kind];
  return <div className={`rounded-lg border px-4 py-3 text-sm ${cls}`}>{children}</div>;
};

const QtyControl = ({ item, busy, onSet }) => {
  const [val, setVal] = useState(String(item.qty));
  useEffect(() => setVal(String(item.qty)), [item.qty]);
  const commit = () => {
    const n = parseInt(val.replace(/[^0-9]/g, ''), 10);
    if (!Number.isFinite(n) || n === item.qty) return setVal(String(item.qty));
    onSet(Math.min(Math.max(n, 0), 999));
  };
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        aria-label={`Kurangi ${item.name}`}
        className="w-8 h-8 rounded-lg border border-gray-300 text-lg leading-none disabled:opacity-50"
        disabled={busy || item.qty <= 1}
        onClick={() => onSet(item.qty - 1)}
      >
        −
      </button>
      <input
        aria-label={`Jumlah ${item.name}`}
        inputMode="numeric"
        className="w-14 h-8 text-center border border-gray-300 rounded-lg"
        value={val}
        disabled={busy}
        onChange={(e) => setVal(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === 'Enter' && commit()}
      />
      <button
        type="button"
        aria-label={`Tambah ${item.name}`}
        className="w-8 h-8 rounded-lg border border-gray-300 text-lg leading-none disabled:opacity-50"
        disabled={busy || item.qty >= 999}
        onClick={() => onSet(item.qty + 1)}
      >
        +
      </button>
    </div>
  );
};

// Penanda harga per baris keranjang: harga efektif per satuan, info grosir (hemat), petunjuk jenjang berikutnya,
// dan "Harga berubah" bila harga sekarang berbeda dari yang terakhir dilihat pelanggan.
export const CartItemPricing = ({ item, onAck, acking }) => {
  const unit = item.unit || 'pcs';
  const unitPrice = item.unitPrice ?? item.price;
  return (
    <div className="text-sm text-gray-600 space-y-1">
      <div>
        <span className="font-medium text-gray-800">{perUnit(unitPrice, unit)}</span>
        {item.tierMinQty && item.baseUnitPrice > unitPrice ? (
          <>
            {' '}
            <span className="text-gray-400 line-through">{perUnit(item.baseUnitPrice, unit)}</span>
          </>
        ) : null}
      </div>
      {item.tierMinQty ? (
        <div className="flex flex-wrap gap-1.5">
          <span className="inline-block rounded bg-green-100 px-2 py-0.5 text-xs font-semibold text-green-800">{tierNote(item.tierMinQty)}</span>
          {item.savings > 0 && <span className="inline-block rounded bg-green-50 px-2 py-0.5 text-xs font-semibold text-green-700">hemat {rupiah(item.savings)}</span>}
        </div>
      ) : null}
      {item.available && item.nextTier && (
        <div className="text-xs text-purple-700" data-testid="next-tier">
          Tambah {item.nextTier.moreQty} lagi untuk harga {perUnit(item.nextTier.unitPrice, unit)}
        </div>
      )}
      {item.priceChanged && (
        <div role="status" className="rounded-md border border-amber-300 bg-amber-50 px-2 py-1.5 text-xs text-amber-900" data-testid="price-changed">
          <strong>Harga berubah:</strong> dari {rupiah(item.previousUnitPrice)} menjadi {perUnit(unitPrice, unit)}{' '}
          {onAck && (
            <button type="button" className="ml-1 font-semibold underline disabled:opacity-50" onClick={onAck} disabled={acking}>
              Mengerti
            </button>
          )}
        </div>
      )}
    </div>
  );
};

// Banner di atas keranjang/checkout bila ada baris yang harganya berubah.
export const PriceChangeBanner = ({ cart, onAck, acking }) => {
  const n = (cart?.items || []).filter((i) => i.priceChanged).length;
  if (!n) return null;
  return (
    <div className="mb-4" data-testid="price-change-banner">
      <Notice kind="warn">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <span>
            <strong>Harga berubah</strong> untuk {n} produk sejak terakhir Anda lihat. Periksa harga terbaru di bawah.
          </span>
          <button
            type="button"
            className="shrink-0 rounded-lg bg-amber-600 px-4 py-2 font-semibold text-white hover:bg-amber-700 disabled:opacity-60"
            onClick={onAck}
            disabled={acking}
          >
            Mengerti
          </button>
        </div>
      </Notice>
    </div>
  );
};

const Cart = () => {
  const navigate = useNavigate();
  const { cart, refresh, setCart, onUnauthorized } = useCart();
  const [loading, setLoading] = useState(!cart);
  const [busyId, setBusyId] = useState(null);
  const [error, setError] = useState('');
  const [acking, setAcking] = useState(false);

  useEffect(() => {
    refresh().finally(() => setLoading(false));
  }, [refresh]);

  const run = async (productId, fn) => {
    setBusyId(productId);
    setError('');
    try {
      const c = await fn();
      if (c && Array.isArray(c.items)) setCart(c);
    } catch (err) {
      if (isUnauthorized(err)) return onUnauthorized?.();
      setError(errorMessage(err, 'Gagal memperbarui keranjang'));
      refresh();
    } finally {
      setBusyId(null);
    }
  };

  const ack = async () => {
    setAcking(true);
    setError('');
    try {
      const c = await ackCartPrices();
      if (c && Array.isArray(c.items)) setCart(c);
    } catch (err) {
      if (isUnauthorized(err)) return onUnauthorized?.();
      setError(errorMessage(err, 'Gagal memperbarui keranjang'));
    } finally {
      setAcking(false);
    }
    return undefined;
  };

  if (loading && !cart) return <p className="max-w-3xl mx-auto px-4 py-10 text-gray-600">Memuat keranjang...</p>;
  const items = cart?.items || [];

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <h1 className="text-2xl font-bold text-gray-800 mb-4">Keranjang</h1>
      {error && (
        <div className="mb-4">
          <Notice kind="error">{error}</Notice>
        </div>
      )}
      {items.length === 0 ? (
        <div className="bg-white rounded-xl shadow p-8 text-center text-gray-600">
          <p className="mb-4">Keranjang Anda masih kosong.</p>
          <Link to="/" className="inline-block bg-purple-700 text-white px-4 py-2 rounded-lg font-semibold hover:bg-purple-800">
            Belanja sekarang
          </Link>
        </div>
      ) : (
        <>
          <PriceChangeBanner cart={cart} onAck={ack} acking={acking} />
          {cart.hasUnavailable && (
            <div className="mb-4">
              <Notice kind="warn">
                Ada produk yang sudah tidak tersedia. Hapus produk bertanda “Tidak tersedia” sebelum checkout.
              </Notice>
            </div>
          )}
          <ul className="bg-white rounded-xl shadow divide-y">
            {items.map((it) => (
              <li key={it.productId} className={`p-4 flex flex-col sm:flex-row sm:items-center gap-3 ${it.available ? '' : 'bg-gray-50'}`}>
                <div className="flex-1 min-w-0">
                  <div className="font-semibold text-gray-800 break-words">{it.name}</div>
                  <CartItemPricing item={it} onAck={ack} acking={acking} />
                  {!it.available && (
                    <span className="inline-block mt-1 text-xs font-semibold text-red-700 bg-red-100 border border-red-200 rounded px-2 py-0.5">
                      Tidak tersedia
                    </span>
                  )}
                </div>
                <div className="flex items-center justify-between sm:justify-end gap-4">
                  {it.available ? (
                    <QtyControl item={it} busy={busyId === it.productId} onSet={(q) => run(it.productId, () => setCartQty(it.productId, q))} />
                  ) : (
                    <span className="text-sm text-gray-500">x{it.qty}</span>
                  )}
                  <div className={`w-28 text-right font-semibold ${it.available ? 'text-gray-800' : 'text-gray-400 line-through'}`}>
                    {rupiah(it.lineTotal)}
                  </div>
                  <button
                    type="button"
                    className="text-sm text-red-600 hover:text-red-800 font-medium disabled:opacity-50"
                    disabled={busyId === it.productId}
                    onClick={() => run(it.productId, () => removeCartItem(it.productId))}
                  >
                    Hapus
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <div className="bg-white rounded-xl shadow p-4 mt-4">
            <div className="flex justify-between text-lg">
              <span className="text-gray-700">Subtotal ({cart.itemCount} item)</span>
              <span className="font-bold text-purple-700">{rupiah(cart.subtotal)}</span>
            </div>
            {cart.savings > 0 && (
              <div className="flex justify-between text-sm text-green-700">
                <span>Hemat harga grosir</span>
                <span>{rupiah(cart.savings)}</span>
              </div>
            )}
            <p className="text-xs text-gray-500 mt-1">
              Harga mengikuti harga terkini (harga grosir otomatis sesuai jumlah per produk). Ongkir dan diskon (bila ada)
              ditetapkan admin setelah pesanan dibuat.
            </p>
            <button
              type="button"
              className="mt-4 w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-50"
              disabled={cart.hasUnavailable || cart.itemCount === 0}
              onClick={() => navigate('/checkout')}
            >
              Lanjut ke checkout
            </button>
          </div>
        </>
      )}
    </div>
  );
};

export default Cart;
