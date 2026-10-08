import React, { useEffect, useRef, useState } from 'react';
import BottomSheet from '../components/BottomSheet';
import { AUTO_SHEET_SESSION_KEY, autoInstallPlatform } from '../admin/AutoInstallSheet';
import { currentSubscription, isPushSupported, notificationPermission, sessionGet, sessionSet, storageGet, storageSet } from '../admin/push';
import { customerPushApi, enableCustomerPush, isCustomerFlagged } from './pushApi';

// Sheet "Aktifkan notifikasi" untuk PELANGGAN yang muncul OTOMATIS sekali per sesi tab setelah sesi
// pelanggan terverifikasi (dirender App.js hanya bila user + verified + bukan /login|/register|/lengkapi-profil).
//
// Syarat tampil (dicek ulang saat jeda 0,8 detik selesai):
//   - push didukung di perangkat ini (iPhone/iPad: hanya bila sudah dibuka dari Layar Utama)
//   - server push aktif, izin BELUM "denied", perangkat belum berlangganan sebagai pelanggan ini
//   - belum pernah memilih "Jangan tampilkan lagi" (kunci terpisah dari sheet pasang aplikasi)
//   - sheet pasang aplikasi TIDAK sedang perlu tampil (prioritas pemasangan; tidak pernah dua sheet sekaligus)
//     dan belum tampil di sesi tab ini; tidak ada dialog modal lain yang terbuka.

export const NOTIFY_SHEET_SESSION_KEY = 'mihan.notifySheet.autoShown';
export const NOTIFY_SHEET_DISMISS_KEY = 'mihan.notifySheet.dismissed';
const DEFAULT_DELAY_MS = 800;

const otherModalOpen = () => !!document.querySelector('[role="dialog"][aria-modal="true"]');

const BellIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" className="h-6 w-6" fill="none" stroke="currentColor" strokeWidth="1.8">
    <path
      strokeLinecap="round"
      strokeLinejoin="round"
      d="M14.857 17.082a23.848 23.848 0 005.454-1.31A8.967 8.967 0 0118 9.75V9A6 6 0 006 9v.75a8.967 8.967 0 01-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 01-5.714 0m5.714 0a3 3 0 11-5.714 0"
    />
  </svg>
);

// Pemeriksaan sinkron (tanpa jaringan): apakah sheet ini boleh dipertimbangkan sama sekali.
export const notifySheetBlocked = () =>
  !isPushSupported() ||
  notificationPermission() === 'denied' ||
  storageGet(NOTIFY_SHEET_DISMISS_KEY) === '1' ||
  sessionGet(NOTIFY_SHEET_SESSION_KEY) === '1' ||
  sessionGet(AUTO_SHEET_SESSION_KEY) === '1' || // sheet pasang sudah tampil di sesi ini
  autoInstallPlatform() !== null; // sheet pasang aplikasi yang harus tampil (prioritas)

const AutoNotifySheet = ({ user, delayMs = DEFAULT_DELAY_MS }) => {
  const [open, setOpen] = useState(false);
  const [config, setConfig] = useState(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(null);
  const [toast, setToast] = useState(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (notifySheetBlocked()) return undefined;
    let cancelled = false;
    const t = setTimeout(async () => {
      if (cancelled || notifySheetBlocked()) return;
      try {
        const [cfg, sub] = await Promise.all([customerPushApi.fetchConfig(), currentSubscription().catch(() => null)]);
        if (cancelled || !mounted.current || notifySheetBlocked()) return;
        if (!cfg?.enabled || !cfg.publicKey) return;
        if (sub && isCustomerFlagged(user)) return; // sudah aktif di perangkat ini
        if (otherModalOpen()) return; // jangan menimpa dialog lain; dicoba lagi pada pemuatan berikutnya
        sessionSet(NOTIFY_SHEET_SESSION_KEY, '1');
        setConfig(cfg);
        setOpen(true);
      } catch {
        /* server tidak terjangkau: tidak usah mengajak */
      }
    }, delayMs);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [delayMs, user]);

  useEffect(() => {
    if (!toast) return undefined;
    const t = setTimeout(() => setToast(null), 4000);
    return () => clearTimeout(t);
  }, [toast]);

  const enable = async () => {
    setMessage(null);
    setBusy(true);
    try {
      const r = await enableCustomerPush(user, config);
      if (r.ok) {
        setOpen(false);
        setToast('Notifikasi aktif. Anda akan menerima kabar status pesanan di perangkat ini.');
      } else if (r.disabled) {
        setMessage('Notifikasi belum tersedia saat ini. Coba lagi nanti.');
      } else if (r.permission === 'denied') {
        setMessage('Izin notifikasi ditolak. Anda bisa mengizinkannya nanti lewat pengaturan perangkat, lalu aktifkan dari menu Pengaturan (ikon gear).');
      } else {
        setMessage('Izin notifikasi belum diberikan. Ketuk "Aktifkan notifikasi" lagi bila ingin mencoba.');
      }
    } catch {
      setMessage('Gagal mengaktifkan notifikasi. Coba lagi, atau aktifkan nanti dari menu Pengaturan (ikon gear).');
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const later = () => setOpen(false);
  const dismissForever = () => {
    storageSet(NOTIFY_SHEET_DISMISS_KEY, '1');
    setOpen(false);
  };

  return (
    <>
      <BottomSheet open={open} onClose={later} labelledBy="notify-sheet-title" testId="auto-notify-sheet">
        <div className="flex items-start gap-3">
          <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-purple-100 text-purple-700 shadow-sm">
            <BellIcon />
          </span>
          <div className="min-w-0 flex-1">
            <h2 id="notify-sheet-title" className="text-lg font-bold text-gray-900">
              Aktifkan notifikasi
            </h2>
            <p className="text-sm text-gray-500">Dapat kabar saat ongkir dikonfirmasi, pembayaran diterima, dan pesanan selesai.</p>
          </div>
          <button
            type="button"
            onClick={later}
            aria-label="Tutup"
            className="-mr-1 -mt-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
        {message && (
          <p role="status" className="mt-4 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900" data-testid="notify-sheet-message">
            {message}
          </p>
        )}
        <button
          type="button"
          onClick={enable}
          disabled={busy}
          className="mt-4 flex w-full items-center justify-center gap-2 rounded-lg bg-purple-700 px-4 py-3 text-sm font-semibold text-white shadow-sm hover:bg-purple-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 focus-visible:ring-offset-1 disabled:opacity-60"
        >
          <BellIcon />
          Aktifkan notifikasi
        </button>
        <button
          type="button"
          onClick={later}
          className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-4 py-2.5 text-sm font-semibold text-gray-800 hover:bg-gray-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
        >
          Nanti
        </button>
        <button type="button" onClick={dismissForever} className="mx-auto mt-2 block text-xs font-medium text-gray-500 underline hover:text-gray-700">
          Jangan tampilkan lagi
        </button>
      </BottomSheet>
      {toast && (
        <div
          role="status"
          data-testid="notify-toast"
          className="fixed inset-x-4 bottom-4 z-50 mx-auto max-w-sm rounded-lg bg-gray-900 px-4 py-3 text-sm text-white shadow-lg"
          style={{ marginBottom: 'env(safe-area-inset-bottom, 0px)' }}
        >
          {toast}
        </div>
      )}
    </>
  );
};

export default AutoNotifySheet;
