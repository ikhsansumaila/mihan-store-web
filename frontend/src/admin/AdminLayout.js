import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom';
import { adminFetch } from './api';
import { Icon } from './icons';
import AutoInstallSheet from './AutoInstallSheet';

// Layout admin bergaya cPanel: sidebar kiri tetap 260px (>= 1024px) / bilah ikon 64px yang selalu
// terlihat (layar sempit), topbar dengan breadcrumb, konten mengisi sisa lebar di kanan. Navigasi antar
// halaman admin memakai react-router (sudah di dalam area Cloudflare Access); "Kembali ke toko" tetap
// anchor biasa.

export const ADMIN_UI_VERSION = 'v1.5.0';

export const MENU = [
  { group: 'Utama', items: [{ to: '/admin', end: true, label: 'Dashboard', icon: 'home', keywords: 'ringkasan beranda statistik' }] },
  {
    group: 'Katalog',
    items: [
      { to: '/admin/products', label: 'Produk', icon: 'box', keywords: 'barang harga stok' },
      { to: '/admin/categories', label: 'Kategori', icon: 'tag', keywords: 'slug' },
    ],
  },
  {
    group: 'Penjualan',
    items: [
      // Lencana = pesanan menunggu konfirmasi (butuh tindakan admin: isi ongkir & konfirmasi).
      { to: '/admin/orders', label: 'Pesanan', icon: 'cart', badge: 'pendingConfirmation', badgeLabel: 'menunggu konfirmasi', keywords: 'order transaksi pembayaran konfirmasi' },
      { to: '/admin/customers', label: 'Pelanggan', icon: 'users', keywords: 'customer pembeli akun alias nama panggilan' },
      { to: '/admin/invoice', label: 'Invoice', icon: 'doc', keywords: 'faktur pdf nota' },
      { to: '/admin/pricelist', label: 'Pricelist', icon: 'pricelist', keywords: 'daftar harga png gambar whatsapp bagikan share' },
    ],
  },
  {
    group: 'Sistem',
    items: [
      { to: '/admin/settings', label: 'Pengaturan Toko', icon: 'settings', keywords: 'whatsapp bank rekening' },
      {
        to: '/admin/regions',
        label: 'Data Wilayah',
        icon: 'map',
        keywords: 'wilayah provinsi kabupaten kota kecamatan kelurahan desa alamat fetch wilayah.id',
      },
      { to: '/admin/activity', label: 'Log Aktivitas', icon: 'clock', keywords: 'riwayat audit' },
    ],
  },
];

// Cari item menu (beserta grupnya) untuk pathname saat ini.
export const findMenuItem = (pathname) => {
  const p = pathname.replace(/\/+$/, '') || '/';
  for (const g of MENU) {
    for (const it of g.items) {
      if (it.end ? p === it.to : p === it.to || p.startsWith(`${it.to}/`)) return { group: g.group, item: it };
    }
  }
  return null;
};

export const breadcrumbFor = (pathname) => {
  const root = { label: 'Admin', to: '/admin' };
  const found = findMenuItem(pathname);
  if (!found) return { title: 'Tidak ditemukan', crumbs: [root, { label: 'Tidak ditemukan' }] };
  const { group, item } = found;
  if (item.end) return { title: item.label, crumbs: [root, { label: item.label }] };
  const isDetail = pathname.replace(/\/+$/, '') !== item.to;
  const crumbs = [root, { label: group }, { label: item.label, to: isDetail ? item.to : undefined }];
  if (isDetail) crumbs.push({ label: 'Detail' });
  return { title: isDetail ? `${item.label} · Detail` : item.label, crumbs };
};

export const filterMenu = (query) => {
  const q = query.trim().toLowerCase();
  if (!q) return MENU;
  return MENU.map((g) => ({
    ...g,
    items: g.items.filter((it) => `${it.label} ${g.group} ${it.keywords || ''}`.toLowerCase().includes(q)),
  })).filter((g) => g.items.length > 0);
};

