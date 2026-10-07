import React, { useCallback, useEffect, useRef, useState } from 'react';
import { btnPrimary, btnSecondary, cardClass } from './ui';
import {
  IOS_GUIDE_DISMISS_KEY,
  currentSubscription,
  fetchPushConfig,
  installAvailable,
  isIOSOtherBrowser,
  isPushSupported,
  needsIOSInstall,
  notificationPermission,
  onInstallAvailable,
  promptInstall,
  sendSubscription,
  sendTestPush,
  storageGet,
  storageSet,
  subscribeDevice,
  unsubscribeDevice,
} from './push';

// Kartu "Notifikasi" di Dashboard admin: status dukungan/izin/langganan perangkat ini, tombol
// Aktifkan / Matikan / Kirim tes, panduan "Pasang ke layar utama" untuk iPhone, dan tombol
// "Pasang aplikasi" (Android, bila browser menawarkan beforeinstallprompt).

const PERMISSION_LABEL = {
  default: 'Belum diminta',
  granted: 'Diizinkan',
  denied: 'Diblokir',
  unsupported: 'Tidak tersedia',
};

// Ikon "Bagikan" Safari (kotak dengan panah ke atas).
const ShareIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" className="inline h-5 w-5 align-text-bottom text-sky-600" fill="none" stroke="currentColor" strokeWidth="1.8">
    <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v12m0-12L8 7m4-4l4 4M7 11H5.5A1.5 1.5 0 004 12.5v7A1.5 1.5 0 005.5 21h13a1.5 1.5 0 001.5-1.5v-7a1.5 1.5 0 00-1.5-1.5H17" />
  </svg>
);

const AddIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true" className="inline h-5 w-5 align-text-bottom text-gray-700" fill="none" stroke="currentColor" strokeWidth="1.8">
    <rect x="4" y="4" width="16" height="16" rx="3" />
    <path strokeLinecap="round" d="M12 8v8M8 12h8" />
  </svg>
);

export const IOSInstallGuide = ({ onDismiss }) => {
  const otherBrowser = isIOSOtherBrowser();
  return (
    <div className="rounded-md border border-sky-200 bg-sky-50 p-3 text-sm text-sky-900" data-testid="ios-install-guide">
      <p className="font-semibold">Pasang ke layar utama</p>
      <p className="mt-1">Di iPhone/iPad, notifikasi hanya bisa diaktifkan dari aplikasi yang dipasang ke Layar Utama (iOS 16.4 atau lebih baru).</p>
      {otherBrowser && (
        <p className="mt-2 rounded bg-amber-100 px-2 py-1 text-amber-900" data-testid="ios-open-safari">
          Buka halaman ini lewat <strong>Safari</strong> terlebih dahulu, lalu ikuti langkah di bawah.
        </p>
      )}
      <ol className="mt-2 list-decimal space-y-1.5 pl-5">
        <li>
          Ketuk ikon <strong>Bagikan</strong> <ShareIcon /> di bilah Safari.
        </li>
        <li>
          Pilih <strong>Tambah ke Layar Utama</strong> <AddIcon />.
        </li>
        <li>
          Ketuk <strong>Tambah</strong>, lalu buka <strong>Mihan Store</strong> dari Layar Utama.
        </li>
        <li>Masuk ke halaman Admin, lalu ketuk <strong>Aktifkan notifikasi</strong>.</li>
      </ol>
      <button type="button" className="mt-3 text-xs font-medium text-sky-800 underline" onClick={onDismiss}>
        Jangan tampilkan lagi
      </button>
    </div>
  );
};

