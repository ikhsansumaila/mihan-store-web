import React, { useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { errorMessage } from '../auth';
import { ackCartPrices, createOrder, isUnauthorized } from './api';
import { useCart } from './CartContext';
import { Notice, PriceChangeBanner } from './Cart';
import { newIdempotencyKey, perUnit, rupiah, tierNote } from './format';

const input =
  'w-full px-3 py-2 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition';

const Field = ({ label, hint, children }) => (
  <label className="block">
    <span className="block text-sm font-semibold text-gray-700 mb-1">
      {label} {hint && <span className="font-normal text-gray-500">{hint}</span>}
    </span>
    {children}
  </label>
);

const Checkout = ({ user }) => {
  const navigate = useNavigate();
  const { cart, refresh, setCart, onUnauthorized } = useCart();
  const [loading, setLoading] = useState(!cart);
  const [form, setForm] = useState({
    recipientName: user?.name || '',
    recipientPhone: user?.phone || '',
    address: '',
    city: '',
    postalCode: '',
    note: '',
  });
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  // Server menjawab 409 price_changed: keranjang terkini ditampilkan, pelanggan harus konfirmasi ulang.
  const [priceChanged, setPriceChanged] = useState(false);
  const [acking, setAcking] = useState(false);
  // Satu kunci per percobaan checkout: klik ganda / kirim ulang -> pesanan yang sama.
  const idemKey = useRef(newIdempotencyKey());
  const inFlight = useRef(false); // cegah klik ganda sebelum state sempat diperbarui

  useEffect(() => {
    refresh().finally(() => setLoading(false));
  }, [refresh]);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  const submit = async (e) => {
    e.preventDefault();
    if (inFlight.current) return;
    setError('');
    if (!form.recipientName.trim() || !form.recipientPhone.trim() || form.address.trim().length < 5 || form.city.trim().length < 2) {
      setError('Lengkapi nama penerima, nomor telepon, alamat (min. 5 karakter), dan kota.');
      return;
    }
    if (form.postalCode.trim() && !/^[0-9]{5}$/.test(form.postalCode.trim())) {
      setError('Kode pos harus 5 digit angka.');
      return;
    }
    inFlight.current = true;
    setSubmitting(true);
    try {
      // Konfirmasi ulang setelah "Harga berubah": harga terbaru dianggap sudah dilihat, total terbaru dikirim.
      let shown = cart;
      if (priceChanged) {
        const c = await ackCartPrices();
        if (c && Array.isArray(c.items)) {
          setCart(c);
          shown = c;
        }
      }
      // Hanya data penerima + kunci idempotensi + total yang DITAMPILKAN; item & harga dihitung server dari
      // keranjang. Total berbeda -> server menolak (409 price_changed) tanpa membuat pesanan.
      const res = await createOrder({
        recipientName: form.recipientName.trim(),
        recipientPhone: form.recipientPhone.trim(),
        address: form.address.trim(),
        city: form.city.trim(),
        postalCode: form.postalCode.trim(),
        note: form.note.trim(),
        idempotencyKey: idemKey.current,
        expectedTotal: shown.subtotal,
      });
      refresh();
      navigate(`/pesanan/${encodeURIComponent(res.data.orderNo)}?baru=1`, { replace: true });
    } catch (err) {
      if (isUnauthorized(err)) {
        onUnauthorized?.();
        return;
      }
      inFlight.current = false;
      setSubmitting(false);
      const data = err.response?.data;
      if (err.response?.status === 409 && data?.error === 'price_changed') {
        if (data.cart && Array.isArray(data.cart.items)) setCart(data.cart);
        else refresh();
        setPriceChanged(true);
        setError('');
        return;
      }
      setError(errorMessage(err, 'Gagal membuat pesanan, coba lagi.'));
      if (err.response?.status === 409 || err.response?.status === 400) refresh();
    }
  };

  const ack = async () => {
    setAcking(true);
    try {
      const c = await ackCartPrices();
      if (c && Array.isArray(c.items)) setCart(c);
    } catch (err) {
      if (isUnauthorized(err)) onUnauthorized?.();
    } finally {
      setAcking(false);
    }
  };

  if (loading && !cart) return <p className="max-w-3xl mx-auto px-4 py-10 text-gray-600">Memuat...</p>;
  const items = (cart?.items || []).filter((i) => i.available);

  if (!cart || cart.itemCount === 0) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-10">
        <h1 className="text-2xl font-bold text-gray-800 mb-4">Checkout</h1>
        <Notice>
          Keranjang Anda kosong. <Link to="/" className="underline font-medium">Kembali belanja</Link>
        </Notice>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      <h1 className="text-2xl font-bold text-gray-800 mb-4">Checkout</h1>
      {priceChanged ? (
        <div className="mb-4" data-testid="checkout-price-changed">
          <Notice kind="warn">
            <strong>Harga berubah.</strong> Total terbaru <strong>{rupiah(cart.subtotal)}</strong>. Periksa ringkasan pesanan, lalu
            tekan <strong>Konfirmasi &amp; buat pesanan</strong> untuk melanjutkan dengan harga terbaru.
          </Notice>
        </div>
      ) : (
        <PriceChangeBanner cart={cart} onAck={ack} acking={acking} />
      )}
      {cart.hasUnavailable && (
        <div className="mb-4">
          <Notice kind="warn">
            Ada produk yang tidak tersedia di keranjang. <Link to="/keranjang" className="underline">Perbarui keranjang</Link> dulu.
          </Notice>
        </div>
      )}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-6">
        <form onSubmit={submit} className="lg:col-span-3 bg-white rounded-xl shadow p-4 sm:p-6 space-y-4" noValidate>
          <h2 className="text-lg font-semibold text-gray-800">Data penerima</h2>
          {error && <Notice kind="error">{error}</Notice>}
          <Field label="Nama penerima">
            <input className={input} maxLength={100} value={form.recipientName} onChange={set('recipientName')} autoComplete="name" required />
          </Field>
          <Field label="Nomor telepon/WhatsApp" hint="(08xx / +628xx)">
            <input className={input} inputMode="tel" maxLength={20} value={form.recipientPhone} onChange={set('recipientPhone')} autoComplete="tel" required />
          </Field>
          <Field label="Alamat lengkap">
            <textarea className={input} rows={3} maxLength={500} value={form.address} onChange={set('address')} autoComplete="street-address" required />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="sm:col-span-2">
              <Field label="Kota/Kabupaten">
                <input className={input} maxLength={100} value={form.city} onChange={set('city')} autoComplete="address-level2" required />
              </Field>
            </div>
            <Field label="Kode pos" hint="(opsional)">
              <input className={input} inputMode="numeric" maxLength={5} value={form.postalCode} onChange={set('postalCode')} autoComplete="postal-code" />
            </Field>
          </div>
          <Field label="Catatan" hint="(opsional)">
            <textarea className={input} rows={2} maxLength={500} value={form.note} onChange={set('note')} />
          </Field>
          <p className="text-xs text-gray-500">
            Pembayaran dengan transfer bank manual. Ongkir ditetapkan admin setelah pesanan dibuat; total akhir terlihat di
            halaman pesanan. Data penerima dipakai untuk pengiriman sesuai{' '}
            <Link to="/privasi" className="underline">Kebijakan Privasi</Link>.
          </p>
          <button
            type="submit"
            className="w-full bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800 disabled:opacity-60"
            disabled={submitting || cart.hasUnavailable}
          >
            {submitting ? 'Membuat pesanan...' : priceChanged ? 'Konfirmasi & buat pesanan' : 'Buat pesanan'}
          </button>
        </form>
        <aside className="lg:col-span-2 bg-white rounded-xl shadow p-4 sm:p-6 h-fit">
          <h2 className="text-lg font-semibold text-gray-800 mb-3">Ringkasan</h2>
          <ul className="divide-y text-sm">
            {items.map((it) => (
              <li key={it.productId} className="py-2 flex justify-between gap-3">
                <span className="text-gray-700 break-words">
                  {it.name} <span className="text-gray-500">x{it.qty}</span>
                  <span className="block text-xs text-gray-500">
                    {perUnit(it.unitPrice ?? it.price, it.unit || 'pcs')}
                    {it.tierMinQty ? <span className="ml-1 font-semibold text-green-700">· {tierNote(it.tierMinQty)}</span> : null}
                  </span>
                  {it.priceChanged && (
                    <span className="block text-xs font-semibold text-amber-800">
                      Harga berubah dari {rupiah(it.previousUnitPrice)}
                    </span>
                  )}
                </span>
                <span className="font-medium whitespace-nowrap">{rupiah(it.lineTotal)}</span>
              </li>
            ))}
          </ul>
          <div className="border-t mt-2 pt-3 space-y-1 text-sm">
            <div className="flex justify-between">
              <span>Subtotal</span>
              <span>{rupiah(cart.subtotal)}</span>
            </div>
            <div className="flex justify-between text-gray-500">
              <span>Ongkir</span>
              <span>ditetapkan admin</span>
            </div>
            <div className="flex justify-between text-base font-bold text-purple-700 pt-1">
              <span>Total sementara</span>
              <span>{rupiah(cart.subtotal)}</span>
            </div>
          </div>
        </aside>
      </div>
    </div>
  );
};

export default Checkout;
