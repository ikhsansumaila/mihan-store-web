import React, { useCallback, useEffect, useRef, useState } from 'react';
import BottomSheet from '../components/BottomSheet';
import InstallSheet, { CUSTOMER_BENEFIT } from '../admin/InstallSheet';
import {
  currentSubscription,
  installAvailable,
  isAndroid,
  isIOS,
  isIOSOtherBrowser,
  isPushSupported,
  isStandalone,
  needsIOSInstall,
  notificationPermission,
  onInstallAvailable,
  promptInstall,
  sendSubscription,
  unsubscribeDevice,
} from '../admin/push';
import { CUSTOMER_PUSH_EVENT, customerPushApi, enableCustomerPush, isCustomerFlagged, clearCustomerFlag } from './pushApi';

// Ikon gear "Pengaturan" di navbar toko (pelanggan login). Membuka sheet Pengaturan berisi:
// Notifikasi (status perangkat + Aktifkan/Matikan), Pasang aplikasi (InstallSheet yang sama dengan admin),
// dan Logout. Sheet (BottomSheet) dipakai di semua ukuran layar: di HP naik dari bawah, di md ke atas
// tampil sebagai dialog terpusat; tutup dengan X, ketuk latar, atau Escape.
// Titik kecil di gear = notifikasi didukung + aktif di server + izin belum diblokir + belum aktif di perangkat ini.

export const GearIcon = ({ className = 'h-6 w-6' }) => (
  <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="none" stroke="currentColor" strokeWidth="1.8">
    <path
      strokeLinecap="round"
      strokeLinejoin="round"
      d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 010 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 010-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28z"
    />
    <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
  </svg>
);

const PERMISSION_LABEL = { default: 'Belum diminta', granted: 'Diizinkan', denied: 'Diblokir', unsupported: 'Tidak tersedia' };

