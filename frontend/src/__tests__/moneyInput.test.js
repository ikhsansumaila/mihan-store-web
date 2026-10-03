// Tes MoneyInput (src/components/MoneyInput.js): fungsi format/parse dan perilaku komponen di jsdom
// (mengetik bertahap, hapus di dekat titik, pilih semua lalu ketik, tempel, kursor, maxDigits, min/max, ref).
import React, { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import MoneyInput, { caretForDigitIndex, extractPastedDigits, formatMoney, parseMoneyText, toMoneyDigits } from '../components/MoneyInput';

global.IS_REACT_ACT_ENVIRONMENT = true;

describe('format & parse', () => {
  test('formatMoney: titik setiap 3 digit, kosong tetap kosong, nol tetap 0', () => {
    expect(formatMoney(1250000)).toBe('1.250.000');
    expect(formatMoney('1250000')).toBe('1.250.000');
    expect(formatMoney('999')).toBe('999');
    expect(formatMoney('1000')).toBe('1.000');
    expect(formatMoney(1000000000)).toBe('1.000.000.000');
    expect(formatMoney('')).toBe('');
    expect(formatMoney(null)).toBe('');
    expect(formatMoney(undefined)).toBe('');
    expect(formatMoney(0)).toBe('0');
    expect(formatMoney('0')).toBe('0');
    expect(formatMoney('000')).toBe('0');
    expect(formatMoney('007')).toBe('7');
    expect(formatMoney('0045000')).toBe('45.000');
  });

  test('toMoneyDigits: angka/string/nilai aneh', () => {
    expect(toMoneyDigits(45000)).toBe('45000');
    expect(toMoneyDigits('45.000')).toBe('45000');
    expect(toMoneyDigits('abc')).toBe('');
    expect(toMoneyDigits(NaN)).toBe('');
    expect(toMoneyDigits(Infinity)).toBe('');
    expect(toMoneyDigits(12.9)).toBe('12');
    expect(toMoneyDigits(1e21)).toBe('');
  });

  test.each([
    ['Rp 1.500.000', '1500000'],
    ['Rp1.500.000', '1500000'],
    ['1,500,000', '1500000'],
    ['1.500.000,00', '1500000'],
    ['1,500,000.00', '1500000'],
    ['1.500.000,5', '1500000'],
    ['Rp 1.500.000,00 IDR', '1500000'],
    ['1500000', '1500000'],
    [' 1 500 000 ', '1500000'],
    ['IDR 25.000,-', '25000'],
    ['1.500', '1500'],
    ['1,500', '1500'],
    ['12,5', '125'],
    ['12.50', '1250'],
    ['1.500.000,000', '1500000000'],
    ['007', '7'],
    ['0', '0'],
    ['abc', ''],
    ['', ''],
    ['-50.000', '50000'],
    ['٣٤', ''],
  ])('parseMoneyText(%p) -> %p', (input, out) => {
    expect(parseMoneyText(input)).toBe(out);
  });

  test('extractPastedDigits tidak merapikan nol depan (untuk tempel di tengah)', () => {
    expect(extractPastedDigits('05')).toBe('05');
    expect(extractPastedDigits('Rp 0,50')).toBe('050');
  });

  test('caretForDigitIndex', () => {
    expect(caretForDigitIndex('1.250.000', 0)).toBe(0);
    expect(caretForDigitIndex('1.250.000', 1)).toBe(1);
    expect(caretForDigitIndex('1.250.000', 2)).toBe(3);
    expect(caretForDigitIndex('1.250.000', 4)).toBe(5);
    expect(caretForDigitIndex('1.250.000', 7)).toBe(9);
    expect(caretForDigitIndex('1.250.000', 99)).toBe(9);
  });
});

describe('komponen', () => {
  let container;
  let root;
  let log;
  const Harness = ({ initial = '', numeric = false, inputRef, ...extra }) => {
    const [v, setV] = useState(initial);
    return (
      <form>
        <MoneyInput
          aria-label="uang"
          ref={inputRef}
          value={v}
          onValueChange={(d, n) => {
            log.push([d, n]);
            setV(numeric ? n : d);
          }}
          {...extra}
        />
        <button type="button" onClick={() => setV('')}>
          reset
        </button>
      </form>
    );
  };
  const mount = async (props = {}) => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
    await act(async () => root.render(<Harness {...props} />));
    const el = container.querySelector('input');
    el.focus();
    return el;
  };
  beforeEach(() => {
    log = [];
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const setRaw = (el, raw, caret, inputType) => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(el, raw);
    el.setSelectionRange(caret, caret);
    act(() => {
      el.dispatchEvent(new InputEvent('input', { bubbles: true, inputType }));
    });
  };
  // Ketik satu karakter di posisi kursor saat ini (mengganti seleksi bila ada).
  const typeChar = (el, ch) => {
    const s = el.selectionStart;
    const e = el.selectionEnd;
    setRaw(el, el.value.slice(0, s) + ch + el.value.slice(e), s + ch.length, 'insertText');
  };
  const typeText = (el, text) => [...text].forEach((ch) => typeChar(el, ch));
  const backspace = (el) => {
    const s = el.selectionStart;
    const e = el.selectionEnd;
    if (s !== e) setRaw(el, el.value.slice(0, s) + el.value.slice(e), s, 'deleteContentBackward');
    else if (s > 0) setRaw(el, el.value.slice(0, s - 1) + el.value.slice(s), s - 1, 'deleteContentBackward');
  };
  const del = (el) => {
    const s = el.selectionStart;
    setRaw(el, el.value.slice(0, s) + el.value.slice(s + 1), s, 'deleteContentForward');
  };
  const paste = (el, text) => {
    const ev = new Event('paste', { bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'clipboardData', { value: { getData: () => text } });
    act(() => {
      el.dispatchEvent(ev);
    });
    return ev;
  };
  const caretAt = (el, pos) => el.setSelectionRange(pos, pos);

  test('atribut dasar: text + inputMode numeric (tanpa spinner), atribut diteruskan', async () => {
    const el = await mount({ id: 'harga', name: 'harga', placeholder: '15.000', required: true, className: 'kelas-x', 'aria-describedby': 'bantuan' });
    expect(el.type).toBe('text');
    expect(el.getAttribute('inputmode')).toBe('numeric');
    expect(el.id).toBe('harga');
    expect(el.name).toBe('harga');
    expect(el.placeholder).toBe('15.000');
    expect(el.required).toBe(true);
    expect(el.className).toBe('kelas-x');
    expect(el.getAttribute('aria-describedby')).toBe('bantuan');
    expect(el.getAttribute('aria-label')).toBe('uang');
    expect(el.value).toBe('');
  });

  test('mengetik bertahap: tampilan berformat, nilai digit & angka bulat, kursor di akhir', async () => {
    const el = await mount();
    const seen = [];
    for (const ch of '1250000') {
      typeChar(el, ch);
      seen.push([el.value, el.selectionStart]);
    }
    expect(seen).toEqual([
      ['1', 1],
      ['12', 2],
      ['125', 3],
      ['1.250', 5],
      ['12.500', 6],
      ['125.000', 7],
      ['1.250.000', 9],
    ]);
    expect(log[log.length - 1]).toEqual(['1250000', 1250000]);
  });

  test('karakter non-digit dibuang; nol depan dirapikan; kosong tetap kosong', async () => {
    const el = await mount();
    typeText(el, 'a');
    expect(el.value).toBe('');
    expect(log).toEqual([]);
    typeText(el, '0');
    expect(el.value).toBe('0');
    expect(log[log.length - 1]).toEqual(['0', 0]);
    typeText(el, '07');
    expect(el.value).toBe('7');
    expect(log[log.length - 1]).toEqual(['7', 7]);
    typeText(el, 'x.,-5');
    expect(el.value).toBe('75');
    backspace(el);
    backspace(el);
    expect(el.value).toBe('');
    expect(log[log.length - 1]).toEqual(['', null]);
  });

  test('menyunting di tengah: kursor tetap di tempatnya', async () => {
    const el = await mount({ initial: '1250000' });
    expect(el.value).toBe('1.250.000');
    // Kursor setelah "1.2" lalu ketik 9 -> 12.950.000, kursor setelah "12.9".
    caretAt(el, 3);
    typeChar(el, '9');
    expect(el.value).toBe('12.950.000');
    expect(el.selectionStart).toBe(4);
    expect(log[log.length - 1]).toEqual(['12950000', 12950000]);
    // Hapus digit di tengah (Backspace setelah "12.9") -> 1.250.000, kursor setelah "1.2".
    backspace(el);
    expect(el.value).toBe('1.250.000');
    expect(el.selectionStart).toBe(3);
  });

  test('Backspace tepat setelah titik menghapus digit sebelum titik; Delete sebelum titik menghapus digit sesudahnya', async () => {
    const el = await mount({ initial: '1250000' });
    caretAt(el, 2); // "1.|250.000"
    backspace(el);
    expect(el.value).toBe('250.000');
    expect(el.selectionStart).toBe(0);
    expect(log[log.length - 1]).toEqual(['250000', 250000]);
    caretAt(el, 3); // "250|.000"
    del(el);
    expect(el.value).toBe('25.000');
    expect(el.selectionStart).toBe(4); // 3 digit di kiri kursor tetap: "25.0|00"
    expect(log[log.length - 1]).toEqual(['25000', 25000]);
  });

  test('menghapus digit pertama menghasilkan nol depan -> dirapikan', async () => {
    const el = await mount({ initial: '1005' });
    caretAt(el, 1); // "1|.005"
    backspace(el);
    expect(el.value).toBe('5');
    expect(el.selectionStart).toBe(0);
    expect(log[log.length - 1]).toEqual(['5', 5]);
  });

  test('pilih semua lalu ketik mengganti nilai', async () => {
    const el = await mount({ initial: '1250000' });
    el.setSelectionRange(0, el.value.length);
    typeChar(el, '4');
    expect(el.value).toBe('4');
    typeText(el, '5000');
    expect(el.value).toBe('45.000');
    expect(log[log.length - 1]).toEqual(['45000', 45000]);
    el.setSelectionRange(0, el.value.length);
    backspace(el);
    expect(el.value).toBe('');
    expect(log[log.length - 1]).toEqual(['', null]);
  });

  test('tempel berbagai format', async () => {
    const el = await mount();
    for (const [text, disp, val] of [
      ['Rp 1.500.000', '1.500.000', 1500000],
      ['1,500,000', '1.500.000', 1500000],
      ['1.500.000,00', '1.500.000', 1500000],
      ['1500000', '1.500.000', 1500000],
      ['Rp 25.000,-', '25.000', 25000],
    ]) {
      el.setSelectionRange(0, el.value.length);
      const ev = paste(el, text);
      expect(ev.defaultPrevented).toBe(true);
      expect(el.value).toBe(disp);
      expect(el.selectionStart).toBe(disp.length);
      expect(log[log.length - 1][1]).toBe(val);
    }
    // Teks tanpa digit diabaikan.
    const n = log.length;
    paste(el, 'halo');
    expect(el.value).toBe('25.000');
    expect(log).toHaveLength(n);
  });

  test('tempel di tengah menyisip pada posisi kursor', async () => {
    const el = await mount({ initial: '1000' });
    caretAt(el, 1); // "1|.000"
    paste(el, '25');
    expect(el.value).toBe('125.000');
    expect(el.selectionStart).toBe(3);
    expect(log[log.length - 1]).toEqual(['125000', 125000]);
  });

  test('maxDigits: ketikan/tempelan yang melewati batas ditolak, nilai lama dan kursor tetap', async () => {
    const el = await mount({ maxDigits: 10 });
    typeText(el, '1000000000');
    expect(el.value).toBe('1.000.000.000');
    const n = log.length;
    typeChar(el, '5');
    expect(el.value).toBe('1.000.000.000');
    expect(el.selectionStart).toBe(13);
    caretAt(el, 1);
    typeChar(el, '9');
    expect(el.value).toBe('1.000.000.000');
    expect(el.selectionStart).toBe(1);
    el.setSelectionRange(0, el.value.length);
    paste(el, 'Rp 15.000.000.000');
    expect(el.value).toBe('1.000.000.000');
    expect(log).toHaveLength(n);
    // Mengganti seluruh isi dengan nilai yang muat tetap boleh.
    el.setSelectionRange(0, el.value.length);
    paste(el, '9.999.999.999');
    expect(el.value).toBe('9.999.999.999');
    expect(log[log.length - 1]).toEqual(['9999999999', 9999999999]);
  });

  test('min/max lunak: aria-invalid saat di luar rentang, nilai tetap diteruskan', async () => {
    const el = await mount({ min: 1, max: 10000000 });
    expect(el.getAttribute('aria-invalid')).toBeNull();
    typeText(el, '0');
    expect(el.getAttribute('aria-invalid')).toBe('true');
    backspace(el);
    expect(el.getAttribute('aria-invalid')).toBeNull(); // kosong: urusan "required"/validasi pemanggil
    typeText(el, '10000000');
    expect(el.value).toBe('10.000.000');
    expect(el.getAttribute('aria-invalid')).toBeNull();
    typeText(el, '1');
    expect(el.value).toBe('100.000.001');
    expect(el.getAttribute('aria-invalid')).toBe('true');
    expect(log[log.length - 1]).toEqual(['100000001', 100000001]);
  });

  const key = (el, k, mods = {}) => {
    const ev = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...mods });
    act(() => {
      el.dispatchEvent(ev);
    });
    return ev;
  };

  test('undo/redo: Ctrl+Z, Ctrl+Y, Ctrl+Shift+Z, dan beforeinput historyUndo dari menu', async () => {
    const el = await mount();
    typeText(el, '1250');
    caretAt(el, 3); // "1.2|50"
    typeChar(el, '9');
    expect(el.value).toBe('12.950');
    expect(key(el, 'z', { ctrlKey: true }).defaultPrevented).toBe(true);
    expect(el.value).toBe('1.250');
    expect(el.selectionStart).toBe(3); // kembali ke tempat penyisipan
    expect(log[log.length - 1]).toEqual(['1250', 1250]);
    key(el, 'y', { ctrlKey: true });
    expect(el.value).toBe('12.950');
    key(el, 'z', { metaKey: true });
    key(el, 'z', { ctrlKey: true });
    expect(el.value).toBe('125');
    key(el, 'Z', { ctrlKey: true, shiftKey: true });
    expect(el.value).toBe('1.250');
    const bi = new InputEvent('beforeinput', { inputType: 'historyUndo', bubbles: true, cancelable: true });
    act(() => {
      el.dispatchEvent(bi);
    });
    expect(bi.defaultPrevented).toBe(true);
    expect(el.value).toBe('125');
    // Ketikan baru menghapus riwayat redo.
    typeChar(el, '7');
    key(el, 'y', { ctrlKey: true });
    expect(el.value).toBe('1.257');
    // Undo sampai kosong lalu tidak ada apa-apa lagi.
    for (let i = 0; i < 10; i += 1) key(el, 'z', { ctrlKey: true });
    expect(el.value).toBe('');
    expect(log[log.length - 1]).toEqual(['', null]);
  });

  test('riwayat undo dikosongkan bila nilai diubah dari luar (mis. form direset)', async () => {
    const el = await mount();
    typeText(el, '15000');
    await act(async () => container.querySelector('button').click());
    expect(el.value).toBe('');
    act(() => el.focus());
    key(el, 'z', { ctrlKey: true });
    expect(el.value).toBe('');
  });

  test('nilai angka (number) dari pemanggil + disabled + ref + onBlur', async () => {
    const ref = React.createRef();
    const blurs = [];
    const el = await mount({ initial: 45000, numeric: true, inputRef: ref, onBlur: (e) => blurs.push(e.target.value) });
    expect(ref.current).toBe(el);
    expect(el.value).toBe('45.000');
    typeChar(el, '0');
    expect(el.value).toBe('450.000');
    expect(log[log.length - 1]).toEqual(['450000', 450000]);
    act(() => el.blur());
    expect(blurs).toEqual(['450.000']);
    act(() => root.unmount());
    root = createRoot(container);
    // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
    await act(async () => root.render(<MoneyInput aria-label="mati" value={0} disabled onValueChange={() => {}} />));
    const off = container.querySelector('input');
    expect(off.disabled).toBe(true);
    expect(off.value).toBe('0');
  });
});