const PushCard = () => {
  const supported = isPushSupported();
  const iosInstall = needsIOSInstall();
  const [guideHidden, setGuideHidden] = useState(() => storageGet(IOS_GUIDE_DISMISS_KEY) === '1');
  const [canInstall, setCanInstall] = useState(installAvailable());
  const [permission, setPermission] = useState(notificationPermission());
  const [config, setConfig] = useState(null); // {enabled, publicKey}
  const [subscribed, setSubscribed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(null); // {tone, text}
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => onInstallAvailable(setCanInstall), []);

  const refresh = useCallback(async () => {
    if (!supported) return;
    try {
      const [cfg, sub] = await Promise.all([fetchPushConfig(), currentSubscription().catch(() => null)]);
      if (!mounted.current) return;
      setConfig(cfg || { enabled: false });
      setSubscribed(!!sub);
      setPermission(notificationPermission());
      // Sinkronkan diam-diam: langganan di perangkat ada tetapi mungkin terhapus di server (mis. 410).
      if (sub && cfg?.enabled && notificationPermission() === 'granted') sendSubscription(sub).catch(() => {});
    } catch (err) {
      if (mounted.current) setMessage({ tone: 'error', text: err.message || 'Gagal memuat status notifikasi.' });
    }
  }, [supported]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const enable = async () => {
    setMessage(null);
    setBusy(true);
    try {
      // Izin diminta LANGSUNG dari ketukan pengguna (Safari mewajibkan ini).
      const result = await window.Notification.requestPermission();
      setPermission(result);
      if (result !== 'granted') {
        setMessage({
          tone: 'warn',
          text: result === 'denied' ? 'Izin notifikasi ditolak. Ubah izin di pengaturan browser/perangkat untuk situs ini.' : 'Izin notifikasi belum diberikan.',
        });
        return;
      }
      const cfg = config?.publicKey ? config : await fetchPushConfig();
      if (!cfg?.enabled || !cfg.publicKey) {
        setConfig(cfg);
        setMessage({ tone: 'warn', text: 'Notifikasi push belum dikonfigurasi di server.' });
        return;
      }
      await subscribeDevice(cfg.publicKey);
      setSubscribed(true);
      setMessage({ tone: 'ok', text: 'Notifikasi aktif di perangkat ini.' });
    } catch (err) {
      setMessage({ tone: 'error', text: err?.message ? `Gagal mengaktifkan notifikasi: ${err.message}` : 'Gagal mengaktifkan notifikasi.' });
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const disable = async () => {
    setMessage(null);
    setBusy(true);
    try {
      await unsubscribeDevice();
      setSubscribed(false);
      setMessage({ tone: 'ok', text: 'Notifikasi dimatikan di perangkat ini.' });
    } catch (err) {
      setMessage({ tone: 'error', text: err?.message ? `Gagal mematikan notifikasi: ${err.message}` : 'Gagal mematikan notifikasi.' });
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const test = async () => {
    setMessage(null);
    setBusy(true);
    try {
      const r = await sendTestPush();
      if (r?.sent > 0) setMessage({ tone: 'ok', text: `Notifikasi tes dikirim ke ${r.sent} perangkat.` });
      else setMessage({ tone: 'warn', text: 'Tidak ada perangkat yang menerima notifikasi tes. Coba matikan lalu aktifkan lagi.' });
    } catch (err) {
      setMessage({ tone: 'error', text: err?.message || 'Gagal mengirim notifikasi tes.' });
    } finally {
      if (mounted.current) setBusy(false);
    }
  };

  const install = async () => {
    await promptInstall();
  };

  const dismissGuide = () => {
    storageSet(IOS_GUIDE_DISMISS_KEY, '1');
    setGuideHidden(true);
  };

  const showGuide = iosInstall && !guideHidden;
  // iOS di tab Safari + panduan disembunyikan + tidak didukung: kartu tidak perlu tampil sama sekali.
  if (iosInstall && guideHidden && !supported) return null;

  const deviceStatus = !supported ? 'Tidak didukung' : subscribed ? 'Berlangganan' : 'Belum berlangganan';
  const toneClass = {
    ok: 'border-emerald-200 bg-emerald-50 text-emerald-800',
    warn: 'border-amber-200 bg-amber-50 text-amber-900',
    error: 'border-red-200 bg-red-50 text-red-700',
  };

  return (
    <section aria-labelledby="notifikasi-judul" className={`${cardClass} p-4`} data-testid="push-card">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 id="notifikasi-judul" className="text-base font-semibold text-gray-900">
            Notifikasi
          </h2>
          <p className="mt-0.5 text-sm text-gray-600">Notifikasi di perangkat ini saat ada pesanan baru atau pesanan dibatalkan pelanggan.</p>
        </div>
        {canInstall && (
          <button type="button" className={btnSecondary} onClick={install}>
            Pasang aplikasi
          </button>
        )}
      </div>

      {showGuide && (
        <div className="mt-3">
          <IOSInstallGuide onDismiss={dismissGuide} />
        </div>
      )}

      <dl className="mt-3 grid grid-cols-1 gap-x-6 gap-y-1 text-sm sm:grid-cols-3" data-testid="push-status">
        <div className="flex gap-1.5">
          <dt className="text-gray-500">Browser:</dt>
          <dd className="font-medium text-gray-800">{supported ? 'Didukung' : 'Tidak didukung'}</dd>
        </div>
        <div className="flex gap-1.5">
          <dt className="text-gray-500">Izin:</dt>
          <dd className="font-medium text-gray-800">{PERMISSION_LABEL[permission] || permission}</dd>
        </div>
        <div className="flex gap-1.5">
          <dt className="text-gray-500">Perangkat ini:</dt>
          <dd className="font-medium text-gray-800">{deviceStatus}</dd>
        </div>
      </dl>

      {!supported && !iosInstall && (
        <p className="mt-2 text-sm text-gray-600">Browser ini tidak mendukung notifikasi push. Gunakan Chrome/Edge/Firefox terbaru atau Safari (iOS 16.4+, dari Layar Utama).</p>
      )}
      {supported && config && !config.enabled && <p className="mt-2 text-sm text-amber-800">Notifikasi push belum dikonfigurasi di server.</p>}
      {supported && permission === 'denied' && (
        <p className="mt-2 text-sm text-amber-800">Izin notifikasi diblokir. Buka pengaturan situs/aplikasi di perangkat, izinkan notifikasi, lalu muat ulang halaman.</p>
      )}

      {supported && (
        <div className="mt-3 flex flex-wrap gap-2">
          {!subscribed ? (
            <button type="button" className={btnPrimary} onClick={enable} disabled={busy || permission === 'denied' || (config && !config.enabled)}>
              Aktifkan notifikasi
            </button>
          ) : (
            <>
              <button type="button" className={btnSecondary} onClick={test} disabled={busy}>
                Kirim tes
              </button>
              <button type="button" className={btnSecondary} onClick={disable} disabled={busy}>
                Matikan notifikasi
              </button>
            </>
          )}
        </div>
      )}

      {message && (
        <p role="status" className={`mt-3 rounded-md border px-3 py-2 text-sm ${toneClass[message.tone] || toneClass.warn}`}>
          {message.text}
        </p>
      )}
    </section>
  );
};

export default PushCard;
