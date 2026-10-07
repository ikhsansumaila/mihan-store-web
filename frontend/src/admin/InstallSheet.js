import React from 'react';
import BottomSheet from '../components/BottomSheet';

// Sheet "Pasang Mihan Store": langkah manual memasang aplikasi ke layar utama.
// platform: 'ios' -> langkah bernomor Safari (catatan "buka lewat Safari" bila browser lain di iOS) + "Tutup".
// platform: 'android' + canPrompt (beforeinstallprompt tersedia) -> tombol "Pasang Sekarang" (prompt native) + "Nanti".
// platform: 'android' tanpa prompt (sudah ditolak / browser lain) -> langkah manual menu Chrome + "Tutup".

const iconCls = 'mx-0.5 inline h-[1.15em] w-[1.15em] align-[-0.2em]';

// Ikon "Bagikan" Safari (kotak dengan panah ke atas).
export const ShareIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" data-icon="share" className={`${iconCls} text-sky-600`} fill="none" stroke="currentColor" strokeWidth="1.8">
    <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v12m0-12L8 7m4-4l4 4M7 11H5.5A1.5 1.5 0 004 12.5v7A1.5 1.5 0 005.5 21h13a1.5 1.5 0 001.5-1.5v-7a1.5 1.5 0 00-1.5-1.5H17" />
  </svg>
);

// Ikon plus dalam kotak ("Tambah ke Layar Utama").
export const AddBoxIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" data-icon="add" className={`${iconCls} text-gray-700`} fill="none" stroke="currentColor" strokeWidth="1.8">
    <rect x="4" y="4" width="16" height="16" rx="3" />
    <path strokeLinecap="round" d="M12 8v8M8 12h8" />
  </svg>
);

const DownloadIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8">
    <path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3" />
  </svg>
);

// Ikon menu titik tiga vertikal Chrome.
export const KebabIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" data-icon="menu" className={`${iconCls} text-gray-700`} fill="currentColor">
    <circle cx="12" cy="5" r="1.8" />
    <circle cx="12" cy="12" r="1.8" />
    <circle cx="12" cy="19" r="1.8" />
  </svg>
);

const Step = ({ n, children }) => (
  <li className="flex items-start gap-3">
    <span aria-hidden="true" className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-purple-700 text-xs font-bold text-white">
      {n}
    </span>
    <span className="min-w-0 pt-0.5 text-sm leading-relaxed text-gray-800">{children}</span>
  </li>
);

const IOS_STEPS = [
  <>
    Ketuk ikon <strong>Bagikan</strong> <ShareIcon /> di bilah Safari.
  </>,
  <>
    Gulir dan pilih <strong>Tambah ke Layar Utama</strong> <AddBoxIcon />.
  </>,
  <>
    Ketuk <strong>Tambah</strong> untuk konfirmasi, lalu buka <strong>Mihan Store</strong> dari Layar Utama.
  </>,
];

const ANDROID_STEPS = [
  <>
    Ketuk menu <strong>⋮</strong> <KebabIcon /> di kanan atas Chrome.
  </>,
  <>
    Pilih <strong>Pasang aplikasi</strong> atau <strong>Tambahkan ke layar utama</strong>.
  </>,
  <>
    Ketuk <strong>Pasang</strong> / <strong>Tambahkan</strong> untuk konfirmasi.
  </>,
];

const InstallSheet = ({ open, onClose, platform, otherBrowser = false, canPrompt = false, onInstall, installing = false, onDismissForever, testId = 'install-sheet' }) => {
  const steps = platform === 'ios' ? IOS_STEPS : ANDROID_STEPS;
  const promptMode = platform !== 'ios' && canPrompt;
  return (
    <BottomSheet open={open} onClose={onClose} labelledBy="install-sheet-title" testId={testId}>
      <div className="flex items-start gap-3">
        <span className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-amber-300 shadow-sm">
          <img src="/icon-192.png" alt="" className="h-12 w-12" />
        </span>
        <div className="min-w-0 flex-1">
          <h2 id="install-sheet-title" className="text-lg font-bold text-gray-900">
            Pasang Mihan Store
          </h2>
          <p className="text-sm text-gray-500">Akses lebih cepat langsung dari layar utama</p>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Tutup"
          className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>

      {platform === 'ios' && otherBrowser && (
        <p className="mt-4 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900" data-testid="install-open-safari">
          Buka halaman ini lewat <strong>Safari</strong> terlebih dahulu, lalu ikuti langkah di bawah.
        </p>
      )}

      {promptMode ? (
        <>
          <p className="mt-4 text-sm text-gray-600" data-testid="install-benefit">
            Buka toko dan panel admin lebih cepat dari layar utama, dan terima notifikasi pesanan.
          </p>
          <button
            type="button"
            onClick={onInstall}
            disabled={installing}
            className="mt-4 flex w-full items-center justify-center gap-2 rounded-lg bg-purple-700 px-4 py-3 text-sm font-semibold text-white shadow-sm hover:bg-purple-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 focus-visible:ring-offset-1 disabled:opacity-60"
          >
            <DownloadIcon />
            Pasang Sekarang
          </button>
        </>
      ) : (
        <div className="mt-4 rounded-xl bg-purple-50 p-4" data-testid="install-steps" data-platform={platform}>
          <ol className="space-y-3">
            {steps.map((s, i) => (
              <Step key={`${platform}-${i}`} n={i + 1}>
                {s}
              </Step>
            ))}
          </ol>
        </div>
      )}

      {platform === 'ios' && (
        <p className="mt-3 text-xs text-gray-600">
          Notifikasi di iPhone/iPad hanya bisa diaktifkan dari aplikasi yang dibuka dari <strong>Layar Utama</strong> (iOS 16.4 atau lebih baru).
        </p>
      )}

      <button
        type="button"
        onClick={onClose}
        className={`${promptMode ? 'mt-2' : 'mt-4'} w-full rounded-lg border border-gray-300 bg-white px-4 py-2.5 text-sm font-semibold text-gray-800 hover:bg-gray-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500`}
      >
        {promptMode ? 'Nanti' : 'Tutup'}
      </button>
      {onDismissForever && (
        <button type="button" onClick={onDismissForever} className="mx-auto mt-2 block text-xs font-medium text-gray-500 underline hover:text-gray-700">
          Jangan tampilkan lagi
        </button>
      )}
    </BottomSheet>
  );
};

export default InstallSheet;
