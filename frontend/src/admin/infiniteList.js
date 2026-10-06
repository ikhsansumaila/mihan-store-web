import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { adminFetch, qs } from './api';
import { btnSecondary } from './ui';

// Daftar admin bergulir tanpa batas (gaya aplikasi HP) di atas API berhalaman yang sudah ada
// (?page=&per_page=). Halaman berikutnya dimuat saat sentinel di bawah daftar mendekati layar.
//
// - Ganti filter (params) = daftar dikosongkan dan dimuat ulang dari halaman 1. Respons permintaan lama
//   diabaikan (nomor generasi), jadi respons basi tidak menimpa yang baru.
// - Hanya satu permintaan berjalan sekaligus: sentinel yang memicu dua kali tidak menggandakan halaman.
// - Item digabung tanpa duplikat berdasarkan id (backend memakai OFFSET; pesanan baru bisa menggeser data).
// - refresh(): memuat ulang halaman 1..N yang sudah tampil (setelah ubah/hapus) tanpa mengosongkan daftar.
// - Opsional cacheId: snapshot sekali pakai (rememberList) agar kembali dari detail tidak kehilangan
//   daftar dan posisi gulir; daftar lalu disegarkan diam-diam.

export const PER_PAGE = 20;

const snapshots = new Map();
const SNAPSHOT_TTL_MS = 10 * 60 * 1000;
export const clearListSnapshots = () => snapshots.clear();
const freshSnapshot = (cacheId) => {
  const snap = cacheId && snapshots.get(cacheId);
  return snap && Date.now() - snap.at < SNAPSHOT_TTL_MS ? snap : null;
};
// Data tambahan halaman (mis. filter) yang disimpan bersama snapshot, dibaca sebelum hook dipanggil.
export const peekListSnapshot = (cacheId) => freshSnapshot(cacheId);

const emptyState = (key) => ({ key, items: [], total: 0, page: 0, done: false, loading: true, error: null });

const mergeById = (prev, next) => {
  const seen = new Set(prev.map((it) => it.id));
  return prev.concat(next.filter((it) => !seen.has(it.id) && seen.add(it.id)));
};

const isDone = (res, count) => {
  const got = res?.items?.length || 0;
  return got < PER_PAGE || count >= (res?.total ?? 0);
};

export const useInfiniteList = (basePath, params, { cacheId, topRef } = {}) => {
  const key = `${basePath}${qs(params)}`;
  const pathFor = (page) => `${basePath}${qs({ ...params, page, per_page: PER_PAGE })}`;
  const pathRef = useRef(pathFor);
  pathRef.current = pathFor;

  const restoredRef = useRef(null);
  const [state, setState] = useState(() => {
    const snap = freshSnapshot(cacheId);
    if (cacheId) snapshots.delete(cacheId);
    if (snap && snap.state.key === key) {
      restoredRef.current = snap;
      return { ...snap.state, loading: false, error: null };
    }
    return emptyState(key);
  });
  const stateRef = useRef(state);
  stateRef.current = state;
  const genRef = useRef(0);
  const busyRef = useRef(false);

  const loadPage = useCallback(async (page) => {
    const gen = genRef.current;
    busyRef.current = true;
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      const res = await adminFetch(pathRef.current(page));
      if (gen !== genRef.current) return;
      setState((s) => {
        const items = page === 1 ? mergeById([], res?.items || []) : mergeById(s.items, res?.items || []);
        return { ...s, items, total: res?.total ?? items.length, page, done: isDone(res, items.length), loading: false, error: null };
      });
    } catch (err) {
      if (gen === genRef.current) setState((s) => ({ ...s, loading: false, error: err }));
    } finally {
      if (gen === genRef.current) busyRef.current = false;
    }
  }, []);

  const refresh = useCallback(async () => {
    const gen = ++genRef.current;
    busyRef.current = true;
    const pages = Math.max(1, stateRef.current.page);
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      let items = [];
      let res = null;
      let page = 0;
      let done = false;
      while (page < pages && !done) {
        page += 1;
        res = await adminFetch(pathRef.current(page));
        if (gen !== genRef.current) return;
        items = mergeById(items, res?.items || []);
        done = isDone(res, items.length);
      }
      const next = { items, total: res?.total ?? items.length, page, done, loading: false };
      setState((s) => ({ ...s, ...next }));
    } catch (err) {
      if (gen === genRef.current) setState((s) => ({ ...s, loading: false, error: err }));
    } finally {
      if (gen === genRef.current) busyRef.current = false;
    }
  }, []);

  // Filter berubah (atau pertama kali tampil): mulai dari halaman 1, gulir ke awal daftar bila sudah lewat.
  const firstRef = useRef(true);
  useEffect(() => {
    const first = firstRef.current;
    firstRef.current = false;
    if (first && restoredRef.current) {
      refresh();
      return;
    }
    genRef.current += 1;
    busyRef.current = false;
    setState(emptyState(key));
    if (!first) {
      const top = topRef?.current?.getBoundingClientRect?.().top;
      if (Number.isFinite(top) && top < 0) window.scrollTo?.({ top: Math.max(0, window.scrollY + top - 72) });
    }
    loadPage(1);
  }, [key]); // sengaja hanya key: loadPage/refresh stabil, path dibaca lewat pathRef

  // Kembali dari detail: pulihkan posisi gulir sekali.
  useLayoutEffect(() => {
    const snap = restoredRef.current;
    if (snap && Number.isFinite(snap.scrollY)) {
      try {
        window.scrollTo(0, snap.scrollY);
      } catch {
        /* jsdom / browser lama */
      }
    }
  }, []);

  useEffect(
    () => () => {
      genRef.current += 1; // abaikan respons yang datang setelah halaman ditutup
    },
    [],
  );

  const loadMore = useCallback(() => {
    const s = stateRef.current;
    if (busyRef.current || s.done || s.error || s.page === 0) return;
    loadPage(s.page + 1);
  }, [loadPage]);

  const retry = useCallback(() => {
    const s = stateRef.current;
    if (busyRef.current) return;
    loadPage(s.page + 1);
  }, [loadPage]);

  // Simpan snapshot sekali pakai (dipanggil saat membuka detail dari daftar).
  const remember = useCallback(
    (extra) => {
      if (!cacheId) return;
      const s = stateRef.current;
      if (s.page === 0) return;
      snapshots.set(cacheId, { state: { ...s, loading: false, error: null }, extra, scrollY: window.scrollY || 0, at: Date.now() });
    },
    [cacheId],
  );

  return { ...state, loadMore, retry, refresh, remember };
};

