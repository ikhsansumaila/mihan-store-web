import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { errorMessage } from '../auth';
import { ackCartPrices, createOrder, isUnauthorized } from './api';
import { useCart } from './CartContext';
import { Notice, PriceChangeBanner } from './Cart';
import { newIdempotencyKey, perUnit, rupiah, tierNote } from './format';
import RegionCombobox from './RegionCombobox';
import { REGION_LEVELS, loadRegions, regionErrorMessage } from './regionsApi';

const inputBase = 'w-full min-h-[44px] px-3 py-2 border-2 rounded-lg text-base focus:outline-none transition';
const inputCls = (bad) => `${inputBase} ${bad ? 'border-red-500 focus:border-red-600' : 'border-gray-300 focus:border-purple-600'}`;

const Field = ({ id, label, hint, required, error, children }) => (
  <div>
    <label htmlFor={id} className="block text-sm font-semibold text-gray-700 mb-1">
      {label} {required && <span className="text-red-600" aria-hidden="true">*</span>}{' '}
      {hint && <span className="font-normal text-gray-500">{hint}</span>}
    </label>
    {children}
    {error && (
      <p id={`${id}-err`} className="mt-1 text-sm text-red-700">
        {error}
      </p>
    )}
  </div>
);

// Telepon: sama dengan aturan server (08xx / 628xx / +628xx, 8–15 digit).
export const validPhone = (s) => {
  let p = String(s || '').trim().replace(/[\s.\-()]/g, '');
  if (p.startsWith('+62')) p = p.slice(1);
  else if (p.startsWith('0')) p = `62${p.slice(1)}`;
  return /^628[0-9]{5,12}$/.test(p);
};

// Validasi klien (server tetap memvalidasi ulang). Semua wajib kecuali catatan.
export const validateCheckoutForm = (form, region) => {
  const e = {};
  const name = form.recipientName.trim();
  if (!name) e.recipientName = 'Nama penerima wajib diisi.';
  else if (name.length > 100) e.recipientName = 'Nama penerima maksimal 100 karakter.';
  if (!form.recipientPhone.trim()) e.recipientPhone = 'Nomor telepon wajib diisi.';
  else if (!validPhone(form.recipientPhone)) e.recipientPhone = 'Nomor telepon tidak valid. Gunakan format 08xx, 628xx, atau +628xx.';
  REGION_LEVELS.forEach((lv) => {
    if (!region[lv.key]) e[lv.field] = `Pilih ${lv.lower}.`;
  });
  if (form.address.trim().length < 5) e.address = 'Alamat lengkap wajib diisi (min. 5 karakter): jalan, RT/RW, nomor rumah.';
  const pc = form.postalCode.trim();
  if (!pc) e.postalCode = 'Kode pos wajib diisi.';
  else if (!/^[0-9]{5}$/.test(pc)) e.postalCode = 'Kode pos harus 5 digit angka.';
  if (form.note.trim().length > 500) e.note = 'Catatan maksimal 500 karakter.';
  return e;
};

const FIELD_ORDER = ['recipientName', 'recipientPhone', 'provinceCode', 'regencyCode', 'districtCode', 'villageCode', 'address', 'postalCode', 'note'];

// Daftar wilayah satu tingkat, dimuat per induk (null = belum bisa dimuat).
const useRegionList = (level, parent) => {
  const [state, setState] = useState({ items: [], loading: false, error: '' });
  const [nonce, setNonce] = useState(0);
  const enabled = level === 0 || !!parent;
  useEffect(() => {
    if (!enabled) {
      setState({ items: [], loading: false, error: '' });
      return undefined;
    }
    let alive = true;
    setState({ items: [], loading: true, error: '' });
    loadRegions(level, parent)
      .then((items) => alive && setState({ items, loading: false, error: '' }))
      .catch((err) => alive && setState({ items: [], loading: false, error: regionErrorMessage(err, REGION_LEVELS[level].lower) }));
    return () => {
      alive = false;
    };
  }, [level, parent, enabled, nonce]);
  const retry = useCallback(() => setNonce((n) => n + 1), []);
  return { ...state, retry };
};

