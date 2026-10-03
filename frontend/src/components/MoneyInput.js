// Kolom isian nominal rupiah bersama: tampil "1.250.000" (titik setiap 3 digit, id-ID) saat diketik, tetapi nilai
// yang diteruskan ke pemanggil tetap digit polos ("1250000") dan angka bulat (1250000).
//
// Pemakaian:
//   <MoneyInput value={price} onValueChange={(digits, num) => setPrice(digits)} maxDigits={10} />
//   - value: string digit / angka bulat / '' / null (kosong). Karakter non-digit pada string diabaikan saat tampil.
//   - onValueChange(digits, number): digits = string digit tanpa nol di depan ('' bila kosong),
//     number = angka bulat (null bila kosong).
//   - maxDigits: batas keras jumlah digit (ketikan/tempelan yang melewati batas ditolak, nilai lama dipertahankan).
//   - min/max: batas lunak; kolom diberi aria-invalid="true" bila nilai di luar rentang. Pesan galat dan
//     penolakan saat simpan tetap urusan pemanggil (validasi form yang sudah ada tidak berubah).
//   - Atribut lain (id, name, placeholder, required, disabled, aria-*, className, onBlur, ref, ...) diteruskan.
//
// Aturan tempel (paste) - lihat parseMoneyText():
//   1. Bila teks berakhir dengan pemisah (koma/titik) + tepat 1-2 digit (boleh diikuti teks non-digit, mis. " IDR")
//      DAN bagian sebelumnya memuat pemisah ribuan (titik/koma di antara dua digit), bagian desimal itu dibuang:
//      "1.500.000,00" -> 1500000, "1,500,000.50" -> 1500000.
//   2. Selain itu semua karakter non-digit dibuang: "Rp 1.500.000" -> 1500000, "1,500,000" -> 1500000,
//      "1500000" -> 1500000, "12,5" -> 125 (tanpa pemisah ribuan tidak dianggap desimal).
//   3. Nol di depan dirapikan ("007" -> 7); teks tanpa digit sama sekali diabaikan (nilai tidak berubah).
// Aturan desimal hanya berlaku untuk tempelan; saat mengetik/menghapus, karakter non-digit selalu dibuang.
//
// Kursor: posisi dihitung ulang dari jumlah digit di kiri kursor, jadi tidak melompat ke akhir saat menyunting di
// tengah. Backspace tepat setelah titik menghapus digit sebelum titik; Delete tepat sebelum titik menghapus digit
// sesudahnya.
//
// Undo/redo: karena tampilan diformat ulang oleh skrip, riwayat undo asli browser tidak bisa dipakai; komponen
// menyimpan riwayatnya sendiri (Ctrl/Cmd+Z, Ctrl+Y / Ctrl/Cmd+Shift+Z, serta "Urungkan/Ulangi" dari menu konteks
// lewat beforeinput historyUndo/historyRedo). Riwayat dikosongkan bila nilai diubah dari luar (mis. form direset).
import React, { forwardRef, useCallback, useEffect, useLayoutEffect, useRef } from 'react';

export const onlyDigits = (s) => String(s ?? '').replace(/[^0-9]/g, '');
export const stripLeadingZeros = (d) => String(d).replace(/^0+(?=[0-9])/, '');

// Nilai pemanggil (angka/string/kosong) -> string digit ternormalisasi ('' bila kosong).
export const toMoneyDigits = (value) => {
  if (value === null || value === undefined || value === '') return '';
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return '';
    const n = Math.trunc(Math.abs(value));
    return Number.isSafeInteger(n) ? String(n) : '';
  }
  return stripLeadingZeros(onlyDigits(value));
};

// "1250000" / 1250000 -> "1.250.000"; kosong -> "".
export const formatMoney = (value) => toMoneyDigits(value).replace(/\B(?=([0-9]{3})+(?![0-9]))/g, '.');