// Kaki daftar: sentinel IntersectionObserver (rootMargin 300px), status memuat (aria-live), pesan akhir,
// dan tombol "Muat lagi" bila gagal (atau bila browser tidak punya IntersectionObserver).
export const InfiniteFooter = ({ list, noun }) => {
  const sentinelRef = useRef(null);
  const visibleRef = useRef(false);
  const moreRef = useRef(list.loadMore);
  moreRef.current = list.loadMore;
  const hasIO = typeof window !== 'undefined' && typeof window.IntersectionObserver === 'function';

  useEffect(() => {
    const el = sentinelRef.current;
    if (!hasIO || !el) return undefined;
    const io = new window.IntersectionObserver(
      (entries) => {
        visibleRef.current = entries.some((e) => e.isIntersecting);
        if (visibleRef.current) moreRef.current();
      },
      { rootMargin: '300px 0px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [hasIO]);

  // Halaman masuk tetapi sentinel masih terlihat (layar tinggi / daftar pendek): lanjutkan memuat.
  useEffect(() => {
    if (visibleRef.current && !list.loading && !list.done && !list.error) moreRef.current();
  }, [list.loading, list.done, list.error, list.items.length]);

  const count = list.items.length;
  let message = '';
  if (list.loading) message = count ? `Memuat ${noun} berikutnya...` : `Memuat ${noun}...`;
  else if (list.error) message = `Gagal memuat ${noun}.`;
  else if (list.done && count) message = `Semua ${noun} sudah ditampilkan (${count}).`;
  const showMore = !list.loading && !list.done && (list.error || (!hasIO && count > 0));

  return (
    <div className="mt-3 pb-4 text-center text-sm text-gray-500" data-testid="infinite-footer">
      <p role="status" aria-live="polite" className="min-h-[1.25rem]">
        {list.loading && count > 0 && (
          <span aria-hidden="true" className="mr-2 inline-block h-3 w-3 animate-spin rounded-full border-2 border-purple-600 border-t-transparent align-[-2px]" />
        )}
        {message}
      </p>
      {showMore && (
        <button type="button" className={`${btnSecondary} mt-2`} onClick={list.retry}>
          Muat lagi
        </button>
      )}
      <div ref={sentinelRef} aria-hidden="true" data-testid="infinite-sentinel" className="h-px w-full" />
    </div>
  );
};
