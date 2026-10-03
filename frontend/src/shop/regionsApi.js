// Data wilayah untuk checkout (API publik /api/regions/*, sumber wilayah.id yang disimpan di DB toko).
// Diambil PER INDUK (provinsi -> kab/kota -> kecamatan -> kelurahan/desa), tidak pernah seluruh dataset.
// Hasil disimpan di memori selama halaman terbuka (permintaan sama tidak diulang).
import axios from 'axios';
import { API_BASE_URL } from '../auth';

export const REGION_LEVELS = [
  { key: 'province', field: 'provinceCode', label: 'Provinsi', lower: 'provinsi', path: () => 'provinces' },
  { key: 'regency', field: 'regencyCode', label: 'Kabupaten/Kota', lower: 'kabupaten/kota', path: (p) => `regencies/${p}` },
  { key: 'district', field: 'districtCode', label: 'Kecamatan', lower: 'kecamatan', path: (p) => `districts/${p}` },
  { key: 'village', field: 'villageCode', label: 'Kelurahan/Desa', lower: 'kelurahan/desa', path: (p) => `villages/${p}` },
];

const cache = new Map();

export const clearRegionCache = () => cache.clear();

const byName = (a, b) => a.name.localeCompare(b.name, 'id', { sensitivity: 'base' });

// Muat daftar wilayah tingkat `level` (0..3) di bawah kode induk `parent`. Promise<[{code,name}]>.
export const loadRegions = (level, parent) => {
  const path = REGION_LEVELS[level].path(parent);
  if (cache.has(path)) return cache.get(path);
  const p = axios
    .get(`${API_BASE_URL}/regions/${path}`)
    .then((r) => {
      const items = Array.isArray(r?.data?.data) ? r.data.data : [];
      return items
        .filter((it) => it && typeof it.code === 'string' && typeof it.name === 'string')
        .map((it) => ({ code: it.code, name: it.name }))
        .sort(byName);
    })
    .catch((err) => {
      cache.delete(path); // galat tidak di-cache agar "Coba lagi" benar-benar mencoba lagi
      throw err;
    });
  cache.set(path, p);
  return p;
};

export const regionErrorMessage = (err, lower) => {
  const st = err?.response?.status;
  if (st === 503) return 'Data wilayah belum tersedia. Silakan coba lagi nanti atau hubungi toko.';
  if (st === 404) return `Daftar ${lower} tidak ditemukan. Pilih ulang wilayah di atasnya.`;
  if (st === 429) return 'Terlalu banyak permintaan. Tunggu sebentar lalu coba lagi.';
  return `Gagal memuat daftar ${lower}. Periksa koneksi lalu coba lagi.`;
};

// Pencarian: tidak peka huruf besar/kecil & aksen; semua kata harus ada di nama.
export const normalizeSearch = (s) =>
  String(s || '')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();

export const filterRegions = (items, query) => {
  const words = normalizeSearch(query).split(' ').filter(Boolean);
  if (!words.length) return items;
  return items.filter((it) => {
    const n = normalizeSearch(it.name);
    return words.every((w) => n.includes(w));
  });
};