const SettingsMenu = ({ user, onLogout }) => {
  const supported = isPushSupported();
  const [open, setOpen] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const [permission, setPermission] = useState(notificationPermission());
  const [config, setConfig] = useState(null);
  const [subscribed, setSubscribed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(null);
  const [canInstall, setCanInstall] = useState(installAvailable());
  const [installing, setInstalling] = useState(false);
  const gearRef = useRef(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const off = onInstallAvailable(setCanInstall);
    return () => {
      mounted.current = false;
      off();
    };
  }, []);

  const refresh = useCallback(async () => {
    if (!supported) return;
    try {
      const [cfg, sub] = await Promise.all([customerPushApi.fetchConfig(), currentSubscription().catch(() => null)]);
      if (!mounted.current) return;
      const active = !!sub && isCustomerFlagged(user);
      setConfig(cfg || { enabled: false });
      setSubscribed(active);
      setPermission(notificationPermission());
      // Sinkron diam-diam (langganan bisa terhapus di server, mis. 410) — hanya bila pelanggan ini yang mengaktifkan.
      if (active && cfg?.enabled && notificationPermission() === 'granted') sendSubscription(sub, customerPushApi).catch(() => {});
    } catch {
      if (mounted.current) setConfig({ enabled: false, error: true });
    }
  }, [supported, user]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Status berubah dari tempat lain (sheet ajakan aktifkan notifikasi) -> segarkan titik & status.
  useEffect(() => {
    const onChange = () => refresh();
    window.addEventListener(CUSTOMER_PUSH_EVENT, onChange);
    return () => window.removeEventListener(CUSTOMER_PUSH_EVENT, onChange);
  }, [refresh]);

  const enable = async () => {
    setMessage(null);
    setBusy(true);
    try {
      const r = await enableCustomerPush(user, config);
      setPermission(notificationPermission());
      if (r.ok) {
        setSubscribed(true);
        setMessage({ tone: 'ok', text: 'Notifikasi aktif. Anda akan menerima kabar status pesanan di perangkat ini.' });
      } else if (r.disabled) {
        if (r.config) setConfig(r.config);
        setMessage({ tone: 'warn', text: 'Notifikasi belum tersedia saat ini.' });
      } else {
        setMessage({
          tone: 'warn',
          text: r.permission === 'denied' ? 'Izin notifikasi ditolak. Ubah izin di pengaturan browser/perangkat untuk situs ini.' : 'Izin notifikasi belum diberikan.',
        });
      }
    } catch {
      setMessage({ tone: 'error', text: 'Gagal mengaktifkan notifikasi. Coba lagi.' });
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const disable = async () => {
    setMessage(null);
    setBusy(true);
    try {
      await unsubscribeDevice(customerPushApi);
      clearCustomerFlag();
      setSubscribed(false);
      setMessage({ tone: 'ok', text: 'Notifikasi dimatikan di perangkat ini.' });
    } catch {
      clearCustomerFlag();
      setSubscribed(false);
      setMessage({ tone: 'error', text: 'Notifikasi dimatikan di perangkat ini, tetapi server belum terhubung.' });
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const standalone = isStandalone();
  const ios = isIOS();
  const platform = ios ? 'ios' : isAndroid() ? 'android' : null;
  const showInstall = !standalone && (canInstall || platform !== null);
  const iosInstall = needsIOSInstall();
  const showDot = supported && !!config?.enabled && permission !== 'denied' && permission !== 'unsupported' && !subscribed;

  const openInstall = () => {
    setOpen(false);
    setInstallOpen(true);
  };
  const closeInstall = () => {
    setInstallOpen(false);
    if (gearRef.current) gearRef.current.focus();
  };
  const installNow = async () => {
    setInstalling(true);
    try {
      await promptInstall();
    } finally {
      if (mounted.current) {
        setInstalling(false);
        closeInstall();
      }
    }
  };

  const toneClass = {
    ok: 'border-emerald-200 bg-emerald-50 text-emerald-800',
    warn: 'border-amber-200 bg-amber-50 text-amber-900',
    error: 'border-red-200 bg-red-50 text-red-700',
  };
  const deviceStatus = !supported ? 'Tidak didukung di browser ini' : subscribed ? 'Aktif' : 'Belum aktif';
  const btn = 'inline-flex items-center justify-center rounded-lg px-4 py-2 text-sm font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 disabled:opacity-60';

  return (
    <>
      <button
        ref={gearRef}
        type="button"
        onClick={() => setOpen(true)}
        aria-label={showDot ? 'Pengaturan (notifikasi belum aktif)' : 'Pengaturan'}
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Pengaturan"
        className="relative inline-flex h-10 w-10 items-center justify-center rounded-full hover:bg-white/15 transition focus:outline-none focus-visible:ring-2 focus-visible:ring-white"
        data-testid="settings-gear"
      >
        <GearIcon />
        {showDot && <span aria-hidden="true" data-testid="settings-dot" className="absolute right-1.5 top-1.5 h-2.5 w-2.5 rounded-full bg-yellow-400 ring-2 ring-purple-700" />}
      </button>

      <BottomSheet open={open} onClose={() => setOpen(false)} labelledBy="settings-title" testId="settings-sheet">
        <div className="flex items-center justify-between gap-3">
          <h2 id="settings-title" className="text-lg font-bold text-gray-900">
            Pengaturan
          </h2>
          <button
            type="button"
            onClick={() => setOpen(false)}
            aria-label="Tutup"
            className="flex h-9 w-9 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
        <p className="mt-0.5 truncate text-sm text-gray-500">Masuk sebagai {user?.name || user?.username}</p>

        <section className="mt-4 rounded-xl border border-gray-200 p-4" aria-labelledby="settings-notif" data-testid="settings-notif">
          <h3 id="settings-notif" className="font-semibold text-gray-900">
            Notifikasi
          </h3>
          <p className="mt-0.5 text-sm text-gray-600">Kabar status pesanan Anda: ongkir dikonfirmasi, pembayaran diterima, pesanan selesai atau dibatalkan.</p>
          <dl className="mt-2 space-y-0.5 text-sm" data-testid="settings-status">
            <div className="flex gap-1.5">
              <dt className="text-gray-500">Perangkat ini:</dt>
              <dd className="font-medium text-gray-800">{deviceStatus}</dd>
            </div>
            {supported && (
              <div className="flex gap-1.5">
                <dt className="text-gray-500">Izin:</dt>
                <dd className="font-medium text-gray-800">{PERMISSION_LABEL[permission] || permission}</dd>
              </div>
            )}
          </dl>
          {!supported && iosInstall && (
            <p className="mt-2 text-sm text-gray-600">Di iPhone/iPad, notifikasi hanya bisa diaktifkan dari aplikasi yang dibuka dari Layar Utama. Pasang aplikasi dulu.</p>
          )}
          {supported && permission === 'denied' && (
            <p className="mt-2 text-sm text-amber-800">Izin notifikasi diblokir. Izinkan notifikasi di pengaturan situs/aplikasi, lalu muat ulang halaman.</p>
          )}
          {supported && config && !config.enabled && <p className="mt-2 text-sm text-amber-800">Notifikasi belum tersedia saat ini.</p>}
          {supported && (
            <div className="mt-3">
              {!subscribed ? (
                <button
                  type="button"
                  className={`${btn} w-full bg-purple-700 text-white hover:bg-purple-800`}
                  onClick={enable}
                  disabled={busy || permission === 'denied' || (config && !config.enabled)}
                >
                  Aktifkan notifikasi
                </button>
              ) : (
                <button type="button" className={`${btn} w-full border border-gray-300 bg-white text-gray-800 hover:bg-gray-50`} onClick={disable} disabled={busy}>
                  Matikan notifikasi
                </button>
              )}
            </div>
          )}
          {message && (
            <p role="status" className={`mt-3 rounded-md border px-3 py-2 text-sm ${toneClass[message.tone] || toneClass.warn}`}>
              {message.text}
            </p>
          )}
        </section>

        {showInstall && (
          <button
            type="button"
            onClick={openInstall}
            className="mt-3 flex w-full items-center justify-between rounded-xl border border-gray-200 px-4 py-3 text-left text-sm font-semibold text-gray-800 hover:bg-gray-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
          >
            <span>Pasang aplikasi</span>
            <span className="text-xs font-normal text-gray-500">ke layar utama</span>
          </button>
        )}

        <button
          type="button"
          onClick={() => {
            setOpen(false);
            onLogout();
          }}
          className={`${btn} mt-3 w-full border border-red-200 bg-white text-red-700 hover:bg-red-50`}
        >
          Logout
        </button>
      </BottomSheet>

      <InstallSheet
        open={installOpen}
        onClose={closeInstall}
        platform={platform || 'android'}
        otherBrowser={ios && isIOSOtherBrowser()}
        canPrompt={canInstall}
        onInstall={installNow}
        installing={installing}
        benefit={CUSTOMER_BENEFIT}
        testId="customer-install-sheet"
      />
    </>
  );
};

export default SettingsMenu;
