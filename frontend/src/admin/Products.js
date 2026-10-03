import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { adminFetch, qs, rupiah } from './api';
import { ErrorBox, Modal, Pagination, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';
import { MAX_TIERS, UNIT_SUGGESTIONS, analyzeTiers, normalizeUnit, tiersToBody, tiersToRows } from '../pricing';

const emptyForm = { name: '', categoryId: '', price: '', unit: 'pcs', description: '', imagePath: '', isActive: true, tiers: [] };

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
                      onClick={() => setRow(i, { type: k })}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <label className="min-w-[8rem] flex-1">
                  <span className="block text-xs text-gray-600">{r.type === 'percent' ? 'Diskon (%)' : `Harga per ${u} (Rp)`}</span>
                  <input
                    className={inputClass}
                    inputMode="decimal"
                    aria-label={`Nilai jenjang ${i + 1}`}
                    value={r.value}
                    onChange={(e) => setRow(i, { value: e.target.value.replace(/[^0-9.,]/g, '') })}
                    placeholder={r.type === 'percent' ? '5' : '42000'}
                  />
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

const ProductForm = ({ initial, categories, onCancel, onSaved }) => {
  const [form, setForm] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
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
    if (String(form.price).trim() === '' || !Number.isInteger(price) || price > 1000000000)
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
      imagePath: form.imagePath.trim(),
      isActive: !!form.isActive,
      tiers: tiersToBody(form.tiers),
    };
    setSaving(true);
    try {
      if (initial.id) await adminFetch(`/products/${initial.id}`, { method: 'PUT', body });
      else await adminFetch('/products', { method: 'POST', body });
      onSaved();
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
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <div ref={errRef}>
        <ErrorBox error={error} />
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
            <input id="pf-price" className={inputClass} inputMode="numeric" value={form.price} onChange={set('price')} placeholder="15000" required />
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
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">
          Path gambar <span className="font-normal text-gray-500">(opsional, upload menyusul)</span>
        </label>
        <input className={inputClass} maxLength={255} value={form.imagePath} onChange={set('imagePath')} placeholder="kerupuk5.jpg" />
      </div>
      <label className="flex items-center gap-2 text-sm text-gray-700">
        <input type="checkbox" checked={!!form.isActive} onChange={set('isActive')} /> Aktif (tampil di toko)
      </label>
      <div className="flex justify-end gap-2 pt-2">
        <button type="button" className={btnSecondary} onClick={onCancel}>
          Batal
        </button>
        <button type="submit" className={btnPrimary} disabled={saving}>
          {saving ? 'Menyimpan...' : 'Simpan'}
        </button>
      </div>
    </form>
  );
};

const Products = () => {
  const [filters, setFilters] = useState({ q: '', category: '', status: '' });
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0, perPage: 20 });
  const [categories, setCategories] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(null); // objek form atau null
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [busyId, setBusyId] = useState(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // Pintasan Dashboard "Tambah Produk" (/admin/products?tambah=1): buka form tambah sekali.
  useEffect(() => {
    if (searchParams.get('tambah') === '1') {
      setEditing({ ...emptyForm });
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await adminFetch(`/products${qs({ ...filters, page, per_page: 20 })}`);
      setData(res);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [filters, page]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    adminFetch('/categories')
      .then((r) => setCategories(r.items || []))
      .catch((err) => setError(err));
  }, []);

  const toggle = async (p) => {
    setBusyId(p.id);
    try {
      await adminFetch(`/products/${p.id}/active`, { method: 'PATCH', body: { active: !p.isActive } });
      await load();
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
      await load();
    } catch (err) {
      setError(err);
      setConfirmDelete(null);
    } finally {
      setBusyId(null);
    }
  };

  const setFilter = (k) => (e) => {
    setPage(1);
    setFilters({ ...filters, [k]: e.target.value });
  };

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

      <ErrorBox error={error} />

      <div className="mt-3 overflow-x-auto bg-white rounded-lg border border-gray-200 shadow-sm">
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
            {loading ? (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-gray-500">
                  Memuat...
                </td>
              </tr>
            ) : data.items.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-gray-500">
                  Tidak ada produk.
                </td>
              </tr>
            ) : (
              data.items.map((p) => (
                <tr key={p.id} className="border-t border-gray-100 hover:bg-gray-50">
                  <td className="px-3 py-2 text-gray-500">{p.id}</td>
                  <td className="px-3 py-2">
                    <div className="font-medium text-gray-800">{p.name}</div>
                    {p.description && <div className="text-xs text-gray-500 line-clamp-1 max-w-xs">{p.description}</div>}
                  </td>
                  <td className="px-3 py-2">
                    {p.categoryName}
                    {p.categoryDeleted && <span className="ml-1 text-xs text-red-600">(dihapus)</span>}
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    {rupiah(p.price)} <span className="text-xs text-gray-500">/ {p.unit || 'pcs'}</span>
                    {p.tiers?.length > 0 && (
                      <div>
                        <span
                          className="mt-0.5 inline-block rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-semibold text-amber-800"
                          title={p.tiers.map((t) => `${t.minQty}+ : ${rupiah(t.unitPrice)}`).join(' · ')}
                        >
                          Grosir ({p.tiers.length} jenjang)
                        </span>
                      </div>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    <button
                      onClick={() => toggle(p)}
                      disabled={busyId === p.id}
                      className={`px-2 py-1 rounded-full text-xs font-semibold ${
                        p.isActive ? 'bg-green-100 text-green-800' : 'bg-gray-200 text-gray-700'
                      }`}
                      title="Klik untuk mengubah status"
                    >
                      {p.isActive ? 'Aktif' : 'Nonaktif'}
                    </button>
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    <button
                      className="text-purple-700 hover:underline mr-3"
                      onClick={() =>
                        setEditing({
                          id: p.id,
                          name: p.name,
                          categoryId: p.categoryDeleted ? '' : String(p.categoryId),
                          price: String(p.price),
                          unit: p.unit || 'pcs',
                          tiers: tiersToRows(p.tiers),
                          description: p.description || '',
                          imagePath: p.imagePath || '',
                          isActive: p.isActive,
                        })
                      }
                    >
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
      <Pagination page={page} perPage={data.perPage} total={data.total} onPage={setPage} />

      {editing && (
        <Modal title={editing.id ? `Ubah produk #${editing.id}` : 'Tambah produk'} onClose={() => setEditing(null)} wide>
          <ProductForm
            initial={editing}
            categories={categories}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              load();
            }}
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
