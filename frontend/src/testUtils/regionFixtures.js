// Data wilayah tiruan + pembantu tes checkout (dipakai beberapa file tes jest; bukan bagian aplikasi).
import { act } from 'react';

export const REGION_FIXTURE = {
  provinces: [
    { code: '36', name: 'Banten' },
    { code: '31', name: 'DKI Jakarta' },
  ],
  'regencies/31': [
    { code: '31.74', name: 'Kota Administrasi Jakarta Selatan' },
    { code: '31.71', name: 'Kota Administrasi Jakarta Pusat' },
  ],
  'regencies/36': [
    { code: '36.71', name: 'Kota Tangerang' },
    { code: '36.03', name: 'Kabupaten Tangerang' },
  ],
  'districts/31.74': [
    { code: '31.74.06', name: 'Cilandak' },
    { code: '31.74.09', name: 'Jagakarsa' },
  ],
  'districts/36.71': [{ code: '36.71.01', name: 'Tangerang' }],
  'villages/31.74.06': [
    { code: '31.74.06.1004', name: 'Lebak Bulus' },
    { code: '31.74.06.1003', name: 'Cilandak Barat' },
  ],
  'villages/36.71.01': [{ code: '36.71.01.1001', name: 'Sukarasa' }],
};

// Respons axios.get tiruan untuk /api/regions/*: Promise data, atau reject {response:{status}}.
// override: { [path]: {status} | items } untuk menguji galat.
export const mockRegionGet = (url, calls, override = {}) => {
  const m = String(url).match(/\/api\/regions\/(.+)$/);
  if (!m) return null;
  const path = m[1];
  if (calls) calls.push(path);
  const o = override[path];
  if (o && o.status) return Promise.reject(Object.assign(new Error('gagal'), { response: { status: o.status, data: {} } }));
  const items = o || REGION_FIXTURE[path];
  if (!items) return Promise.reject(Object.assign(new Error('404'), { response: { status: 404, data: {} } }));
  return Promise.resolve({ data: { data: items, meta: { updatedAt: '2025-07-04' } } });
};

const tick = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};

export const setNativeValue = (el, value) => {
  const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
};

// Pilih wilayah di combobox: fokus, ketik sebagian nama (pencarian), klik pilihan.
export const chooseRegion = async (container, key, name, typed) => {
  const input = container.querySelector(`#co-${key}`);
  if (!input) throw new Error(`combobox ${key} tidak ada`);
  await act(async () => {
    input.focus();
  });
  if (typed !== undefined) {
    await act(async () => setNativeValue(input, typed));
  }
  const opt = [...container.querySelectorAll(`#co-${key}-list [role="option"]`)].find((o) => o.textContent.trim() === name);
  if (!opt) throw new Error(`pilihan ${name} tidak ada di ${key}`);
  await act(async () => {
    opt.click();
  });
  await tick();
  await tick();
};

// Isi semua field wajib checkout (nama, wilayah Kota Tangerang, alamat, kode pos).
export const fillCheckout = async (container, { name = 'Budi Penerima', postal = '15111' } = {}) => {
  await act(async () => {
    setNativeValue(container.querySelector('#co-name'), name);
  });
  await chooseRegion(container, 'province', 'Banten', 'ban');
  await chooseRegion(container, 'regency', 'Kota Tangerang', 'kota');
  await chooseRegion(container, 'district', 'Tangerang');
  await chooseRegion(container, 'village', 'Sukarasa');
  await act(async () => {
    setNativeValue(container.querySelector('#co-address'), 'Jl. Melati No. 9, RT 03/RW 05');
    setNativeValue(container.querySelector('#co-postal'), postal);
  });
};
