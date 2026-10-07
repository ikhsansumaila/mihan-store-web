import React, { useCallback, useEffect, useRef, useState } from 'react';
import { btnPrimary, btnSecondary, cardClass } from './ui';
import InstallSheet from './InstallSheet';
import {
  IOS_GUIDE_DISMISS_KEY,
  currentSubscription,
  fetchPushConfig,
  installAvailable,
  isAndroid,
  isIOS,
  isIOSOtherBrowser,
  isStandalone,
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
// Aktifkan / Matikan / Kirim tes, dan tombol "Pasang aplikasi" (iPhone/iPad dan Android yang belum
// memasang) yang membuka bottom sheet: iOS = langkah Safari; Android dengan beforeinstallprompt =
// "Pasang Sekarang" (prompt native) / "Nanti"; tanpa prompt = langkah manual. Tidak ada popup otomatis.

const PERMISSION_LABEL = {
  default: 'Belum diminta',
  granted: 'Diizinkan',
  denied: 'Diblokir',
  unsupported: 'Tidak tersedia',
};

const PushCard = () => {
  const supported = isPushSupported();
  const iosInstall = needsIOSInstall();
  const [guideHidden, setGuideHidden] = useState(() => storageGet(IOS_GUIDE_DISMISS_KEY) === '1');
  const [canInstall, setCanInstall] = useState(installAvailable());
  const [sheetOpen, setSheetOpen] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [installedNow, setInstalledNow] = useState(false);
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

  // "Pasang aplikasi" selalu membuka sheet; prompt native hanya dari tombol "Pasang Sekarang" di dalamnya.
  const openInstall = () => setSheetOpen(true);
  const installNow = async () => {
    setInstalling(true);
    try {
      const outcome = await promptInstall();
      if (outcome === 'accepted') setInstalledNow(true);
    } finally {
      if (mounted.current) {
        setInstalling(false);
        setSheetOpen(false);
      }
    }
  };

  const dismissGuide = () => {
    storageSet(IOS_GUIDE_DISMISS_KEY, '1');
    setGuideHidden(true);
    setSheetOpen(false);
  };

  const standalone = isStandalone();
  const ios = isIOS();
  const platform = ios ? 'ios' : isAndroid() ? 'android' : null;
  const showInstall = !standalone && !installedNow && !guideHidden && (canInstall || platform !== null);
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
        {showInstall && (
          <button type="button" className={btnSecondary} onClick={openInstall} data-testid="install-button">
            Pasang aplikasi
          </button>
        )}
      </div>

      {iosInstall && !guideHidden && (
        <p className="mt-2 text-sm text-gray-600" data-testid="ios-install-hint">
          Di iPhone/iPad, notifikasi hanya bisa diaktifkan dari aplikasi yang dibuka dari Layar Utama. Ketuk <strong>Pasang aplikasi</strong> untuk
          melihat caranya.
        </p>
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
      <InstallSheet
        open={sheetOpen}
        onClose={() => setSheetOpen(false)}
        platform={platform || 'android'}
        otherBrowser={ios && isIOSOtherBrowser()}
        canPrompt={canInstall}
        onInstall={installNow}
        installing={installing}
        onDismissForever={dismissGuide}
      />
    </section>
  );
};

export default PushCard;
