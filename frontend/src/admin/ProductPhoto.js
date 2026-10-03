import React, { useEffect, useRef, useState } from 'react';
import { adminFetch, adminUpload } from './api';
import { btnDanger, btnSecondary } from './ui';
import { ACCEPT_ATTR, prepareImage, prepErrorMessage } from './imagePrep';
import { safeImageUrl } from '../shop/productImage';

// Unggah foto produk lewat API admin. Mengembalikan {image, thumb}.
export const uploadProductPhoto = (productId, blob, onProgress) =>
  adminUpload(`/products/${productId}/image`, blob, { filename: 'foto.jpg', onProgress });

export const deleteProductPhoto = (productId) => adminFetch(`/products/${productId}/image`, { method: 'DELETE', body: {} });

// Bagian "Foto produk" di form tambah/ubah produk (terkendali oleh ProductForm):
// - current: foto tersimpan {image, thumb}; pending: foto baru yang dipilih (sudah diperkecil) {blob, url, width, height}
// - foto baru diunggah oleh form SETELAH produk tersimpan (produk baru: setelah id baru ada).
// - "Hapus foto" (dengan konfirmasi) langsung menghapus foto tersimpan di server.
const ProductPhotoField = ({ productId, name, current, pending, onPending, onDeleted, uploading, progress, disabled }) => {
  const inputRef = useRef(null);
  const [preparing, setPreparing] = useState(false);
  const [error, setError] = useState(null);
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Lepas object URL pratinjau saat diganti/ditutup.
  useEffect(() => () => pending?.url && URL.revokeObjectURL && URL.revokeObjectURL(pending.url), [pending]);

  const pick = async (e) => {
    const file = e.target.files && e.target.files[0];
    e.target.value = ''; // memilih berkas yang sama lagi tetap memicu onChange
    if (!file) return;
    setError(null);
    setConfirming(false);
    setPreparing(true);
    try {
      const out = await prepareImage(file);
      const url = URL.createObjectURL(out.blob);
      onPending({ ...out, url, originalSize: file.size });
    } catch (err) {
      setError(prepErrorMessage(err));
    } finally {
      setPreparing(false);
    }
  };

  const doDelete = async () => {
    setDeleting(true);
    setError(null);
    try {
      await deleteProductPhoto(productId);
      setConfirming(false);
      onDeleted();
    } catch (err) {
      setError(err.sessionExpired ? 'Sesi admin berakhir, muat ulang halaman.' : err.message || 'Gagal menghapus foto.');
    } finally {
      setDeleting(false);
    }
  };

  const currentThumb = safeImageUrl(current?.thumb) || safeImageUrl(current?.image);
  const shown = pending ? pending.url : currentThumb;
  const busy = preparing || uploading || deleting || disabled;
  const kb = (n) => `${Math.max(1, Math.round(n / 1024)).toLocaleString('id-ID')} KB`;

  return (
    <fieldset className="rounded-md border border-gray-200 p-3" data-testid="photo-field">
      <legend className="px-1 text-sm font-semibold text-gray-700">Foto produk</legend>
      <div className="flex flex-wrap items-start gap-3">
        <div className="relative h-28 w-28 shrink-0 overflow-hidden rounded-md border border-gray-200 bg-gray-100">
          {shown ? (
            <img
              src={shown}
              alt={pending ? `Pratinjau foto baru ${name || 'produk'}` : `Foto ${name || 'produk'}`}
              width={112}
              height={112}
              className="h-full w-full object-cover"
              data-testid={pending ? 'photo-preview-new' : 'photo-preview-current'}
            />
          ) : (
            <div className="flex h-full w-full flex-col items-center justify-center text-center text-xs text-gray-400" data-testid="photo-empty">
              <span className="text-3xl" aria-hidden="true">
                🖼️
              </span>
              Belum ada foto
            </div>
          )}
          {pending && (
            <span className="absolute left-1 top-1 rounded bg-purple-700 px-1.5 py-0.5 text-[11px] font-semibold text-white">Baru</span>
          )}
        </div>
        <div className="min-w-0 flex-1 space-y-2">
          <div className="flex flex-wrap gap-2">
            <button type="button" className={btnSecondary} onClick={() => inputRef.current?.click()} disabled={busy}>
              {preparing ? 'Menyiapkan foto…' : shown ? 'Ganti foto' : 'Pilih foto'}
            </button>
            {pending && (
              <button type="button" className={btnSecondary} onClick={() => onPending(null)} disabled={uploading}>
                Batalkan foto baru
              </button>
            )}
            {!pending && currentThumb && productId && !confirming && (
              <button type="button" className={`${btnSecondary} text-red-700`} onClick={() => setConfirming(true)} disabled={busy}>
                Hapus foto
              </button>
            )}
          </div>
          <input
            ref={inputRef}
            type="file"
            accept={ACCEPT_ATTR}
            className="sr-only"
            tabIndex={-1}
            aria-label="Pilih foto produk"
            onChange={pick}
            data-testid="photo-input"
          />
          {confirming && (
            <div role="alertdialog" aria-label="Konfirmasi hapus foto" className="rounded-md border border-red-200 bg-red-50 p-2 text-sm text-red-800">
              <p>Hapus foto produk ini? Foto langsung hilang dari toko.</p>
              <div className="mt-2 flex flex-wrap gap-2">
                <button type="button" className={btnDanger} onClick={doDelete} disabled={deleting}>
                  {deleting ? 'Menghapus…' : 'Ya, hapus foto'}
                </button>
                <button type="button" className={btnSecondary} onClick={() => setConfirming(false)} disabled={deleting}>
                  Batal
                </button>
              </div>
            </div>
          )}
          {pending && !uploading && (
            <p className="text-xs text-gray-600" data-testid="photo-pending-info">
              Foto baru {pending.width}×{pending.height} px, {kb(pending.blob.size)}
              {pending.originalSize ? ` (asli ${kb(pending.originalSize)})` : ''}. Diunggah saat Anda menekan Simpan.
            </p>
          )}
          {uploading && (
            <div aria-live="polite" data-testid="photo-uploading">
              <p className="text-xs font-medium text-purple-800">Mengunggah… {progress > 0 ? `${progress}%` : ''}</p>
              <div className="mt-1 h-2 w-full overflow-hidden rounded bg-gray-200" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}>
                <div className="h-full bg-purple-600 transition-all" style={{ width: `${Math.max(5, progress)}%` }} />
              </div>
            </div>
          )}
          {!pending && !uploading && (
            <p className="text-xs text-gray-500">JPG, PNG, atau WebP. Foto dari HP otomatis diperkecil sebelum diunggah (maks. 2 MB).</p>
          )}
          {error && (
            <p role="alert" className="text-sm text-red-700" data-testid="photo-error">
              {error}
            </p>
          )}
        </div>
      </div>
    </fieldset>
  );
};

export default ProductPhotoField;
