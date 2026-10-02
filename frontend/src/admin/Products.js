import React, { useCallback, useEffect, useState } from 'react';
import { adminFetch, qs, rupiah } from './api';
import { ErrorBox, Modal, Pagination, inputClass, btnPrimary, btnSecondary, btnDanger } from './ui';

const emptyForm = { name: '', categoryId: '', price: '', description: '', imagePath: '', isActive: true };

const ProductForm = ({ initial, categories, onCancel, onSaved }) => {
  const [form, setForm] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value });

  const submit = async (e) => {
    e.preventDefault();
    setError(null);
    const price = Number(String(form.price).replace(/[^0-9]/g, ''));
    if (!form.name.trim()) return setError(new Error('Nama produk wajib diisi'));
    if (!form.categoryId) return setError(new Error('Kategori wajib dipilih'));
    if (String(form.price).trim() === '' || !Number.isInteger(price) || price > 1000000000)
      return setError(new Error('Harga harus bilangan bulat 0 sampai 1.000.000.000'));
    const body = {
      name: form.name.trim(),
      categoryId: Number(form.categoryId),
      price,
      description: form.description,
      imagePath: form.imagePath.trim(),
      isActive: !!form.isActive,
    };
    setSaving(true);
    try {
      if (initial.id) await adminFetch(`/products/${initial.id}`, { method: 'PUT', body });
      else await adminFetch('/products', { method: 'POST', body });
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
        <div>
          <label className="block text-sm font-semibold text-gray-700 mb-1">Harga (Rp)</label>
          <input className={inputClass} inputMode="numeric" value={form.price} onChange={set('price')} placeholder="15000" required />
        </div>
      </div>
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
        <h1 className="text-2xl font-bold text-gray-800">Produk</h1>
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

      <div className="mt-3 overflow-x-auto bg-white rounded-xl shadow">
        <table className="min-w-full text-sm">
          <thead className="bg-gray-50 text-left text-gray-600">
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
                <tr key={p.id} className="border-t">
                  <td className="px-3 py-2 text-gray-500">{p.id}</td>
                  <td className="px-3 py-2">
                    <div className="font-medium text-gray-800">{p.name}</div>
                    {p.description && <div className="text-xs text-gray-500 line-clamp-1 max-w-xs">{p.description}</div>}
                  </td>
                  <td className="px-3 py-2">
                    {p.categoryName}
                    {p.categoryDeleted && <span className="ml-1 text-xs text-red-600">(dihapus)</span>}
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">{rupiah(p.price)}</td>
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
        <Modal title={editing.id ? `Ubah produk #${editing.id}` : 'Tambah produk'} onClose={() => setEditing(null)}>
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
