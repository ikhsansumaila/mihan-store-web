// Pembantu tes: simulasi ketikan/hapus/tempel pada kolom (jsdom) lewat event input asli (dipakai MoneyInput).
import { act } from 'react';

export const setRaw = (el, raw, caret, inputType) => {
  Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, raw);
  el.setSelectionRange(caret, caret);
  act(() => {
    el.dispatchEvent(new InputEvent('input', { bubbles: true, inputType }));
  });
};

// Ketik satu karakter di kursor (mengganti seleksi bila ada).
export const typeChar = (el, ch) => {
  const s = el.selectionStart;
  const e = el.selectionEnd;
  setRaw(el, el.value.slice(0, s) + ch + el.value.slice(e), s + ch.length, 'insertText');
};

// Fokus, (opsional) pilih semua, lalu ketik per karakter.
export const typeInto = (el, text, { replace = true } = {}) => {
  act(() => el.focus());
  if (replace) el.setSelectionRange(0, el.value.length);
  else el.setSelectionRange(el.value.length, el.value.length);
  [...text].forEach((ch) => typeChar(el, ch));
};

export const pasteInto = (el, text, { replace = true } = {}) => {
  act(() => el.focus());
  if (replace) el.setSelectionRange(0, el.value.length);
  const ev = new Event('paste', { bubbles: true, cancelable: true });
  Object.defineProperty(ev, 'clipboardData', { value: { getData: () => text } });
  act(() => {
    el.dispatchEvent(ev);
  });
};
