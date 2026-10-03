import React, { useEffect, useState } from 'react';

// URL gambar yang aman dimuat: hanya URL absolut http(s) atau path same-origin berawalan "/" (bukan "//").
// Nilai lain (mis. nama berkas lama "kerupuk1.jpg" tanpa berkas fisik) -> null (placeholder, tanpa permintaan jaringan).
export const safeImageUrl = (v) => {
  const s = String(v || '').trim();
  return /^https?:\/\/\S+$/i.test(s) || /^\/[^/\s]\S*$/.test(s) ? s : null;
};

// Thumbnail persegi kecil (keranjang, daftar produk admin): object-cover, lazy, ukuran tetap (tanpa loncatan
// tata letak), placeholder netral bila tidak ada foto atau gagal dimuat.
export const SmallThumb = ({ src, alt, size = 48, className = '' }) => {
  const url = safeImageUrl(src);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [url]);
  const box = { width: size, height: size };
  if (!url || failed) {
    return (
      <span
        className={`inline-flex shrink-0 items-center justify-center rounded-md border border-gray-200 bg-gray-100 text-gray-400 ${className}`}
        style={box}
        role="img"
        aria-label={`Foto ${alt} belum tersedia`}
        data-testid="thumb-placeholder"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" className="h-1/2 w-1/2" aria-hidden="true">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <circle cx="9" cy="10" r="2" />
          <path d="M21 17l-5-5-9 8" />
        </svg>
      </span>
    );
  }
  return (
    <img
      src={url}
      alt={alt}
      width={size}
      height={size}
      loading="lazy"
      decoding="async"
      className={`shrink-0 rounded-md border border-gray-200 bg-gray-100 object-cover ${className}`}
      style={box}
      onError={() => setFailed(true)}
      data-testid="thumb-img"
    />
  );
};
