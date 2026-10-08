import React, { useEffect, useRef, useState } from 'react';
import BottomSheet from '../components/BottomSheet';
import { prepareImage, prepErrorMessage, ACCEPTED_TYPES } from '../admin/imagePrep';
import { deletePaymentProof, getPaymentProofBlob, uploadPaymentProof, isUnauthorized } from './api';
import { buildPaymentConfirmText, fmtDateTime, waLink } from './format';
import { errorMessage } from '../auth';

// Bukti transfer pelanggan (status menunggu pembayaran): tombol "Konfirmasi pembayaran" -> sheet "Upload bukti
// transfer" (pilih foto, pratinjau, kirim; ganti/hapus dengan konfirmasi) -> sheet "Bukti terkirim" (ajakan kirim
// pesan WhatsApp). Kartu "Bukti pembayaran terkirim" di halaman pesanan (lihat; ganti/hapus hanya saat menunggu
// pembayaran). Berkas bukti PRIVAT: gambar diambil lewat permintaan berotorisasi -> blob URL (dibebaskan saat
// tidak dipakai), tidak pernah lewat URL publik.

export const PROOF_MAX_SIDE = 2000;
export const PROOF_MAX_BYTES = 5 * 1024 * 1024;
// iPhone umumnya mengubah HEIC ke JPEG saat dipilih lewat <input accept="image/*">; bila tetap HEIC, dicoba
// didekode browser (Safari baru bisa) lalu dijadikan JPEG. Tipe kosong (sebagian Android) juga dicoba.
export const PROOF_ACCEPT_TYPES = [...ACCEPTED_TYPES, 'image/heic', 'image/heif', ''];
export const MSG_PROOF_TOO_BIG = 'Foto masih lebih dari 5 MB setelah diperkecil. Coba foto lain.';
export const MSG_PROOF_DECODE = 'Foto tidak bisa dibaca. Coba foto lain (di iPhone: pilih dari galeri, atau pakai format kamera "Paling Kompatibel").';

const btnPrimary =
  'inline-flex w-full items-center justify-center rounded-lg bg-purple-700 px-4 py-3 text-sm font-semibold text-white hover:bg-purple-800 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500';
const btnSecondary =
  'inline-flex w-full items-center justify-center rounded-lg border border-gray-300 bg-white px-4 py-2.5 text-sm font-semibold text-gray-800 hover:bg-gray-50 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500';
const btnDanger =
  'inline-flex w-full items-center justify-center rounded-lg border border-red-300 bg-white px-4 py-2.5 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-400';

const CloseX = ({ onClick }) => (
  <button
    type="button"
    onClick={onClick}
    aria-label="Tutup"
    className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
  >
    <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
      <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
    </svg>
  </button>
);

