import React, { useCallback, useEffect, useState } from 'react';
import { adminFetch } from './api';
import { ErrorBox, Modal, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';

const SLUG_RE = /^[a-z0-9_]{2,50}$/;

const CategoryForm = ({ initial, onCancel, onSaved }) => {
  const [form, setForm] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  const submit = async (e) => {
    e.preventDefault();
    setError(null);
    const slug = form.slug.trim();
    if (!SLUG_RE.test(slug)) return setError(new Error('Slug harus 2–50 karakter: huruf kecil, angka, atau garis bawah (_)'));
    if (!form.name.trim()) return setError(new Error('Nama kategori wajib diisi'));
    const sortOrder = Number(form.sortOrder || 0);
    if (!Number.isInteger(sortOrder)) return setError(new Error('Urutan harus bilangan bulat'));
    setSaving(true);
    try {
      const body = { slug, name: form.name.trim(), sortOrder };
      if (initial.id) await adminFetch(`/categories/${initial.id}`, { method: 'PUT', body });
      else await adminFetch('/categories', { method: 'POST', body });
      onSaved();
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <ErrorBox error={error} />
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">Nama</label>
        <input className={inputClass} maxLength={80} value={form.name} onChange={set('name')} required />
      </div>
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">Slug</label>
        <input
          className={inputClass}
          maxLength={50}
          value={form.slug}
          onChange={(e) => setForm({ ...form, slug: e.target.value.toLowerCase() })}
          placeholder="bumbu_dapur"
          required
        />
        <p className="mt-1 text-xs text-gray-500">
          Huruf kecil, angka, garis bawah. Dipakai sebagai nilai <code>category</code> di API toko.
        </p>
      </div>
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-1">Urutan</label>
        <input className={inputClass} inputMode="numeric" value={form.sortOrder} onChange={set('sortOrder')} />
      </div>
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

const Categories = () => {
  const [items, setItems] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await adminFetch('/categories');
      setItems(r.items || []);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const editCategory = (c) => setEditing({ id: c.id, name: c.name, slug: c.slug, sortOrder: String(c.sortOrder) });

  const doDelete = async () => {
    setBusy(true);
    try {
      await adminFetch(`/categories/${confirmDelete.id}`, { method: 'DELETE', body: {} });
      setConfirmDelete(null);
      await load();
    } catch (err) {
      setError(err);
      setConfirmDelete(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <h1 className="text-xl sm:text-2xl font-semibold text-gray-900">Kategori</h1>
        <button className={btnPrimary} onClick={() => setEditing({ name: '', slug: '', sortOrder: '0' })}>
          + Tambah kategori
        </button>
      </div>
      <ErrorBox error={error} />
      {/* API kategori tidak berhalaman (semua kategori sekali ambil), jadi tanpa gulir bertahap.
          HP (< md): kartu per kategori, ketuk kartu = Ubah; md ke atas: tabel. */}
      <ul className="mt-3 space-y-2 md:hidden" data-testid="category-cards">
        {loading && items.length === 0 && <li className="rounded-lg border border-gray-200 bg-white p-6 text-center text-sm text-gray-500">Memuat...</li>}
        {!loading && items.length === 0 && (
          <li className="rounded-lg border border-gray-200 bg-white p-6 text-center text-sm text-gray-500">Belum ada kategori.</li>
        )}
        {items.map((c) => (
          <li
            key={c.id}
            data-testid="category-card"
            onClick={() => editCategory(c)}
            className="cursor-pointer rounded-lg border border-gray-200 bg-white p-3 text-sm shadow-sm active:bg-purple-50"
          >
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
                <div className="font-medium text-gray-800">{c.name}</div>
                <div className="font-mono text-xs text-gray-500">{c.slug}</div>
              </div>
              <span className="shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600">Urutan {c.sortOrder}</span>
            </div>
            <div className="mt-2 flex items-center justify-between gap-2 border-t border-gray-100 pt-2">
              <span className="text-xs text-gray-600">
                Produk aktif / total: {c.activeProducts} / {c.totalProducts}
              </span>
              <div className="flex gap-4">
                <button
                  type="button"
                  className="py-1 font-medium text-purple-700"
                  onClick={(e) => {
                    e.stopPropagation();
                    editCategory(c);
                  }}
                >
                  Ubah
                </button>
                <button
                  type="button"
                  className="py-1 font-medium text-red-600"
                  onClick={(e) => {
                    e.stopPropagation();
                    setConfirmDelete(c);
                  }}
                >
                  Hapus
                </button>
              </div>
            </div>
          </li>
        ))}
      </ul>
      <div className="mt-3 hidden overflow-x-auto bg-white rounded-lg border border-gray-200 shadow-sm md:block">
        <table className="min-w-full text-sm">
          <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
            <tr>
              <th className="px-3 py-2">Urutan</th>
              <th className="px-3 py-2">Nama</th>
              <th className="px-3 py-2">Slug</th>
              <th className="px-3 py-2 text-right">Produk aktif / total</th>
              <th className="px-3 py-2 text-right">Aksi</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-gray-500">
                  Memuat...
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-gray-500">
                  Belum ada kategori.
                </td>
              </tr>
            ) : (
              items.map((c) => (
                <tr key={c.id} className="border-t border-gray-100 hover:bg-gray-50">
                  <td className="px-3 py-2 text-gray-500">{c.sortOrder}</td>
                  <td className="px-3 py-2 font-medium text-gray-800">{c.name}</td>
                  <td className="px-3 py-2 font-mono text-xs">{c.slug}</td>
                  <td className="px-3 py-2 text-right">
                    {c.activeProducts} / {c.totalProducts}
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    <button className="text-purple-700 hover:underline mr-3" onClick={() => editCategory(c)}>
                      Ubah
                    </button>
                    <button className="text-red-600 hover:underline" onClick={() => setConfirmDelete(c)}>
                      Hapus
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {editing && (
        <Modal title={editing.id ? 'Ubah kategori' : 'Tambah kategori'} onClose={() => setEditing(null)}>
          <CategoryForm
            initial={editing}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              load();
            }}
          />
        </Modal>
      )}
      {confirmDelete && (
        <Modal title="Hapus kategori?" onClose={() => setConfirmDelete(null)}>
          <p className="text-gray-700">
            Kategori <strong>{confirmDelete.name}</strong> akan dihapus. Kategori yang masih memiliki produk aktif tidak
            bisa dihapus.
          </p>
          <div className="flex justify-end gap-2 mt-6">
            <button className={btnSecondary} onClick={() => setConfirmDelete(null)}>
              Batal
            </button>
            <button className={btnDanger} onClick={doDelete} disabled={busy}>
              Ya, hapus
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

export default Categories;
