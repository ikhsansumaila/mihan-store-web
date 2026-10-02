// Unduh / bagikan file PNG pricelist dari browser.

export const downloadBlob = (blob, name) => {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
};

// Unduh beberapa file berurutan (jeda kecil agar browser tidak menggabung/menolak unduhan beruntun).
export const downloadAll = async (files, gapMs = 600) => {
  for (let i = 0; i < files.length; i += 1) {
    if (i > 0) await new Promise((r) => setTimeout(r, gapMs)); // eslint-disable-line no-await-in-loop
    downloadBlob(files[i].blob, files[i].name);
  }
};

// Apakah Web Share API bisa membagikan file-file ini (umumnya HP).
export const canShareFiles = (files) => {
  if (typeof navigator === 'undefined' || typeof navigator.share !== 'function' || typeof navigator.canShare !== 'function') return false;
  if (typeof File === 'undefined') return false;
  try {
    return navigator.canShare({ files });
  } catch {
    return false;
  }
};

export const toFiles = (items) => items.map((it) => new File([it.blob], it.name, { type: 'image/png' }));

// Bagikan file lewat sheet bawaan perangkat. Hasil: 'shared' | 'cancelled'; galat lain dilempar.
export const shareFiles = async (files, text) => {
  try {
    await navigator.share({ files, text });
    return 'shared';
  } catch (err) {
    if (err && err.name === 'AbortError') return 'cancelled';
    throw err;
  }
};

export const copyText = async (text) => {
  try {
    if (navigator.clipboard && window.isSecureContext !== false) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // lanjut ke cara lama
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand && document.execCommand('copy');
    ta.remove();
    return !!ok;
  } catch {
    return false;
  }
};