// Ringkasan (/api/admin/summary) dibagi antara sidebar (lencana Pesanan), Dashboard, dan angka tab Pesanan.
// Disegarkan: saat mount, saat Dashboard/Daftar Pesanan dibuka atau tab/filter diganti, setelah setiap
// perubahan pesanan di detail, saat aplikasi kembali terlihat/fokus (throttle), dan polling ringan selama
// Daftar Pesanan terbuka & terlihat. Permintaan bersamaan digabung (inflight); nilai lama dipertahankan
// sampai data baru datang (tidak berkedip).
export const SUMMARY_THROTTLE_MS = 15000;
export const SUMMARY_POLL_MS = 60000;

const SummaryContext = createContext({
  summary: null,
  error: null,
  refresh: () => Promise.resolve(),
  refreshIfStale: () => Promise.resolve(),
});
export const useAdminSummary = () => useContext(SummaryContext);

const pageVisible = () => typeof document === 'undefined' || document.visibilityState !== 'hidden';

const SummaryProvider = ({ children }) => {
  const [summary, setSummary] = useState(null);
  const [error, setError] = useState(null);
  const inflight = useRef(null);
  const lastAt = useRef(0);
  const refresh = useCallback(() => {
    if (inflight.current) return inflight.current;
    lastAt.current = Date.now();
    const p = adminFetch('/summary')
      .then((d) => {
        setSummary(d || {});
        setError(null);
      })
      .catch((err) => setError(err))
      .finally(() => {
        inflight.current = null;
      });
    inflight.current = p;
    return p;
  }, []);
  // Untuk pemicu otomatis (fokus/terlihat/polling): paling sering sekali per SUMMARY_THROTTLE_MS.
  const refreshIfStale = useCallback(() => {
    if (Date.now() - lastAt.current < SUMMARY_THROTTLE_MS) return inflight.current || Promise.resolve();
    return refresh();
  }, [refresh]);
  useEffect(() => {
    refresh();
  }, [refresh]);
  useEffect(() => {
    const onVisible = () => {
      if (pageVisible()) refreshIfStale();
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    return () => {
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
    };
  }, [refreshIfStale]);
  const value = useMemo(() => ({ summary, error, refresh, refreshIfStale }), [summary, error, refresh, refreshIfStale]);
  return <SummaryContext.Provider value={value}>{children}</SummaryContext.Provider>;
};

// Polling ringan ringkasan selama komponen pemanggil terpasang dan halaman terlihat.
export const useSummaryPolling = (intervalMs = SUMMARY_POLL_MS) => {
  const { refreshIfStale } = useAdminSummary();
  useEffect(() => {
    const t = setInterval(() => {
      if (pageVisible()) refreshIfStale();
    }, intervalMs);
    return () => clearInterval(t);
  }, [intervalMs, refreshIfStale]);
};

// Teks yang hanya terlihat di mode penuh (>= 1024px); di bilah ikon tetap ada untuk pembaca layar.
const LABEL_FULL = 'sr-only lg:not-sr-only';

const Sidebar = ({ me }) => {
  const [query, setQuery] = useState('');
  const navigate = useNavigate();
  const { summary } = useAdminSummary();
  const groups = filterMenu(query);

  // Kolom cari disembunyikan di mode ikon; kosongkan agar menu tidak tersaring diam-diam.
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return undefined;
    const mq = window.matchMedia('(min-width: 1024px)');
    const onChange = (e) => !e.matches && setQuery('');
    mq.addEventListener?.('change', onChange);
    return () => mq.removeEventListener?.('change', onChange);
  }, []);

  const onSearchKey = (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      const first = groups[0]?.items[0];
      if (first) {
        navigate(first.to);
        setQuery('');
      }
    } else if (e.key === 'Escape' && query) {
      e.stopPropagation();
      setQuery('');
    }
  };

  const badgeOf = (it) => {
    if (!it.badge) return null;
    const n = summary?.orders?.[it.badge];
    return Number.isFinite(n) && n > 0 ? n : null;
  };

  return (
    // < 1024px: bilah ikon sempit (64px) yang selalu terlihat; >= 1024px: sidebar penuh 260px.
    // Keduanya fixed setinggi layar; daftar menu scroll sendiri bila tidak muat.
    <aside
      id="admin-sidebar"
      aria-label="Sidebar admin"
      className="fixed inset-y-0 left-0 z-30 flex w-16 flex-col overflow-hidden bg-slate-900 text-slate-200 lg:w-[260px]"
    >
      <div className="flex shrink-0 items-center justify-center gap-3 border-b border-slate-800 px-2 py-3 lg:justify-start lg:px-4 lg:py-4">
        <img
          src="/mihan-store-logo.png"
          alt=""
          title="Mihan Store Admin"
          className="h-10 w-10 shrink-0 rounded-md bg-white object-contain p-0.5 lg:h-9 lg:w-9"
        />
        <div className={`${LABEL_FULL} lg:min-w-0 lg:flex-1`}>
          <div className="truncate text-sm font-semibold text-white">Mihan Store Admin</div>
          <div className="hidden text-[11px] text-slate-400 lg:block">Panel pengelola toko</div>
        </div>
      </div>

      <div className="hidden px-3 pt-3 lg:block">
        <label className="relative block">
          <span className="sr-only">Cari menu</span>
          <span className="pointer-events-none absolute inset-y-0 left-2.5 flex items-center text-slate-500">
            <Icon name="search" className="h-4 w-4" />
          </span>
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onSearchKey}
            placeholder="Cari menu..."
            aria-label="Cari menu"
            className="w-full rounded-md border border-slate-700 bg-slate-800 py-2 pl-8 pr-2 text-sm text-slate-100 placeholder-slate-500 focus:border-purple-500 focus:outline-none focus:ring-1 focus:ring-purple-500"
          />
        </label>
      </div>

      <nav aria-label="Menu admin" className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-2 pb-4 lg:mt-2 lg:px-3">
        {groups.length === 0 && <p className="px-2 py-4 text-sm text-slate-400">Tidak ada menu yang cocok.</p>}
        {groups.map((g, gi) => (
          <div key={g.group} className="mt-2 lg:mt-3" data-group={g.group}>
            {gi > 0 && <div aria-hidden="true" className="mx-2 mb-2 border-t border-slate-800 lg:hidden" />}
            <div className="hidden px-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-slate-500 lg:block">{g.group}</div>
            <ul className="space-y-1 lg:space-y-0.5">
              {g.items.map((it) => {
                const badge = badgeOf(it);
                const name = badge !== null ? `${it.label}, ${badge} ${it.badgeLabel || 'menunggu'}` : it.label;
                return (
                  <li key={it.to}>
                    <NavLink
                      to={it.to}
                      end={it.end}
                      onClick={() => setQuery('')}
                      title={name}
                      aria-label={name}
                      data-menu={it.label}
                      className={({ isActive }) =>
                        `group relative flex h-11 items-center justify-center rounded-md border-l-4 text-sm font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400 lg:h-auto lg:justify-start lg:gap-3 lg:px-2.5 lg:py-2 ${
                          isActive
                            ? 'border-purple-400 bg-purple-600/30 text-white'
                            : 'border-transparent text-slate-300 hover:bg-slate-800 hover:text-white'
                        }`
                      }
                    >
                      <Icon name={it.icon} className="h-6 w-6 shrink-0 opacity-90 lg:h-5 lg:w-5" />
                      <span className={`${LABEL_FULL} lg:flex-1 lg:truncate`}>{it.label}</span>
                      {badge !== null && (
                        <>
                          {/* Mode ikon: angka kecil di pojok ikon. */}
                          <span
                            aria-hidden="true"
                            data-badge="icon"
                            className="absolute right-0.5 top-0.5 flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-amber-400 px-1 text-[10px] font-bold leading-none text-slate-900 ring-2 ring-slate-900 lg:hidden"
                          >
                            {badge > 99 ? '99+' : badge}
                          </span>
                          {/* Mode penuh: lencana di ujung kanan baris. */}
                          <span
                            aria-hidden="true"
                            data-badge="full"
                            className="hidden rounded-full bg-amber-400 px-2 py-0.5 text-[11px] font-bold leading-none text-slate-900 lg:inline-block"
                          >
                            {badge}
                          </span>
                        </>
                      )}
                    </NavLink>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      <div className="shrink-0 border-t border-slate-800 px-2 py-2 text-xs lg:px-4 lg:py-3">
        <div className="hidden text-slate-400 lg:block" data-testid="admin-email">
          Masuk sebagai <span className="break-all font-medium text-slate-200">{me?.email || '-'}</span>
        </div>
        <a
          href="/"
          title="Kembali ke toko"
          aria-label="Kembali ke toko"
          className="flex h-11 items-center justify-center rounded-md text-sm font-medium text-purple-300 hover:bg-slate-800 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400 lg:mt-2 lg:inline-flex lg:h-auto lg:justify-start lg:gap-1.5 lg:hover:bg-transparent"
        >
          <Icon name="back" className="h-6 w-6 lg:h-4 lg:w-4" />
          <span className={LABEL_FULL}>Kembali ke toko</span>
        </a>
        <div className="mt-2 hidden text-[11px] text-slate-500 lg:block">Mihan Store Admin {ADMIN_UI_VERSION}</div>
      </div>
    </aside>
  );
};

const Topbar = ({ me }) => {
  const { pathname } = useLocation();
  const { title, crumbs } = breadcrumbFor(pathname);
  const email = me?.email || '';
  const short = email.split('@')[0] || 'admin';
  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-gray-200 bg-white/95 px-3 backdrop-blur sm:h-16 sm:px-6 lg:px-8">
      <div className="min-w-0 flex-1">
        <p className="truncate text-base font-semibold text-gray-900" data-testid="admin-title">
          {title}
        </p>
        <nav aria-label="Breadcrumb" className="min-w-0">
          {/* Layar sempit: hanya dua tingkat terakhir (mis. "Penjualan / Pesanan"). */}
          <ol className="flex min-w-0 items-center gap-1 overflow-hidden whitespace-nowrap text-xs text-gray-500">
            {crumbs.map((c, i) => {
              const last = i === crumbs.length - 1;
              const compactHidden = i < crumbs.length - 2;
              return (
                <li key={`${i}-${c.label}`} className={`${compactHidden ? 'hidden sm:flex' : 'flex'} min-w-0 items-center gap-1`}>
                  {i > 0 && (
                    <span aria-hidden="true" className={i === crumbs.length - 2 ? 'hidden sm:inline' : undefined}>
                      /
                    </span>
                  )}
                  {last ? (
                    <span aria-current="page" className="truncate font-medium text-gray-700">
                      {c.label}
                    </span>
                  ) : c.to ? (
                    <Link to={c.to} className="hover:text-purple-700 hover:underline">
                      {c.label}
                    </Link>
                  ) : (
                    <span>{c.label}</span>
                  )}
                </li>
              );
            })}
          </ol>
        </nav>
      </div>
      <div className="flex shrink-0 items-center gap-2" title={email}>
        <span className="hidden max-w-[12rem] truncate text-sm text-gray-600 md:inline">{short}</span>
        <span
          className="flex h-8 w-8 items-center justify-center rounded-full bg-purple-700 text-sm font-semibold uppercase text-white"
          aria-label={`Admin: ${email}`}
        >
          {short.charAt(0) || 'A'}
        </span>
      </div>
    </header>
  );
};

const AdminLayout = ({ me, children }) => (
  <SummaryProvider>
    <div className="min-h-screen bg-gray-100 text-gray-800">
      <a
        href="#admin-main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-white focus:px-3 focus:py-2 focus:text-sm focus:shadow"
      >
        Lewati ke konten
      </a>
      <Sidebar me={me} />
      {/* Konten mengisi seluruh sisa lebar (viewport - bilah 64px / sidebar 260px), tanpa max-width;
          kata panjang (email, username) dipatah agar tidak mendorong halaman melebar. */}
      <div className="flex min-h-screen min-w-0 flex-col pl-16 lg:pl-[260px]">
        <Topbar me={me} />
        <main id="admin-main" tabIndex={-1} className="w-full min-w-0 flex-1 break-words px-3 py-4 focus:outline-none sm:px-6 lg:px-8 lg:py-6 2xl:px-10">
          {children}
        </main>
      </div>
      {/* Sheet "Pasang Mihan Store" otomatis sekali per sesi tab setelah admin terverifikasi (AdminLayout hanya
          dirender setelah /api/admin/me sukses). */}
      <AutoInstallSheet />
    </div>
  </SummaryProvider>
);

export default AdminLayout;