// Digit mentah dari teks tempelan (tanpa merapikan nol di depan), dengan aturan desimal di atas.
export const extractPastedDigits = (text) => {
  const s = String(text ?? '');
  const m = s.match(/^(.*[0-9])[.,]([0-9]{1,2})[^0-9]*$/s);
  if (m && /[0-9][.,][0-9]/.test(m[1])) return onlyDigits(m[1]);
  return onlyDigits(s);
};

// Teks bebas (mis. tempelan) -> string digit ternormalisasi ('' bila tidak ada digit).
export const parseMoneyText = (text) => stripLeadingZeros(extractPastedDigits(text));

const countDigits = (s) => (String(s).match(/[0-9]/g) || []).length;

// Jumlah digit `a` sampai akhir bagian yang berbeda dari `b` (posisi kursor wajar setelah undo/redo).
const changedEnd = (a, b) => {
  let suffix = 0;
  while (suffix < a.length && suffix < b.length && a[a.length - 1 - suffix] === b[b.length - 1 - suffix]) suffix += 1;
  return a.length - suffix;
};

// Posisi kursor di teks berformat tepat setelah digit ke-k (k = 0 -> awal).
export const caretForDigitIndex = (display, k) => {
  if (k <= 0) return 0;
  let seen = 0;
  for (let i = 0; i < display.length; i += 1) {
    if (/[0-9]/.test(display[i])) {
      seen += 1;
      if (seen === k) return i + 1;
    }
  }
  return display.length;
};

