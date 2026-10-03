import React, { useEffect, useId, useMemo, useRef, useState } from 'react';
import { filterRegions } from './regionsApi';

// Kolom pilihan wilayah yang bisa diketik untuk mencari (combobox ARIA 1.2).
// Nyaman di HP: huruf 16px (tidak memicu zoom iOS), tinggi kolom & tiap pilihan >= 44px,
// daftar bisa di-scroll, tombol panah 44px untuk membuka/menutup.
const MAX_SHOWN = 300;

const RegionCombobox = ({
  id,
  label,
  lower,
  value, // {code, name} | null
  items,
  loading,
  error,
  onRetry,
  onChange,
  disabled,
  disabledHint,
  invalid,
  errorText,
  inputRef,
}) => {
  const autoId = useId();
  const baseId = id || `rc${autoId.replace(/:/g, '')}`;
  const listId = `${baseId}-list`;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [typing, setTyping] = useState(false);
  const [active, setActive] = useState(0);
  const localRef = useRef(null);
  const ref = inputRef || localRef;
  const listRef = useRef(null);
  const wrapRef = useRef(null);

  const shown = useMemo(() => filterRegions(items || [], typing ? query : '').slice(0, MAX_SHOWN), [items, query, typing]);

  // Saat dibuka, pilihan aktif = nilai terpilih (bila ada).
  const selectedIndex = () => {
    const i = value ? (items || []).slice(0, MAX_SHOWN).findIndex((it) => it.code === value.code) : -1;
    return i >= 0 ? i : 0;
  };

  useEffect(() => {
    if (!open || !listRef.current) return;
    const el = listRef.current.querySelector(`[data-index="${active}"]`);
    if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ block: 'nearest' });
  }, [active, open]);

  const isDisabled = disabled || loading;
  const close = () => {
    setOpen(false);
    setTyping(false);
    setQuery('');
  };
  const choose = (it) => {
    onChange(it);
    close();
  };
  const openList = () => {
    if (isDisabled) return;
    if (!open) setActive(selectedIndex());
    setOpen(true);
    // Layar sempit: geser kolom ke atas agar daftar tidak tertutup keyboard.
    if (typeof window !== 'undefined' && window.innerWidth < 640 && wrapRef.current?.scrollIntoView) {
      wrapRef.current.scrollIntoView({ block: 'start', behavior: 'smooth' });
    }
  };

  const onKeyDown = (e) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!open) openList();
      else setActive((a) => Math.min(a + 1, shown.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === 'Enter') {
      if (open) {
        e.preventDefault();
        if (shown[active]) choose(shown[active]);
      }
    } else if (e.key === 'Escape') {
      if (open) {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
    } else if (e.key === 'Tab') {
      close();
    }
  };

  const placeholder = disabled ? disabledHint : loading ? 'Memuat...' : `Ketik untuk mencari ${lower}`;
  const display = typing ? query : value?.name || '';
  const describedBy = [errorText || error ? `${baseId}-err` : null, `${baseId}-help`].filter(Boolean).join(' ');

  return (
    <div ref={wrapRef} className="relative" data-region-field={id}>
      <label htmlFor={baseId} className="block text-sm font-semibold text-gray-700 mb-1">
        {label} <span className="text-red-600" aria-hidden="true">*</span>
      </label>
      <div className="relative">
        <input
          ref={ref}
          id={baseId}
          type="text"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={listId}
          aria-activedescendant={open && shown[active] ? `${baseId}-opt-${active}` : undefined}
          aria-invalid={invalid ? 'true' : undefined}
          aria-required="true"
          aria-describedby={describedBy}
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          disabled={isDisabled}
          placeholder={placeholder}
          value={display}
          onChange={(e) => {
            setQuery(e.target.value);
            setTyping(true);
            if (!open) setOpen(true);
            setActive(0);
          }}
          onFocus={() => {
            if (isDisabled) return;
            setActive(selectedIndex());
            setOpen(true);
          }}
          onClick={openList}
          onBlur={() => setTimeout(close, 120)}
          onKeyDown={onKeyDown}
          className={`w-full min-h-[44px] pl-3 pr-12 py-2 border-2 rounded-lg text-base bg-white focus:outline-none transition disabled:bg-gray-100 disabled:text-gray-500 ${
            invalid ? 'border-red-500 focus:border-red-600' : 'border-gray-300 focus:border-purple-600'
          }`}
        />
        <button
          type="button"
          tabIndex={-1}
          aria-label={open ? `Tutup daftar ${lower}` : `Buka daftar ${lower}`}
          disabled={isDisabled}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            if (open) close();
            else {
              openList();
              ref.current?.focus();
            }
          }}
          className="absolute inset-y-0 right-0 flex w-11 min-w-[44px] items-center justify-center text-gray-500 disabled:text-gray-300"
        >
          {loading ? (
            <span className="h-5 w-5 animate-spin rounded-full border-2 border-purple-300 border-t-purple-700" data-testid="region-loading" />
          ) : (
            <svg viewBox="0 0 20 20" fill="currentColor" className={`h-5 w-5 transition ${open ? 'rotate-180' : ''}`} aria-hidden="true">
              <path fillRule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.17l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clipRule="evenodd" />
            </svg>
          )}
        </button>
      </div>
      <span id={`${baseId}-help`} className="sr-only">
        {loading ? `Memuat daftar ${lower}` : disabled ? disabledHint : `${(items || []).length} pilihan. Ketik untuk mencari.`}
      </span>
      {open && !isDisabled && (
        <ul
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label={`Daftar ${lower}`}
          className="absolute z-30 mt-1 max-h-72 w-full overflow-y-auto overscroll-contain rounded-lg border border-gray-200 bg-white py-1 shadow-lg"
        >
          {shown.length === 0 && (
            <li className="px-3 py-3 text-sm text-gray-500" role="presentation">
              {(items || []).length === 0 ? `Daftar ${lower} kosong` : `Tidak ada ${lower} yang cocok`}
            </li>
          )}
          {shown.map((it, i) => {
            const selected = value?.code === it.code;
            return (
              <li
                key={it.code}
                id={`${baseId}-opt-${i}`}
                data-index={i}
                role="option"
                aria-selected={selected}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => choose(it)}
                onMouseEnter={() => setActive(i)}
                className={`flex min-h-[44px] cursor-pointer items-center px-3 py-2 text-base ${
                  i === active ? 'bg-purple-50 text-purple-900' : 'text-gray-800'
                } ${selected ? 'font-semibold' : ''}`}
              >
                {it.name}
              </li>
            );
          })}
        </ul>
      )}
      {error ? (
        <p id={`${baseId}-err`} className="mt-1 flex flex-wrap items-center gap-2 text-sm text-red-700" role="alert">
          <span>{error}</span>
          {onRetry && (
            <button type="button" onClick={onRetry} className="min-h-[40px] rounded-md border border-red-300 px-3 font-semibold hover:bg-red-50">
              Coba lagi
            </button>
          )}
        </p>
      ) : (
        errorText && (
          <p id={`${baseId}-err`} className="mt-1 text-sm text-red-700">
            {errorText}
          </p>
        )
      )}
    </div>
  );
};

export default RegionCombobox;