const EMPTY_REGION = { province: null, regency: null, district: null, village: null };

const Checkout = ({ user }) => {
  const navigate = useNavigate();
  const { cart, refresh, setCart, onUnauthorized } = useCart();
  const [loading, setLoading] = useState(!cart);
  // Nama penerima SENGAJA kosong (penerima bisa berbeda dari pemilik akun); telepon boleh diisi dari akun.
  const [form, setForm] = useState({
    recipientName: '',
    recipientPhone: user?.phone || '',
    address: '',
    postalCode: '',
    note: '',
  });
  const [region, setRegion] = useState(EMPTY_REGION);
  const [errors, setErrors] = useState({});
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  // Server menjawab 409 price_changed: keranjang terkini ditampilkan, pelanggan harus konfirmasi ulang.
  const [priceChanged, setPriceChanged] = useState(false);
  const [acking, setAcking] = useState(false);
  // Satu kunci per percobaan checkout: klik ganda / kirim ulang -> pesanan yang sama.
  const idemKey = useRef(newIdempotencyKey());
  const inFlight = useRef(false); // cegah klik ganda sebelum state sempat diperbarui
  const refs = useRef({});
  const refFor = (k) => {
    if (!refs.current[k]) refs.current[k] = React.createRef();
    return refs.current[k];
  };

  const lists = [
    useRegionList(0, null),
    useRegionList(1, region.province?.code),
    useRegionList(2, region.regency?.code),
    useRegionList(3, region.district?.code),
  ];

  useEffect(() => {
    refresh().finally(() => setLoading(false));
  }, [refresh]);

  const clearErr = (k) => setErrors((e) => (e[k] ? { ...e, [k]: undefined } : e));
  const set = (k) => (e) => {
    let v = e.target.value;
    if (k === 'postalCode') v = v.replace(/[^0-9]/g, '').slice(0, 5);
    setForm({ ...form, [k]: v });
    clearErr(k);
  };

  // Memilih tingkat atas mengosongkan semua tingkat di bawahnya.
  const pickRegion = (level) => (item) => {
    const key = REGION_LEVELS[level].key;
    setRegion((r) => {
      if (r[key]?.code === item.code) return r;
      const next = { ...r, [key]: item };
      REGION_LEVELS.slice(level + 1).forEach((lv) => {
        next[lv.key] = null;
      });
      return next;
    });
    clearErr(REGION_LEVELS[level].field);
  };

  const focusFirst = (errs) => {
    const k = FIELD_ORDER.find((f) => errs[f]);
    const el = k && refs.current[k]?.current;
    if (el && typeof el.focus === 'function') el.focus();
  };

  const submit = async (e) => {
    e.preventDefault();
    if (inFlight.current) return;
    setError('');
    const errs = validateCheckoutForm(form, region);
    if (Object.keys(errs).length) {
      setErrors(errs);
      setError('Lengkapi data yang ditandai merah.');
      focusFirst(errs);
      return;
    }
    setErrors({});
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
      // Hanya data penerima (wilayah berupa KODE; nama diambil server dari DB) + kunci idempotensi + total yang
      // DITAMPILKAN; item & harga dihitung server dari keranjang.
      const res = await createOrder({
        recipientName: form.recipientName.trim(),
        recipientPhone: form.recipientPhone.trim(),
        provinceCode: region.province.code,
        regencyCode: region.regency.code,
        districtCode: region.district.code,
        villageCode: region.village.code,
        address: form.address.trim(),
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
      const st = err.response?.status;
      if (st === 409 && data?.error === 'price_changed') {
        if (data.cart && Array.isArray(data.cart.items)) setCart(data.cart);
        else refresh();
        setPriceChanged(true);
        setError('');
        return;
      }
      if (st === 422 && data?.field && FIELD_ORDER.includes(data.field)) {
        const errs = { [data.field]: data.error };
        setErrors(errs);
        setError('Periksa kembali data yang ditandai merah.');
        focusFirst(errs);
        return;
      }
      setError(errorMessage(err, 'Gagal membuat pesanan, coba lagi.'));
      if (st === 409 || st === 400) refresh();
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
          <p className="text-xs text-gray-500">
            Semua kolom bertanda <span className="text-red-600">*</span> wajib diisi.
          </p>
          <Field id="co-name" label="Nama penerima" required error={errors.recipientName}>
            <input
              id="co-name"
              ref={refFor('recipientName')}
              className={inputCls(errors.recipientName)}
              maxLength={100}
              value={form.recipientName}
              onChange={set('recipientName')}
              autoComplete="name"
              placeholder="Nama lengkap penerima paket"
              aria-invalid={errors.recipientName ? 'true' : undefined}
              aria-describedby={errors.recipientName ? 'co-name-err' : undefined}
              required
            />
          </Field>
          <Field id="co-phone" label="Nomor telepon/WhatsApp" hint="(08xx / +628xx)" required error={errors.recipientPhone}>
            <input
              id="co-phone"
              ref={refFor('recipientPhone')}
              className={inputCls(errors.recipientPhone)}
              inputMode="tel"
              maxLength={20}
              value={form.recipientPhone}
              onChange={set('recipientPhone')}
              autoComplete="tel"
              aria-invalid={errors.recipientPhone ? 'true' : undefined}
              aria-describedby={errors.recipientPhone ? 'co-phone-err' : undefined}
              required
            />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {REGION_LEVELS.map((lv, i) => {
              const parent = i > 0 ? REGION_LEVELS[i - 1] : null;
              return (
                <RegionCombobox
                  key={lv.key}
                  id={`co-${lv.key}`}
                  inputRef={refFor(lv.field)}
                  label={lv.label}
                  lower={lv.lower}
                  value={region[lv.key]}
                  items={lists[i].items}
                  loading={lists[i].loading}
                  error={lists[i].error}
                  onRetry={lists[i].retry}
                  onChange={pickRegion(i)}
                  disabled={!!parent && !region[parent.key]}
                  disabledHint={parent ? `Pilih ${parent.lower} dulu` : ''}
                  invalid={!!errors[lv.field]}
                  errorText={errors[lv.field]}
                />
              );
            })}
          </div>
          <Field id="co-address" label="Alamat lengkap" hint="(jalan, RT/RW, nomor rumah)" required error={errors.address}>
            <textarea
              id="co-address"
              ref={refFor('address')}
              className={inputCls(errors.address)}
              rows={3}
              maxLength={500}
              value={form.address}
              onChange={set('address')}
              autoComplete="street-address"
              placeholder="Contoh: Jl. Melati No. 9, RT 03/RW 05"
              aria-invalid={errors.address ? 'true' : undefined}
              aria-describedby={errors.address ? 'co-address-err' : undefined}
              required
            />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Field id="co-postal" label="Kode pos" hint="(5 digit)" required error={errors.postalCode}>
              <input
                id="co-postal"
                ref={refFor('postalCode')}
                className={inputCls(errors.postalCode)}
                inputMode="numeric"
                pattern="[0-9]{5}"
                maxLength={5}
                value={form.postalCode}
                onChange={set('postalCode')}
                autoComplete="postal-code"
                aria-invalid={errors.postalCode ? 'true' : undefined}
                aria-describedby={errors.postalCode ? 'co-postal-err' : undefined}
                required
              />
            </Field>
          </div>
          <Field id="co-note" label="Catatan" hint="(opsional)" error={errors.note}>
            <textarea id="co-note" ref={refFor('note')} className={inputCls(errors.note)} rows={2} maxLength={500} value={form.note} onChange={set('note')} />
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
