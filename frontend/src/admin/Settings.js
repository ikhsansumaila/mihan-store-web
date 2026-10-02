import React, { useEffect, useState } from 'react';
import { adminFetch } from './api';
import { ErrorBox, inputClass, btnPrimary } from './ui';

const FIELDS = [
  { key: 'store_whatsapp', label: 'Nomor WhatsApp toko', hint: 'Format 08xx / 628xx / +628xx. Dipakai tombol "Konfirmasi via WhatsApp".', max: 20, inputMode: 'tel' },
  { key: 'bank_name', label: 'Nama bank', hint: 'Mis. BCA', max: 100 },
  { key: 'bank_account_number', label: 'Nomor rekening', hint: 'Angka (boleh spasi/tanda hubung)', max: 40, inputMode: 'numeric' },
  { key: 'bank_account_holder', label: 'Atas nama', max: 100 },
  { key: 'payment_note', label: 'Catatan pembayaran', hint: 'Ditampilkan di halaman pesanan pelanggan (opsional).', max: 500, multiline: true },
];

const Settings = () => {
  const [form, setForm] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    adminFetch('/settings')
      .then((r) => setForm(r.settings || {}))
      .catch(setError);
  }, []);

  const save = async (e) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const body = {};
      FIELDS.forEach((f) => {
        body[f.key] = (form[f.key] || '').trim();
      });
      const r = await adminFetch('/settings', { method: 'PUT', body });
      setForm(r.settings || {});
      setSaved(true);
    } catch (err) {
      setError(err);
    } finally {
      setSaving(false);
    }
  };

  if (!form) return error ? <ErrorBox error={error} /> : <p className="text-gray-500">Memuat...</p>;
  const missing = FIELDS.filter((f) => f.key !== 'payment_note' && !form[f.key]);

  return (
    <div className="max-w-2xl">
      <h1 className="text-xl sm:text-2xl font-semibold text-gray-900 mb-4">Pengaturan Toko</h1>
      {missing.length > 0 && (
        <div className="mb-4 rounded-lg border border-yellow-300 bg-yellow-50 px-4 py-3 text-sm text-yellow-900">
          Belum diisi: {missing.map((f) => f.label).join(', ')}. Pelanggan akan melihat peringatan sampai info ini diisi.
        </div>
      )}
      <form onSubmit={save} className="bg-white rounded-lg border border-gray-200 shadow-sm p-4 sm:p-6 space-y-4">
        <ErrorBox error={error} />
        {FIELDS.map((f) => (
          <label key={f.key} className="block">
            <span className="block text-sm font-semibold text-gray-700 mb-1">{f.label}</span>
            {f.multiline ? (
              <textarea
                className={inputClass}
                rows={3}
                maxLength={f.max}
                value={form[f.key] || ''}
                onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
              />
            ) : (
              <input
                className={inputClass}
                maxLength={f.max}
                inputMode={f.inputMode}
                value={form[f.key] || ''}
                onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                placeholder="BELUM DIISI"
              />
            )}
            {f.hint && <span className="block text-xs text-gray-500 mt-1">{f.hint}</span>}
          </label>
        ))}
        <div className="flex items-center justify-end gap-3">
          {saved && <span className="text-sm text-green-700">Tersimpan</span>}
          <button type="submit" className={btnPrimary} disabled={saving}>
            {saving ? 'Menyimpan...' : 'Simpan pengaturan'}
          </button>
        </div>
      </form>
    </div>
  );
};

export default Settings;