const MoneyInput = forwardRef(function MoneyInput(
  { value, onValueChange, min, max, maxDigits = 15, onKeyDown, onPaste, onChange: _ignored, 'aria-invalid': ariaInvalid, ...rest },
  ref
) {
  const inputRef = useRef(null);
  const lastKey = useRef(null);
  const pendingCaret = useRef(null);
  const history = useRef({ undo: [], redo: [], expected: null });
  const digits = toMoneyDigits(value);
  const display = formatMoney(digits);

  const setRefs = useCallback(
    (el) => {
      inputRef.current = el;
      if (typeof ref === 'function') ref(el);
      else if (ref) ref.current = el;
    },
    [ref]
  );

  // Nilai berubah dari luar (bukan hasil ketikan di sini) -> riwayat undo tidak lagi relevan.
  const h = history.current;
  if (h.expected !== null && h.expected !== digits) {
    h.undo = [];
    h.redo = [];
  }
  h.expected = digits;

  // Cadangan bila render ulang menimpa posisi kursor (mis. pemanggil merapikan nilai).
  useLayoutEffect(() => {
    const el = inputRef.current;
    const pos = pendingCaret.current;
    pendingCaret.current = null;
    if (el && pos !== null && el.ownerDocument.activeElement === el && el.value === display && el.selectionStart !== pos) {
      el.setSelectionRange(pos, pos);
    }
  });

  const placeCaret = (el, pos) => {
    if (el.ownerDocument.activeElement !== el) return;
    try {
      el.setSelectionRange(pos, pos);
    } catch {
      /* jenis input tanpa dukungan seleksi */
    }
    pendingCaret.current = pos;
  };

  // Terapkan digit baru (belum dirapikan) dengan `left` digit di kiri kursor.
  // mode: 'edit' (catat ke riwayat undo), 'undo'/'redo' (dari riwayat).
  const commit = (el, rawDigits, left, insertedDigits, mode = 'edit') => {
    const next = stripLeadingZeros(rawDigits);
    let k = Math.max(0, left - (rawDigits.length - next.length));
    if (next.length > maxDigits) {
      // Tolak: kembalikan tampilan lama dan kursor ke tempat semula.
      el.value = display;
      placeCaret(el, caretForDigitIndex(display, Math.max(0, left - insertedDigits)));
      return;
    }
    const nextDisplay = formatMoney(next);
    k = Math.min(k, next.length);
    el.value = nextDisplay;
    placeCaret(el, caretForDigitIndex(nextDisplay, k));
    if (next === digits) return;
    if (mode === 'edit') {
      h.undo.push({ digits, k: changedEnd(digits, next) });
      if (h.undo.length > 100) h.undo.shift();
      h.redo = [];
    }
    h.expected = next;
    if (onValueChange) onValueChange(next, next === '' ? null : Number(next));
  };

  // Undo/redo dari riwayat sendiri; true bila ada yang diterapkan.
  const travel = (el, dir) => {
    const from = dir === 'undo' ? h.undo : h.redo;
    const to = dir === 'undo' ? h.redo : h.undo;
    const entry = from.pop();
    if (!entry) return false;
    to.push({ digits, k: changedEnd(digits, entry.digits) });
    commit(el, entry.digits, entry.k, 0, dir);
    return true;
  };
  const travelRef = useRef(travel);
  travelRef.current = travel;

  // "Urungkan/Ulangi" dari menu konteks/keyboard virtual datang sebagai beforeinput historyUndo/historyRedo.
  useEffect(() => {
    const el = inputRef.current;
    if (!el) return undefined;
    const onBeforeInput = (e) => {
      if (e.inputType === 'historyUndo' || e.inputType === 'historyRedo') {
        e.preventDefault();
        travelRef.current(el, e.inputType === 'historyUndo' ? 'undo' : 'redo');
      }
    };
    el.addEventListener('beforeinput', onBeforeInput);
    return () => el.removeEventListener('beforeinput', onBeforeInput);
  }, []);

  const handleChange = (e) => {
    const el = e.target;
    const raw = el.value;
    const caret = el.selectionStart ?? raw.length;
    let left = countDigits(raw.slice(0, caret));
    let nextDigits = onlyDigits(raw);
    // Hanya titik pemisah yang terhapus: hapus digit di sebelahnya sesuai arah.
    if (nextDigits === digits && raw.length === display.length - 1 && raw !== display) {
      const type = e.nativeEvent?.inputType || '';
      const forward = type === 'deleteContentForward' || (!type && lastKey.current === 'Delete');
      if (forward) {
        if (left < nextDigits.length) nextDigits = nextDigits.slice(0, left) + nextDigits.slice(left + 1);
      } else if (left > 0) {
        nextDigits = nextDigits.slice(0, left - 1) + nextDigits.slice(left);
        left -= 1;
      }
    }
    commit(el, nextDigits, left, Math.max(0, nextDigits.length - digits.length));
  };

  const handlePaste = (e) => {
    if (onPaste) onPaste(e);
    if (e.defaultPrevented) return;
    const text = e.clipboardData ? e.clipboardData.getData('text') : '';
    const pasted = extractPastedDigits(text);
    e.preventDefault();
    if (!pasted) return;
    const el = e.target;
    const start = el.selectionStart ?? display.length;
    const end = el.selectionEnd ?? start;
    const leftD = countDigits(display.slice(0, start));
    const rightFrom = countDigits(display.slice(0, Math.max(start, end)));
    commit(el, digits.slice(0, leftD) + pasted + digits.slice(rightFrom), leftD + pasted.length, pasted.length);
  };

  const handleKeyDown = (e) => {
    lastKey.current = e.key;
    if (onKeyDown) onKeyDown(e);
    if (e.defaultPrevented || !(e.ctrlKey || e.metaKey) || e.altKey) return;
    const k = String(e.key).toLowerCase();
    const dir = k === 'z' ? (e.shiftKey ? 'redo' : 'undo') : k === 'y' && !e.shiftKey ? 'redo' : null;
    if (dir) {
      e.preventDefault();
      travel(e.target, dir);
    }
  };

  const n = digits === '' ? null : Number(digits);
  const outOfRange = n !== null && ((min !== undefined && min !== null && n < min) || (max !== undefined && max !== null && n > max));
  const invalid = ariaInvalid !== undefined ? ariaInvalid : outOfRange ? 'true' : undefined;

  return (
    <input
      type="text"
      inputMode="numeric"
      autoComplete="off"
      {...rest}
      ref={setRefs}
      value={display}
      aria-invalid={invalid}
      onChange={handleChange}
      onPaste={handlePaste}
      onKeyDown={handleKeyDown}
    />
  );
});

export default MoneyInput;