// Gambar bukti sebagai blob URL (permintaan berotorisasi); dibebaskan saat komponen dilepas / versi berganti.
export const useProofImage = (orderNo, version, enabled = true) => {
  const [state, setState] = useState({ url: '', error: false });
  useEffect(() => {
    if (!enabled || !version) {
      setState({ url: '', error: false });
      return undefined;
    }
    let alive = true;
    let url = '';
    getPaymentProofBlob(orderNo)
      .then((blob) => {
        if (!alive) return;
        url = URL.createObjectURL(blob);
        setState({ url, error: false });
      })
      .catch(() => alive && setState({ url: '', error: true }));
    return () => {
      alive = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [orderNo, version, enabled]);
  return state;
};

const ProofThumb = ({ url, error, className = 'h-20 w-20', alt = 'Bukti transfer' }) => {
  if (url) return <img src={url} alt={alt} className={`${className} rounded-lg border border-gray-200 object-cover`} data-testid="proof-thumb" />;
  return (
    <span className={`${className} flex items-center justify-center rounded-lg border border-dashed border-gray-300 text-[11px] text-gray-400`}>
      {error ? 'Gagal memuat' : 'Memuat...'}
    </span>
  );
};

// ---------- Sheet unggah / kelola ----------
const UploadSheet = ({ order, proofUrl, proofError, onClose, onUploaded, onDeleted, onUnauthorized }) => {
  const hasProof = !!order.paymentProof;
  const [mode, setMode] = useState(hasProof ? 'current' : 'pick'); // current | pick | confirm-replace | confirm-delete
  const [prepared, setPrepared] = useState(null); // { blob, url, width, height }
  const [preparing, setPreparing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const busyRef = useRef(false);
  const inputRef = useRef(null);

  useEffect(
    () => () => {
      if (prepared?.url) URL.revokeObjectURL(prepared.url);
    },
    [prepared]
  );

  const pick = async (e) => {
    const file = e.target.files && e.target.files[0];
    e.target.value = '';
    if (!file) return;
    setError('');
    if (file.type && !file.type.startsWith('image/')) {
      setError('Pilih berkas foto (gambar).');
      return;
    }
    setPreparing(true);
    try {
      const out = await prepareImage(file, {
        maxSide: PROOF_MAX_SIDE,
        maxBytes: PROOF_MAX_BYTES,
        acceptTypes: PROOF_ACCEPT_TYPES,
        tooBigMessage: MSG_PROOF_TOO_BIG,
      });
      setPrepared({ blob: out.blob, url: URL.createObjectURL(out.blob), width: out.width, height: out.height });
    } catch (err) {
      const msg = prepErrorMessage(err);
      setError(msg.startsWith('Foto tidak bisa dibaca') ? MSG_PROOF_DECODE : msg);
    } finally {
      setPreparing(false);
    }
  };

  const send = async () => {
    if (!prepared || busyRef.current) return; // cegah klik ganda
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const res = await uploadPaymentProof(order.orderNo, prepared.blob);
      onUploaded(res);
    } catch (err) {
      if (isUnauthorized(err)) onUnauthorized?.();
      else setError(errorMessage(err, 'Gagal mengirim bukti. Coba lagi.'));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const remove = async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      await deletePaymentProof(order.orderNo);
      onDeleted();
    } catch (err) {
      if (isUnauthorized(err)) onUnauthorized?.();
      else setError(errorMessage(err, 'Gagal menghapus bukti. Coba lagi.'));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const close = () => {
    if (!busyRef.current) onClose();
  };

  return (
    <BottomSheet open onClose={close} labelledBy="proof-sheet-title" testId="proof-sheet">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 id="proof-sheet-title" className="text-lg font-bold text-gray-900">
            Upload bukti transfer
          </h2>
          <p className="text-sm text-gray-500">Pesanan {order.orderNo}</p>
        </div>
        <CloseX onClick={close} />
      </div>

      {mode === 'current' && (
        <div className="mt-4 space-y-3" data-testid="proof-current">
          <div className="flex items-center gap-3">
            <ProofThumb url={proofUrl} error={proofError} />
            <div className="text-sm text-gray-700">
              <div className="font-semibold">Bukti saat ini</div>
              <div className="text-gray-500">Dikirim {fmtDateTime(order.paymentProof?.uploadedAt)}</div>
            </div>
          </div>
          <button type="button" className={btnPrimary} onClick={() => setMode('confirm-replace')} disabled={busy}>
            Ganti bukti
          </button>
          <button type="button" className={btnDanger} onClick={() => setMode('confirm-delete')} disabled={busy}>
            Hapus bukti
          </button>
          <button type="button" className={btnSecondary} onClick={close} disabled={busy}>
            Tutup
          </button>
        </div>
      )}

      {mode === 'confirm-replace' && (
        <div className="mt-4 space-y-3 rounded-xl bg-amber-50 p-4 text-sm text-amber-900" data-testid="proof-confirm-replace">
          <p className="font-semibold">Ganti bukti?</p>
          <p>Bukti lama akan dihapus dan diganti dengan foto baru.</p>
          <button type="button" className={btnPrimary} onClick={() => setMode('pick')}>
            Ya, pilih foto baru
          </button>
          <button type="button" className={btnSecondary} onClick={() => setMode('current')}>
            Batal
          </button>
        </div>
      )}

      {mode === 'confirm-delete' && (
        <div className="mt-4 space-y-3 rounded-xl bg-red-50 p-4 text-sm text-red-900" data-testid="proof-confirm-delete">
          <p className="font-semibold">Hapus bukti ini?</p>
          <p>Bukti akan dihapus permanen. Anda bisa mengunggah bukti lain selama pesanan menunggu pembayaran.</p>
          <button type="button" className={btnDanger} onClick={remove} disabled={busy}>
            {busy ? 'Menghapus...' : 'Ya, hapus bukti'}
          </button>
          <button type="button" className={btnSecondary} onClick={() => setMode('current')} disabled={busy}>
            Batal
          </button>
        </div>
      )}

      {mode === 'pick' && (
        <div className="mt-4 space-y-3">
          <label className="block">
            <span className="mb-1 block text-sm font-medium text-gray-700">Foto bukti transfer</span>
            {/* accept="image/*" tanpa capture: HP menawarkan kamera atau galeri. */}
            <input
              ref={inputRef}
              type="file"
              accept="image/*"
              onChange={pick}
              disabled={busy || preparing}
              className="block w-full text-base text-gray-700 file:mr-3 file:rounded-lg file:border-0 file:bg-purple-100 file:px-4 file:py-2.5 file:text-sm file:font-semibold file:text-purple-800"
              data-testid="proof-input"
            />
          </label>
          <p className="text-xs text-gray-500">Foto atau tangkapan layar bukti transfer. Foto diperkecil otomatis sebelum dikirim.</p>
          {preparing && <p className="text-sm text-gray-600">Menyiapkan foto...</p>}
          {prepared && (
            <div className="rounded-xl border border-gray-200 p-2" data-testid="proof-preview">
              <img src={prepared.url} alt="Pratinjau bukti transfer" className="mx-auto max-h-64 rounded-lg object-contain" />
            </div>
          )}
          {error && (
            <p role="alert" className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700" data-testid="proof-error">
              {error}
            </p>
          )}
          <button type="button" className={btnPrimary} onClick={send} disabled={!prepared || busy || preparing}>
            {busy ? 'Mengirim...' : 'Kirim bukti'}
          </button>
          <button type="button" className={btnSecondary} onClick={close} disabled={busy}>
            Batal
          </button>
        </div>
      )}
      {mode !== 'pick' && error && (
        <p role="alert" className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700" data-testid="proof-error">
          {error}
        </p>
      )}
    </BottomSheet>
  );
};

// ---------- Sheet "Bukti terkirim" ----------
const SentSheet = ({ order, info, accountName, onClose }) => {
  const wa = info?.storeWhatsapp ? waLink(info.storeWhatsapp, buildPaymentConfirmText(order, accountName)) : '';
  return (
    <BottomSheet open onClose={onClose} labelledBy="proof-sent-title" testId="proof-sent">
      <div className="flex items-start justify-between gap-3">
        <h2 id="proof-sent-title" className="text-lg font-bold text-gray-900">
          Bukti terkirim
        </h2>
        <CloseX onClick={onClose} />
      </div>
      <p className="mt-2 text-sm text-gray-700">Bukti transfer sudah kami terima. Kirim juga pesan ke WhatsApp admin?</p>
      <p className="mt-1 text-xs text-gray-500">Foto tidak ikut terkirim lewat WhatsApp; admin melihat bukti di sistem toko.</p>
      <div className="mt-4 space-y-2">
        {wa ? (
          <a href={wa} target="_blank" rel="noopener noreferrer" className={`${btnPrimary} bg-green-600 hover:bg-green-700`} onClick={onClose}>
            Kirim ke WhatsApp
          </a>
        ) : (
          <p className="rounded-lg bg-gray-50 px-3 py-2 text-sm text-gray-600" data-testid="proof-no-wa">
            Nomor WhatsApp toko belum tersedia. Admin tetap menerima bukti Anda dan akan memeriksanya.
          </p>
        )}
        <button type="button" className={btnSecondary} onClick={onClose}>
          Nanti saja
        </button>
      </div>
    </BottomSheet>
  );
};

// ---------- Viewer ----------
const ViewerSheet = ({ url, error, onClose }) => (
  <BottomSheet open onClose={onClose} labelledBy="proof-view-title" testId="proof-viewer">
    <div className="flex items-start justify-between gap-3">
      <h2 id="proof-view-title" className="text-lg font-bold text-gray-900">
        Bukti pembayaran
      </h2>
      <CloseX onClick={onClose} />
    </div>
    <div className="mt-3">
      <ProofThumb url={url} error={error} className="w-full max-h-[70vh] min-h-[8rem] object-contain" />
    </div>
  </BottomSheet>
);

// ---------- Bagian di halaman pesanan ----------
// order: detail pesanan (paymentProof, canUploadProof); info: store-info; onChange(): muat ulang detail.
export const PaymentProofButton = ({ onOpen }) => (
  <button
    type="button"
    className="flex-1 text-center bg-purple-700 text-white py-3 rounded-lg font-semibold hover:bg-purple-800"
    onClick={onOpen}
    data-testid="proof-open"
  >
    Konfirmasi pembayaran
  </button>
);

export const usePaymentProofFlow = ({ order, info, accountName, onChange, onUnauthorized }) => {
  const [sheet, setSheet] = useState(null); // null | 'upload' | 'sent' | 'view'
  const version = order?.paymentProof?.uploadedAt || '';
  const { url, error } = useProofImage(order?.orderNo, version, !!order?.paymentProof);
  const open = () => setSheet('upload');
  const card =
    order?.paymentProof ? (
      <div className="bg-white rounded-xl shadow p-4 text-sm" data-testid="proof-card">
        <h2 className="font-semibold text-gray-800">Bukti pembayaran terkirim</h2>
        <div className="mt-2 flex items-center gap-3">
          <button type="button" onClick={() => setSheet('view')} aria-label="Perbesar bukti pembayaran" className="shrink-0">
            <ProofThumb url={url} error={error} className="h-16 w-16" />
          </button>
          <div className="min-w-0 flex-1 text-gray-600">
            <div>Dikirim {fmtDateTime(order.paymentProof.uploadedAt)}</div>
            <div className="text-xs text-gray-500">{order.canUploadProof ? 'Admin akan memeriksa bukti Anda.' : 'Bukti terkunci.'}</div>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          <button type="button" className="rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium" onClick={() => setSheet('view')}>
            Lihat
          </button>
          {order.canUploadProof && (
            <button type="button" className="rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium" onClick={open} data-testid="proof-manage">
              Ganti/Hapus
            </button>
          )}
        </div>
      </div>
    ) : null;
  const sheets = (
    <>
      {sheet === 'upload' && (
        <UploadSheet
          order={order}
          proofUrl={url}
          proofError={error}
          onClose={() => setSheet(null)}
          onUnauthorized={onUnauthorized}
          onUploaded={async () => {
            await onChange?.();
            setSheet('sent');
          }}
          onDeleted={async () => {
            await onChange?.();
            setSheet(null);
          }}
        />
      )}
      {sheet === 'sent' && <SentSheet order={order} info={info} accountName={accountName} onClose={() => setSheet(null)} />}
      {sheet === 'view' && <ViewerSheet url={url} error={error} onClose={() => setSheet(null)} />}
    </>
  );
  return { open, card, sheets };
};
