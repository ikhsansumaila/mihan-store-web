import React, { useState } from 'react';
import { adminFetch } from './api';
import { ErrorBox, Modal, inputClass, btnPrimary, btnSecondary } from './ui';
import { ALIAS_MAX, aliasLength, normalizeAlias } from './alias';

// Modal kecil ubah alias. customer: {id, name, alias}; onSaved(aliasBaru|null).
export const AliasEditModal = ({ customer, onClose, onSaved }) => {
  const [value, setValue] = useState(customer.alias || '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const n = aliasLength(value);
  const tooLong = n > ALIAS_MAX;

  const submit = async (e) => {
    e.preventDefault();
    if (tooLong) return;
    setSaving(true);
    setError(null);
    try {
      const r = await adminFetch(`/customers/${customer.id}/alias`, { method: 'PATCH', body: { alias: normalizeAlias(value) } });
      onSaved(r?.alias ?? null);
    } catch (err) {
      setError(err);
      setSaving(false);
    }
  };

  return (
    <Modal title={customer.alias ? 'Ubah alias pelanggan' : 'Beri alias pelanggan'} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3" data-testid="alias-form">
        <ErrorBox error={error} />
        <p className="text-sm text-gray-600">
          Akun: <strong className="text-gray-800">{customer.name}</strong>
          {customer.username ? <span className="text-gray-500"> (@{customer.username})</span> : null}
        </p>
        <label className="block">
          <span className="mb-1 block text-xs font-semibold text-gray-600">Alias (nama panggilan internal)</span>
          <input
            className={inputClass}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="mis. Bu Siti Toko Maju"
            aria-invalid={tooLong ? 'true' : undefined}
            aria-describedby="alias-help"
            autoFocus
          />
        </label>
        <div id="alias-help" className="flex flex-wrap items-start justify-between gap-2 text-xs">
          <span className="text-gray-500">Hanya terlihat admin, tidak pernah ditampilkan ke pelanggan. Kosongkan untuk menghapus alias.</span>
          <span data-testid="alias-counter" className={tooLong ? 'font-semibold text-red-600' : 'text-gray-500'}>
            {n}/{ALIAS_MAX}
          </span>
        </div>
        {tooLong && <p className="text-sm text-red-600">Alias maksimal {ALIAS_MAX} karakter</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className={btnSecondary} onClick={onClose}>
            Batal
          </button>
          <button type="submit" className={btnPrimary} disabled={saving || tooLong}>
            {saving ? 'Menyimpan...' : 'Simpan alias'}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export const AliasText = ({ alias, className = '' }) =>
  alias ? (
    <span className={`font-semibold text-purple-800 ${className}`} data-testid="alias">
      {alias}
    </span>
  ) : (
    <span className={`text-xs italic text-gray-400 ${className}`}>Belum ada alias</span>
  );

