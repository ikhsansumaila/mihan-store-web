import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom';
import { adminFetch } from './api';
import { Icon } from './icons';

// Layout admin bergaya cPanel: sidebar kiri tetap (desktop >= 1024px) / laci (layar sempit),
// topbar dengan breadcrumb, konten di kanan. Navigasi antar halaman admin memakai react-router
// (sudah di dalam area Cloudflare Access); "Kembali ke toko" tetap anchor biasa.

export const ADMIN_UI_VERSION = 'v1.1.0';

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
      { to: '/admin/orders', label: 'Pesanan', icon: 'cart', badge: 'pendingPayment', keywords: 'order transaksi pembayaran' },
      { to: '/admin/invoice', label: 'Invoice', icon: 'doc', keywords: 'faktur pdf nota' },
    ],
  },
  {
    group: 'Sistem',
    items: [
      { to: '/admin/settings', label: 'Pengaturan Toko', icon: 'settings', keywords: 'whatsapp bank rekening' },
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

// Ringkasan (/api/admin/summary) dibagi antara sidebar (lencana Pesanan) dan Dashboard.
const SummaryContext = createContext({ summary: null, error: null, refresh: () => Promise.resolve() });
export const useAdminSummary = () => useContext(SummaryContext);

const SummaryProvider = ({ children }) => {
  const [summary, setSummary] = useState(null);
  const [error, setError] = useState(null);
  const inflight = useRef(null);
  const refresh = useCallback(() => {
    if (inflight.current) return inflight.current;
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
  useEffect(() => {
    refresh();
  }, [refresh]);
  const value = useMemo(() => ({ summary, error, refresh }), [summary, error, refresh]);
  return <SummaryContext.Provider value={value}>{children}</SummaryContext.Provider>;
};

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

const Sidebar = ({ me, open, onClose, closeBtnRef, asideRef }) => {
  const [query, setQuery] = useState('');
  const navigate = useNavigate();
  const { summary } = useAdminSummary();
  const groups = filterMenu(query);

  const pick = () => {
    setQuery('');
    onClose();
  };

  const onSearchKey = (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      const first = groups[0]?.items[0];
      if (first) {
        navigate(first.to);
        pick();
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
    <aside
      id="admin-sidebar"
      ref={asideRef}
      aria-label="Sidebar admin"
      role={open ? 'dialog' : undefined}
      aria-modal={open ? 'true' : undefined}
      data-open={open ? 'true' : 'false'}
      className={`fixed inset-y-0 left-0 z-40 flex w-[260px] max-w-[85vw] flex-col bg-slate-900 text-slate-200 shadow-xl duration-200 lg:visible lg:translate-x-0 lg:shadow-none ${
        // Saat dibuka visibility langsung aktif (agar fokus bisa pindah); saat ditutup baru
        // disembunyikan setelah animasi geser selesai (keluar dari urutan Tab).
        open ? 'visible translate-x-0 transition-transform' : 'invisible -translate-x-full transition-[transform,visibility]'
      }`}
    >
      <div className="flex items-center gap-3 border-b border-slate-800 px-4 py-4">
        <img src="/mihan-store-logo.png" alt="" className="h-9 w-9 shrink-0 rounded-md bg-white object-contain p-0.5" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-white">Mihan Store Admin</div>
          <div className="text-[11px] text-slate-400">Panel pengelola toko</div>
        </div>
        <button
          type="button"
          ref={closeBtnRef}
          onClick={onClose}
          className="rounded-md p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400 lg:hidden"
          aria-label="Tutup menu"
        >
          <Icon name="close" />
        </button>
      </div>

      <div className="px-3 pt-3">
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

      <nav aria-label="Menu admin" className="mt-2 flex-1 overflow-y-auto px-3 pb-4">
        {groups.length === 0 && <p className="px-2 py-4 text-sm text-slate-400">Tidak ada menu yang cocok.</p>}
        {groups.map((g) => (
          <div key={g.group} className="mt-3" data-group={g.group}>
            <div className="px-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-slate-500">{g.group}</div>
            <ul className="space-y-0.5">
              {g.items.map((it) => {
                const badge = badgeOf(it);
                return (
                  <li key={it.to}>
                    <NavLink
                      to={it.to}
                      end={it.end}
                      onClick={pick}
                      className={({ isActive }) =>
                        `group flex items-center gap-3 rounded-md border-l-4 px-2.5 py-2 text-sm font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400 ${
                          isActive
                            ? 'border-purple-400 bg-purple-600/25 text-white'
                            : 'border-transparent text-slate-300 hover:bg-slate-800 hover:text-white'
                        }`
                      }
                    >
                      <Icon name={it.icon} className="h-5 w-5 shrink-0 opacity-90" />
                      <span className="flex-1 truncate">{it.label}</span>
                      {badge !== null && (
                        <span
                          className="rounded-full bg-amber-400 px-2 py-0.5 text-[11px] font-bold leading-none text-slate-900"
                          aria-label={`${badge} menunggu pembayaran`}
                          title={`${badge} pesanan menunggu pembayaran`}
                        >
                          {badge}
                        </span>
                      )}
                    </NavLink>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      <div className="border-t border-slate-800 px-4 py-3 text-xs">
        <div className="text-slate-400">
          Masuk sebagai <span className="break-all font-medium text-slate-200">{me?.email || '-'}</span>
        </div>
        <a
          href="/"
          className="mt-2 inline-flex items-center gap-1.5 rounded text-sm font-medium text-purple-300 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-400"
        >
          <Icon name="back" className="h-4 w-4" />
          Kembali ke toko
        </a>
        <div className="mt-2 text-[11px] text-slate-500">Mihan Store Admin {ADMIN_UI_VERSION}</div>
      </div>
    </aside>
  );
};

const Topbar = ({ me, open, onOpen, menuBtnRef }) => {
  const { pathname } = useLocation();
  const { title, crumbs } = breadcrumbFor(pathname);
  const email = me?.email || '';
  const short = email.split('@')[0] || 'admin';
  return (
    <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-gray-200 bg-white/95 px-4 backdrop-blur lg:px-8">
      <button
        type="button"
        ref={menuBtnRef}
        onClick={onOpen}
        className="-ml-1 rounded-md p-2 text-gray-600 hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 lg:hidden"
        aria-label="Buka menu"
        aria-controls="admin-sidebar"
        aria-expanded={open}
      >
        <Icon name="menu" className="h-6 w-6" />
      </button>
      <div className="min-w-0 flex-1">
        <p className="truncate text-base font-semibold text-gray-900" data-testid="admin-title">
          {title}
        </p>
        <nav aria-label="Breadcrumb" className="hidden sm:block">
          <ol className="flex items-center gap-1 text-xs text-gray-500">
            {crumbs.map((c, i) => {
              const last = i === crumbs.length - 1;
              return (
                <li key={`${i}-${c.label}`} className="flex items-center gap-1">
                  {i > 0 && <span aria-hidden="true">/</span>}
                  {last ? (
                    <span aria-current="page" className="font-medium text-gray-700">
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
        <span className="hidden max-w-[10rem] truncate text-sm text-gray-600 sm:inline">{short}</span>
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

const AdminLayout = ({ me, children }) => {
  const [open, setOpen] = useState(false);
  const { pathname } = useLocation();
  const menuBtnRef = useRef(null);
  const closeBtnRef = useRef(null);
  const asideRef = useRef(null);
  const wasOpen = useRef(false);

  const close = useCallback(() => setOpen(false), []);

  // Tutup laci saat rute berubah.
  useEffect(() => {
    setOpen(false);
  }, [pathname]);

  // Fokus: saat dibuka pindah ke tombol tutup; saat ditutup kembali ke tombol hamburger.
  useEffect(() => {
    if (open) {
      wasOpen.current = true;
      // Browser bisa menolak fokus selama laci masih dalam transisi tampil; coba ulang singkat.
      const focusClose = () => {
        if (asideRef.current && !asideRef.current.contains(document.activeElement)) closeBtnRef.current?.focus();
      };
      focusClose();
      const timers = [0, 60, 250].map((ms) => window.setTimeout(focusClose, ms));
      return () => timers.forEach((t) => window.clearTimeout(t));
    }
    if (wasOpen.current) {
      wasOpen.current = false;
      menuBtnRef.current?.focus();
    }
    return undefined;
  }, [open]);

  // Escape menutup, Tab dikurung di dalam laci, scroll halaman dikunci selama laci terbuka.
  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e) => {
      if (e.key === 'Escape') {
        setOpen(false);
      } else if (e.key === 'Tab' && asideRef.current) {
        const els = [...asideRef.current.querySelectorAll(FOCUSABLE)].filter((el) => el.offsetParent !== null || el === document.activeElement);
        if (els.length === 0) return;
        const first = els[0];
        const last = els[els.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [open]);

  // Bila layar melebar ke desktop, laci tidak relevan lagi.
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return undefined;
    const mq = window.matchMedia('(min-width: 1024px)');
    const onChange = (e) => e.matches && setOpen(false);
    mq.addEventListener?.('change', onChange);
    return () => mq.removeEventListener?.('change', onChange);
  }, []);

  return (
    <SummaryProvider>
      <div className="min-h-screen bg-gray-100 text-gray-800">
        <a
          href="#admin-main"
          className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-white focus:px-3 focus:py-2 focus:text-sm focus:shadow"
        >
          Lewati ke konten
        </a>
        <Sidebar me={me} open={open} onClose={close} closeBtnRef={closeBtnRef} asideRef={asideRef} />
        {open && (
          <div className="fixed inset-0 z-30 bg-slate-900/60 lg:hidden" onClick={close} aria-hidden="true" data-testid="admin-overlay" />
        )}
        <div className="flex min-h-screen min-w-0 flex-col lg:pl-[260px]">
          <Topbar me={me} open={open} onOpen={() => setOpen(true)} menuBtnRef={menuBtnRef} />
          <main id="admin-main" tabIndex={-1} className="w-full min-w-0 max-w-7xl flex-1 px-4 py-5 focus:outline-none sm:px-6 lg:px-8 lg:py-6">
            {children}
          </main>
        </div>
      </div>
    </SummaryProvider>
  );
};

export default AdminLayout;
