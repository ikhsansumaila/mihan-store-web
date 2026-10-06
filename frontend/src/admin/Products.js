import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { adminFetch, rupiah } from './api';
import { ErrorBox, Modal, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';
import { InfiniteFooter, useInfiniteList } from './infiniteList';
import { MAX_TIERS, UNIT_SUGGESTIONS, analyzeTiers, normalizeUnit, tiersToBody, tiersToRows } from '../pricing';
import MoneyInput from '../components/MoneyInput';
import ProductPhotoField, { uploadProductPhoto } from './ProductPhoto';
import { SmallThumb } from '../shop/productImage';

// Batas digit kolom uang: harga eceran/jenjang maksimal Rp 1.000.000.000 (10 digit) seperti batas server.
const PRICE_MAX = 1000000000;
const PRICE_DIGITS = 10;

// Foto produk tidak lagi lewat "path gambar": diunggah lewat bagian "Foto produk" (image/thumb = URL foto tersimpan).
const emptyForm = { name: '', categoryId: '', price: '', unit: 'pcs', description: '', image: '', thumb: '', isActive: true, tiers: [] };

let tierKeySeq = 0;
const newTierRow = () => {
  tierKeySeq += 1;
  return { key: `n${tierKeySeq}`, minQty: '', type: 'fixed', value: '' };
};

// Editor jenjang harga grosir: baris dinamis (jumlah minimal, jenis Rp/%, nilai, hapus), pratinjau harga efektif,
// peringatan langsung (aturan sama dengan server), dan kesalahan server (422) per baris.
export const TierEditor = ({ rows, onChange, basePrice, unit, serverErrors }) => {
  const analysis = useMemo(() => analyzeTiers(basePrice, rows), [basePrice, rows]);
  const setRow = (i, patch) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const full = rows.length >= MAX_TIERS;
  const preview = analysis.rows.filter((r) => r.price !== null).sort((a, b) => a.minQty - b.minQty);
  const u = unit || 'pcs';
  return (
    <fieldset className="rounded-md border border-gray-200 p-3" data-testid="tier-editor">
      <legend className="px-1 text-sm font-semibold text-gray-700">Harga grosir</legend>
      <p className="mb-2 text-xs text-gray-500">
        Otomatis untuk semua pelanggan berdasarkan jumlah per produk; harga jenjang berlaku untuk semua unit di keranjang. Makin besar
        jumlah minimal, harga harus makin murah. Maksimal {MAX_TIERS} jenjang.
      </p>
      {rows.length === 0 && <p className="mb-2 text-sm text-gray-500">Belum ada jenjang (hanya harga eceran).</p>}
      <ul className="space-y-2">
        {rows.map((r, i) => {
          const errs = [...(analysis.rows[i]?.errors || []), ...((serverErrors && serverErrors[i]) || [])];
          return (
            <li key={r.key} data-testid="tier-row" className={`rounded-md border p-2 ${errs.length ? 'border-red-300 bg-red-50' : 'border-gray-200'}`}>
              <div className="flex flex-wrap items-end gap-2">
                <label className="w-28">
                  <span className="block text-xs text-gray-600">Jumlah min. ({u})</span>
                  <input
                    className={inputClass}
                    inputMode="numeric"
                    aria-label={`Jumlah minimal jenjang ${i + 1}`}
                    value={r.minQty}
                    onChange={(e) => setRow(i, { minQty: e.target.value.replace(/[^0-9]/g, '') })}
                    placeholder="10"
                  />
                </label>
                <div role="group" aria-label={`Jenis potongan jenjang ${i + 1}`} className="flex">
                  {[
                    ['fixed', 'Rp'],
                    ['percent', '%'],
                  ].map(([k, label]) => (
                    <button
                      key={k}
                      type="button"
                      aria-pressed={r.type === k}
                      className={`h-10 min-w-[44px] border px-3 text-sm font-semibold first:rounded-l-md last:rounded-r-md ${
                        r.type === k ? 'border-purple-700 bg-purple-700 text-white' : 'border-gray-300 bg-white text-gray-700'
                      }`}
                      onClick={() =>
                        // Ke "Rp": nilai persen berdesimal (mis. "12,5") tidak bisa jadi rupiah -> dikosongkan agar
                        // kolom uang tidak menampilkan angka yang berbeda dari nilai sebenarnya.
                        setRow(i, k === 'fixed' && !/^[0-9]*$/.test(String(r.value).trim()) ? { type: k, value: '' } : { type: k })
                      }
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <label className="min-w-[8rem] flex-1">
                  <span className="block text-xs text-gray-600">{r.type === 'percent' ? 'Diskon (%)' : `Harga per ${u} (Rp)`}</span>
                  {r.type === 'percent' ? (
                    <input
                      className={inputClass}
                      inputMode="decimal"
                      aria-label={`Nilai jenjang ${i + 1}`}
                      value={r.value}
                      onChange={(e) => setRow(i, { value: e.target.value.replace(/[^0-9.,]/g, '') })}
                      placeholder="5"
                    />
                  ) : (
                    <MoneyInput
                      className={inputClass}
                      aria-label={`Nilai jenjang ${i + 1}`}
                      value={r.value}
                      onValueChange={(digits) => setRow(i, { value: digits })}
                      maxDigits={PRICE_DIGITS}
                      min={1}
                      max={PRICE_MAX}
                      placeholder="42.000"
                    />
                  )}
                </label>
                <button
                  type="button"
                  className="h-10 rounded-md px-3 text-sm font-medium text-red-600 hover:bg-red-100"
                  onClick={() => onChange(rows.filter((_, j) => j !== i))}
                  aria-label={`Hapus jenjang ${i + 1}`}
                >
                  Hapus
                </button>
              </div>
              {errs.map((m, k) => (
                <p key={k} role="alert" className="mt-1 text-xs text-red-700">
                  {m}
                </p>
              ))}
            </li>
          );
        })}
      </ul>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <button type="button" className={btnSecondary} onClick={() => onChange([...rows, newTierRow()])} disabled={full}>
          + Tambah jenjang
        </button>
        <span className="text-xs text-gray-500">
          {rows.length}/{MAX_TIERS} jenjang{full ? ' (batas maksimal tercapai)' : ''}
        </span>
      </div>
      {preview.length > 0 && (
        <div className="mt-3 overflow-x-auto">
          <table className="w-full text-sm" data-testid="tier-preview">
            <caption className="mb-1 text-left text-xs font-semibold text-gray-600">Pratinjau harga per {u}</caption>
            <thead className="text-left text-xs text-gray-500">
              <tr>
                <th className="py-1 pr-2">Jumlah</th>
                <th className="py-1 pr-2 text-right">Harga efektif</th>
                <th className="py-1 text-right">Hemat per {u}</th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-t">
                <td className="py-1 pr-2">1+ (eceran)</td>
                <td className="py-1 pr-2 text-right">{rupiah(Number(basePrice) || 0)}</td>
                <td className="py-1 text-right text-gray-400">-</td>
              </tr>
              {preview.map((r) => (
                <tr key={r.index} className={`border-t ${r.errors.length ? 'text-red-700' : ''}`}>
                  <td className="py-1 pr-2">{r.minQty}+</td>
                  <td className="py-1 pr-2 text-right font-semibold">{rupiah(r.price)}</td>
                  <td className="py-1 text-right">
                    {rupiah(r.savePerUnit)} ({r.savePct.toLocaleString('id-ID')}%)
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </fieldset>
  );
};

const ProductForm = ({ initial, categories, onCancel, onSaved, onChanged }) => {
  const [form, setForm] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  // Foto: tersimpan (current) dan foto baru terpilih (pending, sudah diperkecil di browser).
  const [photo, setPhoto] = useState({ image: initial.image || '', thumb: initial.thumb || '' });
  const [pending, setPending] = useState(null);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState(0);
  // Produk baru yang sudah tersimpan (id) bila unggah fotonya gagal: simpan ulang memakai PUT, bukan POST.
  const [createdId, setCreatedId] = useState(null);
  const [uploadError, setUploadError] = useState(null);
  const productId = initial.id || createdId;
  const [tierServerErrors, setTierServerErrors] = useState(null);
  const errRef = useRef(null);
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value });
  const priceNum = String(form.price).trim() === '' ? NaN : Number(String(form.price).replace(/[^0-9]/g, ''));
  const analysis = useMemo(() => analyzeTiers(priceNum, form.tiers), [priceNum, form.tiers]);
  const unitNorm = normalizeUnit(form.unit);

  const fail = (err) => {
    setError(err);
    setTimeout(() => errRef.current?.scrollIntoView?.({ block: 'nearest' }), 0);
  };

  const submit = async (e) => {
    e.preventDefault();
    setError(null);
    setTierServerErrors(null);
    const price = Number(String(form.price).replace(/[^0-9]/g, ''));
    if (!form.name.trim()) return fail(new Error('Nama produk wajib diisi'));
    if (!form.categoryId) return fail(new Error('Kategori wajib dipilih'));
    if (String(form.price).trim() === '' || !Number.isInteger(price) || price > PRICE_MAX)
      return fail(new Error('Harga harus bilangan bulat 0 sampai 1.000.000.000'));
    if (!unitNorm) return fail(new Error('Satuan hanya boleh huruf, angka, spasi, titik, atau garis miring (maksimal 20 karakter)'));
    if (form.tiers.length > MAX_TIERS) return fail(new Error(`Maksimal ${MAX_TIERS} jenjang harga grosir per produk`));
    if (!analysis.valid && form.tiers.length > 0) return fail(new Error('Perbaiki jenjang harga grosir yang ditandai merah.'));
    const body = {
      name: form.name.trim(),
      categoryId: Number(form.categoryId),
      price,
      unit: unitNorm,
      description: form.description,
      isActive: !!form.isActive,
      tiers: tiersToBody(form.tiers),
    };
    setSaving(true);
    setUploadError(null);
    let id = productId;
    try {
      if (id) await adminFetch(`/products/${id}`, { method: 'PUT', body });
      else {
        const created = await adminFetch('/products', { method: 'POST', body });
        id = created?.id;
        if (id) setCreatedId(id);
        onChanged?.();
      }
    } catch (err) {
      const te = err?.data?.tierErrors;
      if (Array.isArray(te) && te.length) {
        const byIndex = {};
        te.forEach((x) => {
          const i = Number(x.index);
          if (i >= 0 && i < form.tiers.length) (byIndex[i] = byIndex[i] || []).push(x.message);
        });
        setTierServerErrors(byIndex);
      }
      fail(err);
      setSaving(false);
      return undefined;
    }
    setSaving(false);
    if (pending && id) {
      const ok = await doUpload(id);
      if (!ok) return undefined;
    }
    onSaved();
    return undefined;
  };

  // Unggah foto baru ke produk id. false bila gagal (produk tetap tersimpan; pesan + tombol coba lagi).
  const doUpload = async (id) => {
    setUploading(true);
    setProgress(0);
    setUploadError(null);
    try {
      const res = await uploadProductPhoto(id, pending.blob, setProgress);
      setPhoto({ image: res?.image || '', thumb: res?.thumb || '' });
      setPending(null);
      return true;
    } catch (err) {
      setUploadError(err);
      setTimeout(() => errRef.current?.scrollIntoView?.({ block: 'nearest' }), 0);
      return false;
    } finally {
      setUploading(false);
    }
  };

  const retryUpload = async () => {
    if (!productId || !pending) return;
    if (await doUpload(productId)) onSaved();
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <div ref={errRef}>
        <ErrorBox error={error} />
        {uploadError && (
          <div role="alert" className="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900" data-testid="photo-upload-failed">
            <p className="font-semibold">Produk sudah tersimpan, tetapi foto gagal diunggah.</p>
            <p className="mt-1">
              {uploadError.sessionExpired ? 'Sesi admin berakhir, muat ulang halaman lalu unggah lagi lewat Ubah.' : uploadError.message}
            </p>
            <div className="mt-2 flex flex-wrap gap-2">
              {!uploadError.sessionExpired && pending && (
                <button type="button" className={btnPrimary} onClick={retryUpload} disabled={uploading}>
                  Coba unggah lagi
                </button>
              )}
              <button type="button" className={btnSecondary} onClick={onSaved} disabled={uploading}>
                Tutup (foto bisa ditambahkan nanti)
              </button>
            </div>
          </div>
        )}
      </div>
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">Nama produk</label>
        <input className={inputClass} maxLength={150} value={form.name} onChange={set('name')} required />
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div>
          <label className="block text-sm font-semibold text-gray-700 mb-1">Kategori</label>
          <select className={inputClass} value={form.categoryId} onChange={set('categoryId')} required>
            <option value="">Pilih kategori</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_7rem] gap-2">
          <div>
            <label htmlFor="pf-price" className="block text-sm font-semibold text-gray-700 mb-1">
              Harga eceran (Rp)
            </label>
            <MoneyInput
              id="pf-price"
              className={inputClass}
              value={form.price}
              onValueChange={(digits) => setForm((f) => ({ ...f, price: digits }))}
              maxDigits={PRICE_DIGITS}
              min={0}
              max={PRICE_MAX}
              placeholder="15.000"
              required
            />
          </div>
          <div>
            <label htmlFor="pf-unit" className="block text-sm font-semibold text-gray-700 mb-1">
              Satuan
            </label>
            <input
              id="pf-unit"
              className={unitNorm ? inputClass : inputClass.replace('border-gray-300', 'border-red-500')}
              list="pf-unit-list"
              maxLength={20}
              value={form.unit}
              onChange={set('unit')}
              placeholder="pcs"
              aria-invalid={!unitNorm}
            />
            <datalist id="pf-unit-list">
              {UNIT_SUGGESTIONS.map((u) => (
                <option key={u} value={u} />
              ))}
            </datalist>
          </div>
        </div>
      </div>
      {!unitNorm && <p className="-mt-2 text-xs text-red-700">Satuan hanya boleh huruf, angka, spasi, titik, atau garis miring (maks. 20).</p>}
      <TierEditor
        rows={form.tiers}
        onChange={(tiers) => {
          setTierServerErrors(null);
          setForm((f) => ({ ...f, tiers }));
        }}
        basePrice={priceNum}
        unit={unitNorm || 'pcs'}
        serverErrors={tierServerErrors}
      />
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">Deskripsi</label>
        <textarea className={inputClass} rows={3} maxLength={2000} value={form.description} onChange={set('description')} />
      </div>
      <ProductPhotoField
        productId={productId}
        name={form.name}
        current={photo}
        pending={pending}
        onPending={(p) => {
          setUploadError(null);
          setPending(p);
        }}
        onDeleted={() => {
          setPhoto({ image: '', thumb: '' });
          onChanged?.();
        }}
        uploading={uploading}
        progress={progress}
        disabled={saving}
      />
      <label className="flex items-center gap-2 text-sm text-gray-700">
        <input type="checkbox" checked={!!form.isActive} onChange={set('isActive')} /> Aktif (tampil di toko)
      </label>
      <div className="flex justify-end gap-2 pt-2">
        <button type="button" className={btnSecondary} onClick={onCancel}>
          Batal
        </button>
        <button type="submit" className={btnPrimary} disabled={saving || uploading}>
          {saving ? 'Menyimpan...' : uploading ? 'Mengunggah foto…' : 'Simpan'}
        </button>
      </div>
    </form>
  );
};

const toForm = (p) => ({
  id: p.id,
  name: p.name,
  categoryId: p.categoryDeleted ? '' : String(p.categoryId),
  price: String(p.price),
  unit: p.unit || 'pcs',
  tiers: tiersToRows(p.tiers),
  description: p.description || '',
  image: p.image || '',
  thumb: p.thumb || '',
  isActive: p.isActive,
});

const TierBadge = ({ tiers }) =>
  tiers?.length > 0 ? (
    <span
      className="mt-0.5 inline-block rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-semibold text-amber-800"
      title={tiers.map((t) => `${t.minQty}+ : ${rupiah(t.unitPrice)}`).join(' · ')}
    >
      Grosir ({tiers.length} jenjang)
    </span>
  ) : null;

const ActiveToggle = ({ p, busy, onToggle }) => (
  <button
    type="button"
    onClick={(e) => {
      e.stopPropagation();
      onToggle(p);
    }}
    disabled={busy}
    className={`px-2 py-1 rounded-full text-xs font-semibold ${p.isActive ? 'bg-green-100 text-green-800' : 'bg-gray-200 text-gray-700'}`}
    title="Klik untuk mengubah status"
  >
    {p.isActive ? 'Aktif' : 'Nonaktif'}
  </button>
);

const Products = () => {
  const [filters, setFilters] = useState({ q: '', category: '', status: '' });
  const [categories, setCategories] = useState([]);
  const [error, setError] = useState(null);
  const [editing, setEditing] = useState(null); // objek form atau null
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [busyId, setBusyId] = useState(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const topRef = useRef(null);
  // Gulir tanpa batas; ganti pencarian/kategori/status = mulai lagi dari halaman 1.
  const list = useInfiniteList('/products', filters, { topRef });
  const { refresh } = list;

  // Pintasan Dashboard "Tambah Produk" (/admin/products?tambah=1): buka form tambah sekali.
  useEffect(() => {
    if (searchParams.get('tambah') === '1') {
      setEditing({ ...emptyForm });
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    adminFetch('/categories')
      .then((r) => setCategories(r.items || []))
      .catch((err) => setError(err));
  }, []);

  const toggle = async (p) => {
    setBusyId(p.id);
    try {
      await adminFetch(`/products/${p.id}/active`, { method: 'PATCH', body: { active: !p.isActive } });
      await refresh();
    } catch (err) {
      setError(err);
    } finally {
      setBusyId(null);
    }
  };

  const doDelete = async () => {
    const p = confirmDelete;
    setBusyId(p.id);
    try {
      await adminFetch(`/products/${p.id}`, { method: 'DELETE', body: {} });
      setConfirmDelete(null);
      await refresh();
    } catch (err) {
      setError(err);
      setConfirmDelete(null);
    } finally {
      setBusyId(null);
    }
  };

  const setFilter = (k) => (e) => {
    setFilters({ ...filters, [k]: e.target.value });
  };

  const firstLoad = list.loading && list.items.length === 0;
  const empty = !list.loading && !list.error && list.items.length === 0;

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <h1 className="text-xl sm:text-2xl font-semibold text-gray-900">Produk</h1>
        <button className={btnPrimary} onClick={() => setEditing({ ...emptyForm })}>
          + Tambah produk
        </button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-4">
        <input className={inputClass} placeholder="Cari nama produk..." value={filters.q} onChange={setFilter('q')} />
        <select className={inputClass} value={filters.category} onChange={setFilter('category')}>
          <option value="">Semua kategori</option>
          {categories.map((c) => (
            <option key={c.id} value={c.slug}>
              {c.name}
            </option>
          ))}
        </select>
        <select className={inputClass} value={filters.status} onChange={setFilter('status')}>
          <option value="">Semua status</option>
          <option value="aktif">Aktif</option>
          <option value="nonaktif">Nonaktif</option>
        </select>
      </div>

      <ErrorBox error={error || list.error} />

      <div ref={topRef} className="mt-3">
        {/* HP (< md): kartu per produk; ketuk kartu = Ubah. Tombol status/Ubah/Hapus tetap terlihat tanpa geser. */}
        <ul className="space-y-2 md:hidden" data-testid="product-cards">
          {firstLoad && <li className="rounded-lg border border-gray-200 bg-white p-6 text-center text-sm text-gray-500">Memuat...</li>}
          {empty && <li className="rounded-lg border border-gray-200 bg-white p-6 text-center text-sm text-gray-500">Tidak ada produk.</li>}
          {list.items.map((p) => (
            <li
              key={p.id}
              data-testid="product-card"
              onClick={() => setEditing(toForm(p))}
              className="cursor-pointer rounded-lg border border-gray-200 bg-white p-3 text-sm shadow-sm active:bg-purple-50"
            >
              <div className="flex gap-3">
                <SmallThumb src={p.thumb} alt={p.name} size={56} />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-gray-800 [overflow-wrap:anywhere]">{p.name}</div>
                  <div className="text-xs text-gray-500">
                    #{p.id} · {p.categoryName}
                    {p.categoryDeleted && <span className="ml-1 text-red-600">(dihapus)</span>}
                  </div>
                  <div className="mt-1 font-semibold text-gray-900">
                    {rupiah(p.price)} <span className="text-xs font-normal text-gray-500">/ {p.unit || 'pcs'}</span>
                  </div>
                  <TierBadge tiers={p.tiers} />
                </div>
              </div>
              <div className="mt-2 flex items-center justify-between gap-2 border-t border-gray-100 pt-2">
                <ActiveToggle p={p} busy={busyId === p.id} onToggle={toggle} />
                <div className="flex gap-4">
                  <button
                    type="button"
                    className="py-1 font-medium text-purple-700"
                    onClick={(e) => {
                      e.stopPropagation();
                      setEditing(toForm(p));
                    }}
                  >
                    Ubah
                  </button>
                  <button
                    type="button"
                    className="py-1 font-medium text-red-600"
                    onClick={(e) => {
                      e.stopPropagation();
                      setConfirmDelete(p);
                    }}
                  >
                    Hapus
                  </button>
                </div>
              </div>
            </li>
          ))}
        </ul>

        {/* md ke atas: tabel. */}
        <div className="hidden overflow-x-auto bg-white rounded-lg border border-gray-200 shadow-sm md:block">
          <table className="min-w-full text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-2">ID</th>
                <th className="px-3 py-2">Nama</th>
                <th className="px-3 py-2">Kategori</th>
                <th className="px-3 py-2 text-right">Harga</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2 text-right">Aksi</th>
              </tr>
            </thead>
            <tbody>
              {firstLoad ? (
                <tr>
                  <td colSpan={6} className="px-3 py-6 text-center text-gray-500">
                    Memuat...
                  </td>
                </tr>
              ) : empty ? (
                <tr>
                  <td colSpan={6} className="px-3 py-6 text-center text-gray-500">
                    Tidak ada produk.
                  </td>
                </tr>
              ) : (
                list.items.map((p) => (
                  <tr key={p.id} className="border-t border-gray-100 hover:bg-gray-50">
                    <td className="px-3 py-2 text-gray-500">{p.id}</td>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        <SmallThumb src={p.thumb} alt={p.name} size={40} />
                        <div className="min-w-0">
                          <div className="font-medium text-gray-800">{p.name}</div>
                          {p.description && <div className="text-xs text-gray-500 line-clamp-1 max-w-xs">{p.description}</div>}
                        </div>
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      {p.categoryName}
                      {p.categoryDeleted && <span className="ml-1 text-xs text-red-600">(dihapus)</span>}
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">
                      {rupiah(p.price)} <span className="text-xs text-gray-500">/ {p.unit || 'pcs'}</span>
                      {p.tiers?.length > 0 && (
                        <div>
                          <TierBadge tiers={p.tiers} />
                        </div>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      <ActiveToggle p={p} busy={busyId === p.id} onToggle={toggle} />
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">
                      <button className="text-purple-700 hover:underline mr-3" onClick={() => setEditing(toForm(p))}>
                        Ubah
                      </button>
                      <button className="text-red-600 hover:underline" onClick={() => setConfirmDelete(p)}>
                        Hapus
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
      <InfiniteFooter list={list} noun="produk" />

      {editing && (
        <Modal title={editing.id ? `Ubah produk #${editing.id}` : 'Tambah produk'} onClose={() => setEditing(null)} wide>
          <ProductForm
            initial={editing}
            categories={categories}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              refresh();
            }}
            onChanged={refresh}
          />
        </Modal>
      )}

      {confirmDelete && (
        <Modal title="Hapus produk?" onClose={() => setConfirmDelete(null)}>
          <p className="text-gray-700">
            Produk <strong>{confirmDelete.name}</strong> akan dihapus dari toko dan daftar admin. Lanjutkan?
          </p>
          <div className="flex justify-end gap-2 mt-6">
            <button className={btnSecondary} onClick={() => setConfirmDelete(null)}>
              Batal
            </button>
            <button className={btnDanger} onClick={doDelete} disabled={busyId === confirmDelete.id}>
              Ya, hapus
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

export default Products;
