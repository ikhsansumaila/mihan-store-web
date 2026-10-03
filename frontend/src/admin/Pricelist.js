import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { adminFetch, qs } from './api';
import { ErrorBox, cardClass, inputClass, btnPrimary, btnSecondary } from './ui';
import { Icon } from './icons';
import {
  THEMES,
  DEFAULT_THEME,
  DEFAULT_TITLE,
  PRICE_MODES,
  DEFAULT_PRICE_MODE,
  NOTE_MAX,
  TITLE_MAX,
  DATE_MAX,
  URL_MAX,
  defaultDateText,
  defaultSelection,
  catalogForSelection,
  groupProducts,
  formatRupiah,
  fileNameFor,
  buildShareText,
  normalizeOrderUrl,
  defaultOrderUrl,
  waShareUrl,
} from '../pricelist/layout';
import { renderPricelist, canvasToBlob, loadLogo, loadPhotos } from '../pricelist/render';
import { canShareFiles, copyText, downloadAll, downloadBlob, shareFiles, toFiles } from '../pricelist/share';

// Halaman Pricelist: pilih produk, atur judul/tanggal/catatan/tema/kolom, pratinjau PNG langsung (dibuat di
// browser dengan Canvas 2D), unduh PNG, dan bagikan ke WhatsApp (Web Share API di HP; di komputer: unduh +
// buka wa.me dengan teks pendamping). Pemesanan diarahkan ke alamat web toko (bukan nomor WhatsApp).
// Tidak ada perubahan data di backend.

const MAX_PAGES_FETCH = 50;

const fetchAllProducts = async () => {
  const all = [];
  for (let page = 1; page <= MAX_PAGES_FETCH; page += 1) {
    // eslint-disable-next-line no-await-in-loop
    const r = await adminFetch(`/products${qs({ page, per_page: 100 })}`);
    const items = (r && r.items) || [];
    all.push(...items);
    const total = Number(r && r.total) || 0;
    if (!items.length || all.length >= total) break;
  }
  return all;
};

const LOGO_TIMEOUT_MS = 4000;
const loadLogoWithTimeout = () =>
  Promise.race([loadLogo(), new Promise((r) => setTimeout(() => r(null), LOGO_TIMEOUT_MS))]);

const Label = ({ htmlFor, children, hint }) => (
  <label htmlFor={htmlFor} className="mb-1 block text-sm font-semibold text-gray-700">
    {children}
    {hint && <span className="ml-1 font-normal text-gray-500">{hint}</span>}
  </label>
);

const CategoryChecklist = ({ group, selected, onToggle, onSetMany }) => {
  const ref = useRef(null);
  const ids = group.items.map((it) => it.id);
  const activeIds = group.items.filter((it) => it.active).map((it) => it.id);
  const count = ids.filter((id) => selected.has(id)).length;
  // Tercentang penuh bila semua produk aktif terpilih (kategori tanpa produk aktif: semua produknya).
  const target = activeIds.length ? activeIds : ids;
  const checked = count > 0 && target.every((id) => selected.has(id));
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = count > 0 && !checked;
  }, [count, checked]);
  const headId = `pl-cat-${group.key}`;
  return (
    <fieldset className="min-w-0 rounded-md border border-gray-200" data-category={group.name}>
      <legend className="sr-only">Kategori {group.name}</legend>
      <div className="flex flex-wrap items-center gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2">
        <label htmlFor={headId} className="flex min-w-[10rem] flex-1 items-center gap-2 text-sm font-semibold text-gray-800">
          <input
            id={headId}
            ref={ref}
            type="checkbox"
            className="h-4 w-4 shrink-0 accent-purple-700"
            checked={checked}
            onChange={(e) => onSetMany(e.target.checked ? target : ids, e.target.checked)}
            aria-label={`Kategori ${group.name}: pilih semua`}
          />
          <span className="min-w-0 break-words">{group.name}</span>
          <span className="shrink-0 font-normal text-gray-500">
            ({count}/{ids.length})
          </span>
        </label>
        <div className="flex shrink-0 gap-1">
          <button
            type="button"
            className="rounded px-2 py-1 text-xs font-medium text-purple-700 hover:bg-purple-50"
            onClick={() => onSetMany(activeIds, true)}
            aria-label={`Pilih semua produk aktif di ${group.name}`}
          >
            Pilih semua
          </button>
          <button
            type="button"
            className="rounded px-2 py-1 text-xs font-medium text-gray-600 hover:bg-gray-100"
            onClick={() => onSetMany(ids, false)}
            aria-label={`Kosongkan pilihan di ${group.name}`}
          >
            Kosongkan
          </button>
        </div>
      </div>
      <ul className="divide-y divide-gray-100">
        {group.items.map((it) => {
          const id = `pl-p-${it.id}`;
          return (
            <li key={it.id}>
              <label htmlFor={id} className="flex min-w-0 cursor-pointer items-start gap-2 px-3 py-2 text-sm hover:bg-gray-50">
                <input id={id} type="checkbox" className="mt-0.5 h-4 w-4 shrink-0 accent-purple-700" checked={selected.has(it.id)} onChange={() => onToggle(it.id)} />
                <span className="min-w-0 flex-1 break-words text-gray-800">
                  {it.name}
                  {!it.active && (
                    <span className="ml-1.5 inline-block rounded bg-gray-200 px-1.5 py-0.5 align-middle text-[11px] font-semibold text-gray-700" data-inactive="1">
                      Nonaktif
                    </span>
                  )}
                </span>
                <span className="shrink-0 text-right font-semibold tabular-nums text-gray-700">
                  {formatRupiah(it.price)} <span className="text-xs font-normal text-gray-500">/ {it.unit}</span>
                  {it.tiers.length > 0 && (
                    <span className="block text-[11px] font-semibold text-amber-700" data-tiers="1">
                      Grosir ({it.tiers.length} jenjang)
                    </span>
                  )}
                </span>
              </label>
            </li>
          );
        })}
      </ul>
    </fieldset>
  );
};

