import React from 'react';

// Gaya bersama area admin (layout cPanel): kartu putih bergaris tipis, tombol & input seragam.
export const cardClass = 'bg-white rounded-lg border border-gray-200 shadow-sm';
export const inputClass =
  'w-full px-3 py-2 bg-white border border-gray-300 rounded-md text-sm text-gray-800 shadow-sm focus:outline-none focus:border-purple-600 focus:ring-2 focus:ring-purple-200 transition';
const btnBase =
  'inline-flex items-center justify-center gap-1.5 px-4 py-2 rounded-md text-sm transition focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-1 disabled:opacity-60 disabled:cursor-not-allowed';
export const btnPrimary = `${btnBase} bg-purple-700 text-white font-semibold shadow-sm hover:bg-purple-800 focus-visible:ring-purple-500`;
export const btnSecondary = `${btnBase} bg-white border border-gray-300 text-gray-700 font-medium shadow-sm hover:bg-gray-50 focus-visible:ring-purple-500`;
export const btnDanger = `${btnBase} bg-red-600 text-white font-semibold shadow-sm hover:bg-red-700 focus-visible:ring-red-500`;

export const SessionExpired = () => (
  <div className="rounded-lg border border-yellow-300 bg-yellow-50 px-4 py-4 text-yellow-900">
    <p className="font-semibold">Sesi admin berakhir, muat ulang halaman</p>
    <p className="text-sm mt-1">
      <a href={window.location.pathname} className="underline font-medium">
        Muat ulang halaman
      </a>{' '}
      untuk masuk kembali lewat Cloudflare Access.
    </p>
  </div>
);

export const ErrorBox = ({ error }) => {
  if (!error) return null;
  if (error.sessionExpired) return <SessionExpired />;
  return (
    <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
      {error.message || String(error)}
    </div>
  );
};

export const Modal = ({ title, onClose, children, wide = false }) => (
  <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-black/40 p-0 sm:p-4" role="dialog" aria-modal="true">
    <div className={`w-full ${wide ? 'sm:max-w-2xl' : 'sm:max-w-lg'} max-h-[95vh] overflow-y-auto bg-white rounded-t-xl sm:rounded-lg shadow-xl`}>
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-4">
        <h3 className="text-lg font-semibold text-gray-800">{title}</h3>
        <button onClick={onClose} className="text-gray-500 hover:text-gray-800 text-2xl leading-none" aria-label="Tutup">
          ×
        </button>
      </div>
      <div className="px-5 py-4">{children}</div>
    </div>
  </div>
);

export const Pagination = ({ page, perPage, total, onPage }) => {
  const pages = Math.max(1, Math.ceil((total || 0) / (perPage || 1)));
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 mt-4 text-sm text-gray-600">
      <span>
        Total {total ?? 0} data · halaman {page} dari {pages}
      </span>
      <div className="flex gap-2">
        <button className={btnSecondary} disabled={page <= 1} onClick={() => onPage(page - 1)}>
          ‹ Sebelumnya
        </button>
        <button className={btnSecondary} disabled={page >= pages} onClick={() => onPage(page + 1)}>
          Berikutnya ›
        </button>
      </div>
    </div>
  );
};
