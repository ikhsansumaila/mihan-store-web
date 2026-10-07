import React, { useEffect, useRef, useState } from 'react';
import InstallSheet from './InstallSheet';
import {
  IOS_GUIDE_DISMISS_KEY,
  INSTALL_DISMISSED_EVENT,
  installAvailable,
  isAndroid,
  isIOS,
  isIOSOtherBrowser,
  isStandalone,
  onInstallAvailable,
  promptInstall,
  sessionGet,
  sessionSet,
  storageGet,
  storageSet,
} from './push';

// Sheet "Pasang Mihan Store" yang muncul OTOMATIS sekali setelah admin baru masuk.
//
// "Baru selesai login" = shell admin (AdminLayout) pertama kali dipasang di tab ini SETELAH
// /api/admin/me sukses (artinya login Cloudflare Access selesai dan sesi admin terverifikasi backend).
// Penanda di sessionStorage membuatnya hanya sekali per sesi tab: pindah halaman admin (AdminLayout tetap
// terpasang) maupun muat ulang tidak memunculkannya lagi. Tab/jendela baru = sesi baru.
//
// Tidak muncul bila: sudah standalone/terpasang, "Jangan tampilkan lagi" pernah dipilih, bukan iPhone/iPad
// atau Android (desktop tidak pernah otomatis), atau sedang ada dialog modal lain terbuka.
// iOS: langkah Safari. Android: sama seperti tombol di kartu Notifikasi ("Pasang Sekarang" bila
// beforeinstallprompt tersedia, selain itu langkah manual).

export const AUTO_SHEET_SESSION_KEY = 'mihan.installSheet.autoShown';
const DEFAULT_DELAY_MS = 800; // beri waktu halaman admin selesai dirender sebelum sheet & fokus pindah

export const autoInstallPlatform = () => {
  if (isStandalone()) return null;
  if (storageGet(IOS_GUIDE_DISMISS_KEY) === '1') return null;
  if (isIOS()) return 'ios';
  if (isAndroid()) return 'android';
  return null;
};

const otherModalOpen = () => !!document.querySelector('[role="dialog"][aria-modal="true"]');

const AutoInstallSheet = ({ delayMs = DEFAULT_DELAY_MS }) => {
  const [open, setOpen] = useState(false);
  const [platform, setPlatform] = useState(null);
  const [canInstall, setCanInstall] = useState(installAvailable());
  const [installing, setInstalling] = useState(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const off = onInstallAvailable(setCanInstall);
    return () => {
      mounted.current = false;
      off();
    };
  }, []);

  useEffect(() => {
    if (sessionGet(AUTO_SHEET_SESSION_KEY) === '1') return undefined;
    const p = autoInstallPlatform();
    if (!p) return undefined;
    const t = setTimeout(() => {
      // Ulangi pemeriksaan saat akan tampil (bisa saja berubah selama jeda).
      if (!mounted.current || sessionGet(AUTO_SHEET_SESSION_KEY) === '1' || autoInstallPlatform() !== p) return;
      if (otherModalOpen()) return; // jangan menimpa dialog lain; dicoba lagi pada pemuatan berikutnya
      sessionSet(AUTO_SHEET_SESSION_KEY, '1');
      setPlatform(p);
      setOpen(true);
    }, delayMs);
    return () => clearTimeout(t);
  }, [delayMs]);

  const close = () => setOpen(false); // hanya untuk sesi ini

  const installNow = async () => {
    setInstalling(true);
    try {
      await promptInstall();
    } finally {
      if (mounted.current) {
        setInstalling(false);
        setOpen(false);
      }
    }
  };

  const dismissForever = () => {
    storageSet(IOS_GUIDE_DISMISS_KEY, '1');
    try {
      window.dispatchEvent(new Event(INSTALL_DISMISSED_EVENT));
    } catch {
      /* abaikan */
    }
    setOpen(false);
  };

  if (!platform) return null;
  return (
    <InstallSheet
      open={open}
      onClose={close}
      platform={platform}
      otherBrowser={platform === 'ios' && isIOSOtherBrowser()}
      canPrompt={canInstall}
      onInstall={installNow}
      installing={installing}
      onDismissForever={dismissForever}
      testId="auto-install-sheet"
    />
  );
};

export default AutoInstallSheet;