const Pricelist = () => {
  const [catalog, setCatalog] = useState(null); // { products, categories }
  const [loadError, setLoadError] = useState(null);
  const [orderUrl, setOrderUrl] = useState(() => defaultOrderUrl());
  // Alamat valid terakhir: dipakai pratinjau & teks pendamping selama isian sedang tidak valid.
  const [renderUrl, setRenderUrl] = useState(() => (normalizeOrderUrl(defaultOrderUrl()) || { href: '' }).href);
  const [title, setTitle] = useState(DEFAULT_TITLE);
  const [dateText, setDateText] = useState(() => defaultDateText(new Date()));
  const [note, setNote] = useState('');
  const [theme, setTheme] = useState(DEFAULT_THEME);
  const [columns, setColumns] = useState(1);
  const [priceMode, setPriceMode] = useState(DEFAULT_PRICE_MODE);
  // Opsi foto produk di gambar (bawaan mati). Thumbnail dimuat same-origin sebelum menggambar.
  const [showPhotos, setShowPhotos] = useState(false);
  const photoCacheRef = useRef(new Map());
  const [selected, setSelected] = useState(() => new Set());
  const [customText, setCustomText] = useState(null);
  const [logo, setLogo] = useState(undefined); // undefined = sedang dimuat
  const [preview, setPreview] = useState({ pages: [], busy: true, error: null });
  const [shareMsg, setShareMsg] = useState(null);
  const [copied, setCopied] = useState(false);
  const [fallbackShown, setFallbackShown] = useState(false);
  const today = useMemo(() => new Date(), []);
  const urlsRef = useRef([]);

  useEffect(() => {
    let alive = true;
    Promise.all([fetchAllProducts(), adminFetch('/categories')])
      .then(([products, cats]) => {
        if (!alive) return;
        setCatalog({ products, categories: (cats && cats.items) || [] });
        setSelected(defaultSelection(products));
      })
      .catch((err) => alive && setLoadError(err));
    loadLogoWithTimeout().then((img) => alive && setLogo(img || null));
    return () => {
      alive = false;
    };
  }, []);

  // Lepas object URL pratinjau saat halaman ditutup.
  useEffect(
    () => () => {
      urlsRef.current.forEach((u) => URL.revokeObjectURL(u));
      urlsRef.current = [];
    },
    []
  );

  const selectionGroups = useMemo(() => (catalog ? catalogForSelection(catalog.products, catalog.categories) : []), [catalog]);
  const groups = useMemo(() => (catalog ? groupProducts(catalog.products, catalog.categories, selected) : []), [catalog, selected]);
  const selectedCount = groups.reduce((s, g) => s + g.items.length, 0);
  const inactiveSelected = groups.reduce((s, g) => s + g.items.filter((it) => !it.active).length, 0);
  const tieredSelected = groups.reduce((s, g) => s + g.items.filter((it) => it.tiers.length > 0).length, 0);
  const url = normalizeOrderUrl(orderUrl);
  const urlInvalid = !url;
  // Alamat tidak valid: tombol unduh/bagikan dinonaktifkan sampai diperbaiki.
  const autoText = buildShareText({ title, dateText, note, orderUrl: renderUrl });
  const shareText = customText ?? autoText;
  const canShare = useMemo(() => {
    try {
      return canShareFiles([new File([''], 'pricelist.png', { type: 'image/png' })]);
    } catch {
      return false;
    }
  }, []);

  // Pratinjau langsung: gambar ulang (dengan jeda kecil) setiap pengaturan/pilihan berubah.
  useEffect(() => {
    if (!catalog || logo === undefined) return undefined;
    if (!groups.length) {
      urlsRef.current.forEach((u) => URL.revokeObjectURL(u));
      urlsRef.current = [];
      setPreview({ pages: [], busy: false, error: null });
      return undefined;
    }
    let cancelled = false;
    setPreview((p) => ({ ...p, busy: true }));
    const t = setTimeout(async () => {
      try {
        const photos = showPhotos
          ? await loadPhotos(
              groups.flatMap((g) => g.items),
              { cache: photoCacheRef.current }
            )
          : null;
        if (cancelled) return;
        const rendered = renderPricelist(
          { groups, columns, title, dateText, note, orderUrl: renderUrl, theme, priceMode, showPhotos },
          { logo, photos }
        );
        const blobs = await Promise.all(rendered.map((r) => canvasToBlob(r.canvas)));
        if (cancelled) return;
        const pages = rendered.map((r, i) => ({
          blob: blobs[i],
          url: URL.createObjectURL(blobs[i]),
          name: fileNameFor(today, i),
          width: r.width,
          height: r.height,
        }));
        urlsRef.current.forEach((u) => URL.revokeObjectURL(u));
        urlsRef.current = pages.map((p) => p.url);
        setPreview({ pages, busy: false, error: null });
      } catch (err) {
        if (!cancelled) setPreview({ pages: [], busy: false, error: err });
      }
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [catalog, groups, columns, title, dateText, note, renderUrl, theme, priceMode, showPhotos, logo, today]);

  const toggle = useCallback((id) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);
  const setMany = useCallback((ids, on) => {
    setSelected((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => (on ? next.add(id) : next.delete(id)));
      return next;
    });
  }, []);
  const selectAllActive = () => catalog && setSelected(defaultSelection(catalog.products));
  const clearAll = () => setSelected(new Set());

  const pages = preview.pages;
  const ready = pages.length > 0 && !preview.busy && !urlInvalid;

  const onDownload = () => {
    setShareMsg(null);
    downloadAll(pages);
  };

  const onShare = async () => {
    setShareMsg(null);
    if (!pages.length) return;
    const files = toFiles(pages);
    if (canShareFiles(files)) {
      try {
        const r = await shareFiles(files, shareText);
        if (r === 'shared') setShareMsg({ type: 'ok', text: 'Menu berbagi selesai. Pilih WhatsApp lalu kontak, grup, atau status.' });
      } catch {
        setShareMsg({
          type: 'error',
          text: 'Maaf, gambar tidak bisa dibagikan langsung dari perangkat ini. Gunakan "Unduh PNG", lalu kirim file-nya lewat WhatsApp.',
        });
      }
      return;
    }
    // Komputer / browser tanpa berbagi file: unduh PNG lalu buka WhatsApp dengan teks pendamping.
    downloadAll(pages);
    window.open(waShareUrl(shareText), '_blank', 'noopener,noreferrer');
    setFallbackShown(true);
  };

  const onCopy = async () => {
    const ok = await copyText(shareText);
    setCopied(ok ? 'ok' : 'fail');
    setTimeout(() => setCopied(false), 2500);
  };

  if (loadError) return <ErrorBox error={loadError} />;
  if (!catalog) return <p className="text-gray-500">Memuat...</p>;

  const noteLeft = NOTE_MAX - note.length;

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 sm:text-2xl">Pricelist</h1>
        <p className="mt-1 text-sm text-gray-600">
          Buat gambar PNG daftar harga dari katalog, lalu unduh atau bagikan ke WhatsApp. Gambar dibuat langsung di browser; data produk
          tidak diubah.
        </p>
      </div>

      <div className="grid min-w-0 grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        {/* Pengaturan */}
        <section aria-labelledby="pl-settings" className={`${cardClass} min-w-0 space-y-4 p-4 sm:p-5 lg:col-start-1`}>
          <h2 id="pl-settings" className="text-sm font-semibold uppercase tracking-wide text-gray-500">
            Pengaturan
          </h2>
          <div>
            <Label htmlFor="pl-title">Judul</Label>
            <input id="pl-title" className={inputClass} maxLength={TITLE_MAX} value={title} onChange={(e) => setTitle(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="pl-date">Keterangan tanggal</Label>
            <input id="pl-date" className={inputClass} maxLength={DATE_MAX} value={dateText} onChange={(e) => setDateText(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="pl-note" hint="(opsional, mis. promo)">
              Catatan
            </Label>
            <textarea
              id="pl-note"
              className={inputClass}
              rows={2}
              maxLength={NOTE_MAX}
              value={note}
              onChange={(e) => setNote(e.target.value.slice(0, NOTE_MAX))}
              placeholder="Contoh: Gratis ongkir area Tangerang untuk pembelian di atas Rp 200.000"
              aria-describedby="pl-note-count"
            />
            <p id="pl-note-count" className="mt-1 text-xs text-gray-500">
              Sisa {noteLeft} dari {NOTE_MAX} karakter
            </p>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <fieldset className="min-w-0">
              <legend className="mb-1 block text-sm font-semibold text-gray-700">Tema warna</legend>
              <div className="flex flex-wrap gap-2">
                {Object.entries(THEMES).map(([k, t]) => (
                  <label
                    key={k}
                    className={`flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm focus-within:ring-2 focus-within:ring-purple-400 ${
                      theme === k ? 'border-purple-600 bg-purple-50 font-semibold text-gray-900' : 'border-gray-300 text-gray-700'
                    }`}
                  >
                    <input type="radio" name="pl-theme" value={k} checked={theme === k} onChange={() => setTheme(k)} className="sr-only" />
                    <span aria-hidden="true" className="h-4 w-4 rounded-full" style={{ backgroundColor: t.accent }} />
                    {t.label}
                  </label>
                ))}
              </div>
            </fieldset>
            <fieldset className="min-w-0">
              <legend className="mb-1 block text-sm font-semibold text-gray-700">Tata letak</legend>
              <div className="flex flex-wrap gap-2">
                {[1, 2].map((n) => (
                  <label
                    key={n}
                    className={`flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm ${
                      columns === n ? 'border-purple-600 bg-purple-50 font-semibold text-gray-900' : 'border-gray-300 text-gray-700'
                    }`}
                  >
                    <input type="radio" name="pl-cols" value={n} checked={columns === n} onChange={() => setColumns(n)} className="accent-purple-700" />
                    {n} kolom
                  </label>
                ))}
              </div>
            </fieldset>
          </div>
          <fieldset className="min-w-0">
            <legend className="mb-1 block text-sm font-semibold text-gray-700">Tampilan harga</legend>
            <div className="flex flex-wrap gap-2">
              {Object.entries(PRICE_MODES).map(([k, label]) => (
                <label
                  key={k}
                  className={`flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm ${
                    priceMode === k ? 'border-purple-600 bg-purple-50 font-semibold text-gray-900' : 'border-gray-300 text-gray-700'
                  }`}
                >
                  <input type="radio" name="pl-price-mode" value={k} checked={priceMode === k} onChange={() => setPriceMode(k)} className="accent-purple-700" />
                  {label}
                </label>
              ))}
            </div>
            <p className="mt-1 text-xs text-gray-500" data-testid="pl-mode-help">
              {tieredSelected > 0
                ? `${tieredSelected} produk terpilih punya harga grosir; jenjang tampil sebagai teks kecil di bawah nama produk (mis. "10+ : Rp 42.000"). Produk tanpa jenjang tampil seperti biasa.`
                : 'Belum ada produk terpilih yang punya harga grosir; gambar hanya memuat harga eceran.'}
            </p>
          </fieldset>
          <div>
            <label htmlFor="pl-photos" className="flex cursor-pointer items-start gap-2 text-sm text-gray-800">
              <input
                id="pl-photos"
                type="checkbox"
                className="mt-0.5 h-5 w-5 shrink-0 accent-purple-700"
                checked={showPhotos}
                onChange={(e) => setShowPhotos(e.target.checked)}
                data-testid="pl-photos"
              />
              <span>
                <span className="font-semibold">Tampilkan foto produk</span>
                <span className="block text-xs text-gray-500">
                  Foto kecil di kiri tiap produk (bawaan mati). Produk tanpa foto mendapat kotak kosong. Gambar menjadi lebih panjang.
                </span>
              </span>
            </label>
          </div>
          <div>
            <Label htmlFor="pl-url" hint="(untuk pelanggan memesan)">
              Alamat web pemesanan
            </Label>
            <input
              id="pl-url"
              type="url"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              className={urlInvalid ? inputClass.replace('border-gray-300', 'border-red-500') : inputClass}
              maxLength={URL_MAX}
              value={orderUrl}
              onChange={(e) => {
                const v = e.target.value;
                setOrderUrl(v);
                const n = normalizeOrderUrl(v);
                if (n) setRenderUrl(n.href);
              }}
              aria-invalid={urlInvalid}
              aria-describedby="pl-url-help"
              placeholder="https://store.mihan.web.id"
            />
            <p id="pl-url-help" className={`mt-1 text-xs ${urlInvalid ? 'text-red-700' : 'text-gray-500'}`} data-testid="pl-url-help">
              {urlInvalid ? (
                'Alamat tidak valid: gunakan http:// atau https:// tanpa spasi, mis. https://store.mihan.web.id.'
              ) : (
                <>
                  Footer gambar: Pesan online: <span className="font-medium text-gray-700">{url.display}</span>. Teks pendamping memakai{' '}
                  <span className="break-all font-medium text-gray-700">{url.href}</span>.
                </>
              )}
            </p>
          </div>
        </section>

        {/* Pratinjau + aksi */}
        <section aria-labelledby="pl-preview" className={`${cardClass} min-w-0 p-4 sm:p-5 lg:col-start-2 lg:row-span-2 lg:row-start-1 lg:self-start`}>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 id="pl-preview" className="text-sm font-semibold uppercase tracking-wide text-gray-500">
              Pratinjau PNG
            </h2>
            <span className="text-xs text-gray-500" aria-live="polite" data-testid="pl-status">
              {preview.busy
                ? 'Memperbarui pratinjau...'
                : pages.length
                ? `${pages.length} gambar · ${selectedCount} produk`
                : 'Belum ada gambar'}
            </span>
          </div>

          <div className="flex flex-col gap-2 sm:flex-row">
            <button type="button" className={`${btnPrimary} sm:flex-1`} onClick={onShare} disabled={!ready}>
              <Icon name="share" className="h-5 w-5" />
              Bagikan ke WhatsApp
            </button>
            <button type="button" className={`${btnSecondary} sm:flex-1`} onClick={onDownload} disabled={!ready}>
              <Icon name="download" className="h-5 w-5" />
              {pages.length > 1 ? `Unduh PNG (${pages.length} file)` : 'Unduh PNG'}
            </button>
          </div>
          <p className="mt-2 text-xs text-gray-600" data-testid="pl-share-hint">
            {canShare
              ? 'Di HP, gambar langsung terlampir: pilih WhatsApp, lalu kontak, grup, atau status.'
              : 'WhatsApp Web tidak bisa menerima gambar dari tombol web: lampirkan file PNG yang baru diunduh secara manual.'}
            {pages.length > 1 && ' Bila browser bertanya, izinkan pengunduhan beberapa file sekaligus, atau unduh per halaman di bawah.'}
          </p>
          {urlInvalid && (
            <p role="alert" className="mt-2 text-sm text-red-700" data-testid="pl-url-blocked">
              Perbaiki "Alamat web pemesanan" dulu sebelum mengunduh atau membagikan.
            </p>
          )}
          {fallbackShown && (
            <div role="status" className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900" data-testid="pl-fallback">
              <p className="font-semibold">WhatsApp dibuka di tab baru dengan teks pendamping.</p>
              <p className="mt-1">
                WhatsApp Web tidak bisa menerima gambar dari tombol web: lampirkan file PNG yang baru diunduh ({pages.map((p) => p.name).join(', ')})
                secara manual lewat ikon lampiran di WhatsApp. Bila tab tidak terbuka, izinkan pop-up atau salin teks pendamping di bawah.
              </p>
            </div>
          )}
          {shareMsg && (
            <div
              role={shareMsg.type === 'error' ? 'alert' : 'status'}
              className={`mt-3 rounded-md border px-3 py-2 text-sm ${
                shareMsg.type === 'error' ? 'border-red-200 bg-red-50 text-red-700' : 'border-green-200 bg-green-50 text-green-800'
              }`}
            >
              {shareMsg.text}
            </div>
          )}

          <div className="mt-4">
            <Label htmlFor="pl-share-text">Teks pendamping</Label>
            <textarea
              id="pl-share-text"
              className={inputClass}
              rows={5}
              maxLength={1000}
              value={shareText}
              onChange={(e) => setCustomText(e.target.value)}
            />
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <button type="button" className={btnSecondary} onClick={onCopy}>
                <Icon name="copy" className="h-4 w-4" />
                Salin teks pendamping
              </button>
              {customText !== null && (
                <button type="button" className="text-sm text-purple-700 hover:underline" onClick={() => setCustomText(null)}>
                  Kembalikan teks otomatis
                </button>
              )}
              <span aria-live="polite" className="text-sm">
                {copied === 'ok' && <span className="text-green-700">Teks tersalin</span>}
                {copied === 'fail' && <span className="text-red-700">Gagal menyalin, salin manual dari kotak teks</span>}
              </span>
            </div>
          </div>

          {preview.error && (
            <div className="mt-4">
              <ErrorBox error={preview.error} />
            </div>
          )}
          {!groups.length && (
            <p className="mt-4 rounded-md border border-dashed border-gray-300 px-3 py-6 text-center text-sm text-gray-500">
              Belum ada produk dipilih. Centang minimal satu produk.
            </p>
          )}
          <div className={`mt-4 space-y-4 transition-opacity ${preview.busy ? 'opacity-60' : ''}`} data-testid="pl-previews">
            {pages.map((p, i) => (
              <figure key={p.url} className="min-w-0">
                <div className="overflow-hidden rounded-md border border-gray-200 bg-gray-50 shadow-sm">
                  <img
                    src={p.url}
                    alt={`Pratinjau pricelist halaman ${i + 1} dari ${pages.length}`}
                    width={p.width}
                    height={p.height}
                    className="block h-auto w-full"
                  />
                </div>
                <figcaption className="mt-1 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-600">
                  <span>
                    Halaman {i + 1}/{pages.length} · {p.width}×{p.height} px
                  </span>
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 rounded px-2 py-1 font-medium text-purple-700 hover:bg-purple-50 disabled:opacity-50"
                    onClick={() => downloadBlob(p.blob, p.name)}
                    disabled={urlInvalid}
                    aria-label={`Unduh halaman ${i + 1} (${p.name})`}
                  >
                    <Icon name="download" className="h-4 w-4" />
                    Unduh halaman {i + 1}
                  </button>
                </figcaption>
              </figure>
            ))}
          </div>
        </section>

        {/* Pilihan produk */}
        <section aria-labelledby="pl-products" className={`${cardClass} min-w-0 p-4 sm:p-5 lg:col-start-1`}>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 id="pl-products" className="text-sm font-semibold uppercase tracking-wide text-gray-500">
              Produk ({selectedCount} dipilih)
            </h2>
            <div className="flex gap-2">
              <button type="button" className={btnSecondary} onClick={selectAllActive}>
                Pilih semua
              </button>
              <button type="button" className={btnSecondary} onClick={clearAll}>
                Kosongkan
              </button>
            </div>
          </div>
          <p className="mb-3 text-xs text-gray-500">
            Bawaan: semua produk aktif. "Pilih semua" hanya memilih produk aktif; produk nonaktif ikut bila dicentang manual.
          </p>
          {inactiveSelected > 0 && (
            <p className="mb-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900" data-testid="pl-inactive-warning">
              {inactiveSelected} produk nonaktif ikut dicantumkan di gambar (tidak tampil di toko).
            </p>
          )}
          {selectionGroups.length === 0 ? (
            <p className="text-sm text-gray-500">Belum ada produk di katalog.</p>
          ) : (
            <div className="space-y-3">
              {selectionGroups.map((g) => (
                <CategoryChecklist key={g.key} group={g} selected={selected} onToggle={toggle} onSetMany={setMany} />
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
};

export default Pricelist;
