// Gaya HP global (src/index.css): huruf & jarak lebih kecil di < 640px tanpa mengubah layar lebar.
import fs from 'fs';
import path from 'path';

const css = fs.readFileSync(path.join(__dirname, '..', 'index.css'), 'utf8');
const stripComments = (s) => s.replace(/\/\*[\s\S]*?\*\//g, '');
const mobileBlock = () => {
  const start = css.indexOf('@media (max-width: 639.98px)');
  expect(start).toBeGreaterThan(-1);
  return stripComments(css.slice(start));
};

test('index.css diimpor dari titik masuk aplikasi', () => {
  const entry = fs.readFileSync(path.join(__dirname, '..', 'index.js'), 'utf8');
  expect(entry).toMatch(/import '\.\/index\.css'/);
});

test('hanya satu media query mobile (< 640px); di luar itu hanya latar body', () => {
  expect(css.match(/@media/g)).toHaveLength(1);
  expect(stripComments(css)).not.toMatch(/@media[^{]*min-width/);
  const outside = stripComments(css.slice(0, css.indexOf('@media'))).replace(/\s+/g, ' ').trim();
  expect(outside).toBe('body { background-color: #f9fafb; }');
});

test('skala dasar 14px dan batas bawah keterbacaan, input iOS, target sentuh', () => {
  const m = mobileBlock();
  expect(m).toMatch(/html\s*{\s*font-size:\s*14px;/);
  expect(m).toMatch(/:root \.text-sm\s*{\s*font-size:\s*13px;/);
  expect(m).toMatch(/:root \.text-xs\s*{\s*font-size:\s*12px;/);
  // Input/select/textarea tetap 16px (cegah zoom otomatis iOS Safari saat fokus).
  expect(m).toMatch(/:root select,\s*:root textarea\s*{\s*font-size:\s*16px !important;/);
  expect(m).toMatch(/:root button:not\(\.sr-only\),[\s\S]*?{\s*min-height:\s*40px;\s*min-width:\s*40px;/);
  // Bilah ikon admin tetap 64px dan tombol menu 44px (px, tidak ikut menyusut).
  expect(m).toMatch(/#admin-sidebar\s*{\s*width:\s*64px;/);
  expect(m).toMatch(/#admin-sidebar \+ div\s*{\s*padding-left:\s*64px;/);
  expect(m).toMatch(/#admin-sidebar nav a,\s*:root #admin-sidebar a\[href='\/'\]\s*{\s*height:\s*44px;/);
});
