// Tes render (jsdom) halaman Admin -> Data Wilayah: status (kosong / tersimpan), tombol fetch, kemajuan
// (polling), ringkasan + popup Simpan/Batal, batalkan run berjalan, riwayat.
// Catatan: CRA memakai resetMocks, jadi mock modul memakai fungsi biasa + log panggilan sendiri.
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

const mockState = { calls: [], status: null, runs: {}, fetchReply: null };

jest.mock('../admin/api', () => {
  const actual = jest.requireActual('../admin/api');
  return {
    ...actual,
    adminFetch: (path, opts) => {
      const method = opts?.method || 'GET';
      mockState.calls.push({ method, path });
      if (path === '/regions/status') return Promise.resolve(mockState.status);
      if (path === '/regions/fetch') {
        if (mockState.fetchReply) return mockState.fetchReply();
        return Promise.resolve({ run: mockState.status.activeRun });
      }
      const m = path.match(/^\/regions\/runs\/(\d+)(\/(save|discard))?$/);
      if (m && !m[3]) return Promise.resolve({ run: mockState.runs[m[1]]() });
      if (m && m[3] === 'save') return Promise.resolve(mockState.afterSave);
      if (m && m[3] === 'discard') return Promise.resolve(mockState.afterDiscard);
      return Promise.resolve({});
    },
  };
});

const Regions = require('../admin/Regions').default;

global.IS_REACT_ACT_ENVIRONMENT = true;

let container;
let root;
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
};
const wait = async (ms) => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
};

const EMPTY = {
  stored: { hasData: false, datasetTotal: 0, items: { provinces: 0, regencies: 0, districts: 0, villages: 0 }, datasets: {}, sourceUpdatedAt: null, lastSaved: null },
  activeRun: null,
  runs: [],
  stagedTtlHours: 24,
};
const run = (over) => ({
  id: 7,
  status: 'running',
  statusLabel: 'Berjalan',
  startedBy: 'pemilik@example.com',
  startedAt: '2026-10-03T03:00:00Z',
  progress: { done: 120, total: 553, phase: 3 },
  counts: { provinces: 38, regencies: 514, districts: 0, villages: 0 },
  stats: { requests: 120, retries: 2, failed: 0, durationMs: 30000 },
  diff: null,
  ...over,
});
const STAGED = run({
  status: 'staged',
  statusLabel: 'Menunggu keputusan simpan',
  progress: { done: 7800, total: 7800, phase: 4 },
  counts: { provinces: 38, regencies: 514, districts: 7277, villages: 83762 },
  sourceUpdatedAt: '2025-07-04',
  diff: {
    new: 7840,
    changed: 0,
    missing: 0,
    itemsStored: { provinces: 0, regencies: 0, districts: 0, villages: 0 },
    itemsNew: { provinces: 38, regencies: 514, districts: 7277, villages: 83762 },
    warnings: ['Jumlah kelurahan/desa turun 12.0% (95000 → 83762)'],
  },
});
const SAVED_STATUS = {
  stored: {
    hasData: true,
    datasetTotal: 7840,
    sizeBytes: 5 * 1024 * 1024,
    items: { provinces: 38, regencies: 514, districts: 7277, villages: 83762 },
    datasets: { provinces: 1, regencies: 38, districts: 514, villages: 7287 },
    sourceUpdatedAt: '2025-07-04',
    lastSaved: { runId: 7, at: '2026-10-03T03:20:00Z', by: 'pemilik@example.com' },
  },
  activeRun: null,
  runs: [{ ...STAGED, status: 'saved', statusLabel: 'Tersimpan', savedAt: '2026-10-03T03:20:00Z', savedBy: 'pemilik@example.com' }],
};

const waitFor = async (cond, ms = 2000) => {
  const end = Date.now() + ms;
  while (!cond() && Date.now() < end) {
    // eslint-disable-next-line no-await-in-loop
    await wait(10);
  }
};

const render = async () => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  // eslint-disable-next-line testing-library/no-unnecessary-act -- root.render React DOM, bukan Testing Library
  await act(async () => {
    root.render(<Regions pollMs={15} />);
  });
  await flush();
};

beforeEach(() => {
  mockState.calls = [];
  mockState.runs = {};
  mockState.fetchReply = null;
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const btn = (text) => [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const click = async (el) => {
  await act(async () => {
    el.click();
  });
  await flush();
};
const posts = () => mockState.calls.filter((c) => c.method === 'POST').map((c) => c.path);

test('kosong -> Fetch -> kemajuan (polling) -> ringkasan + popup -> Simpan', async () => {
  mockState.status = EMPTY;
  await render();
  expect(container.querySelector('h1').textContent).toBe('Data Wilayah');
  expect(container.querySelector('[data-testid="region-empty"]').textContent).toContain('Belum ada data wilayah');
  expect(container.textContent).toContain('Belum ada riwayat.');
  const fetchBtn = container.querySelector('[data-testid="region-fetch"]');
  expect(fetchBtn.textContent).toContain('Fetch ulang dari wilayah.id');
  expect(fetchBtn.disabled).toBe(false);

  // Fetch dimulai -> status berikutnya: run berjalan.
  let finished = false;
  mockState.runs['7'] = () => (finished ? STAGED : run({ progress: { done: 300, total: 553, phase: 3 } }));
  mockState.fetchReply = () => {
    mockState.status = { ...EMPTY, activeRun: run(), runs: [run()] };
    return Promise.resolve({ run: run() });
  };
  await click(fetchBtn);
  expect(posts()).toEqual(['/regions/fetch']);
  const prog = container.querySelector('[data-testid="region-progress"]');
  expect(prog.textContent).toContain('Mengambil data kecamatan');
  expect(prog.textContent).toContain('120 / 553 permintaan (22%)');
  expect(container.querySelector('[role="progressbar"]').getAttribute('aria-valuenow')).toBe('22');
  expect(container.querySelector('[data-testid="region-fetch"]').disabled).toBe(true);

  // Polling: kemajuan diperbarui, lalu selesai (staged) -> status dimuat ulang -> popup terbuka.
  await waitFor(() => container.textContent.includes('300 / 553 permintaan'));
  expect(container.textContent).toContain('300 / 553 permintaan');
  mockState.status = { ...EMPTY, activeRun: STAGED, runs: [STAGED] };
  finished = true;
  await waitFor(() => !!container.querySelector('[data-testid="region-modal"]'));
  const modal = container.querySelector('[data-testid="region-modal"]');
  expect(modal).toBeTruthy();
  expect(container.querySelector('[role="dialog"] h3').textContent).toBe('Simpan data wilayah ke database?');
  const rows = [...modal.querySelectorAll('tr[data-level]')].map((tr) => tr.textContent);
  expect(rows[3]).toContain('Kelurahan/Desa');
  expect(rows[3]).toContain('83.762');
  expect(modal.querySelector('[data-testid="region-warnings"]').textContent).toContain('turun 12.0%');
  expect(modal.textContent).toContain('4 Juli 2025');
  const pollsBefore = mockState.calls.filter((c) => c.path === '/regions/runs/7').length;

  // Simpan.
  mockState.afterSave = SAVED_STATUS;
  await click([...modal.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Simpan'));
  expect(posts()).toEqual(['/regions/fetch', '/regions/runs/7/save']);
  expect(container.querySelector('[data-testid="region-modal"]')).toBeNull();
  expect(container.textContent).toContain('Data wilayah tersimpan ke database.');
  const stored = container.querySelector('[data-testid="region-stored"]').textContent;
  expect(stored).toContain('83.762');
  expect(stored).toContain('4 Juli 2025');
  expect(stored).toContain('pemilik@example.com');
  expect(container.querySelector('[data-testid="region-history"]').textContent).toContain('Tersimpan');
  // Polling berhenti setelah run tidak berjalan lagi.
  await wait(50);
  expect(mockState.calls.filter((c) => c.path === '/regions/runs/7').length).toBe(pollsBefore);
});

test('popup Batal membuang hasil fetch (data lama tidak berubah)', async () => {
  mockState.status = { ...SAVED_STATUS, activeRun: STAGED, runs: [STAGED, ...SAVED_STATUS.runs] };
  mockState.afterDiscard = { ...SAVED_STATUS, runs: [{ ...STAGED, status: 'discarded', statusLabel: 'Dibuang' }, ...SAVED_STATUS.runs] };
  await render();
  const modal = container.querySelector('[data-testid="region-modal"]');
  expect(modal).toBeTruthy();
  await click([...modal.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Batal'));
  expect(posts()).toEqual(['/regions/runs/7/discard']);
  expect(container.querySelector('[data-testid="region-modal"]')).toBeNull();
  expect(container.textContent).toContain('Hasil fetch dibuang. Data lama tidak berubah.');
  expect(container.querySelector('[data-testid="region-history"]').textContent).toContain('Dibuang');
  // Menutup popup (×) tidak membuang; ringkasan tetap bisa dibuka lagi.
});

test('tutup popup lalu buka lagi dari kartu ringkasan; batalkan run berjalan dengan konfirmasi', async () => {
  mockState.status = { ...EMPTY, activeRun: STAGED, runs: [STAGED] };
  await render();
  await click(container.querySelector('[aria-label="Tutup"]'));
  expect(container.querySelector('[data-testid="region-modal"]')).toBeNull();
  expect(posts()).toEqual([]);
  expect(container.querySelector('[data-testid="region-summary"]')).toBeTruthy();
  await click(btn('Simpan ke database…'));
  expect(container.querySelector('[data-testid="region-modal"]')).toBeTruthy();

  act(() => root.unmount());
  container.remove();
  mockState.calls = [];
  mockState.runs['7'] = () => run();
  mockState.status = { ...EMPTY, activeRun: run(), runs: [run()] };
  mockState.afterDiscard = { ...EMPTY, runs: [run({ status: 'cancelled', statusLabel: 'Dibatalkan', error: 'Dibatalkan oleh admin' })] };
  await render();
  await click(btn('Batalkan'));
  expect(posts()).toEqual([]); // perlu konfirmasi
  await click(btn('Ya, batalkan'));
  expect(posts()).toEqual(['/regions/runs/7/discard']);
  expect(container.textContent).toContain('Fetch dibatalkan. Data lama tidak berubah.');
  expect(container.textContent).toContain('Dibatalkan oleh admin');
});

test('galat fetch ditampilkan (mis. 409 masih ada run aktif)', async () => {
  mockState.status = EMPTY;
  mockState.fetchReply = () => Promise.reject(Object.assign(new Error('Masih ada pengambilan data wilayah yang berjalan'), { status: 409 }));
  await render();
  await click(container.querySelector('[data-testid="region-fetch"]'));
  expect(container.textContent).toContain('Masih ada pengambilan data wilayah yang berjalan');
});
