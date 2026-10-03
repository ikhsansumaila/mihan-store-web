import React, { useCallback, useEffect, useRef, useState } from 'react';
import { adminFetch, fmtTime } from './api';
import { Icon } from './icons';
import { ErrorBox, Modal, btnPrimary, btnSecondary, btnDanger, cardClass } from './ui';

// Admin -> Data Wilayah (/admin/regions). Data wilayah Indonesia dari wilayah.id disimpan di DB toko
// (format JSON). Alur: Fetch ulang -> kemajuan (polling ringan hanya selama berjalan) -> ringkasan ->
// popup "Simpan data wilayah ke database?" (Simpan / Batal). Batal = hasil fetch dibuang, data lama tetap.

export const POLL_MS = 3000;

export const LEVELS = [
  { key: 'provinces', label: 'Provinsi' },
  { key: 'regencies', label: 'Kabupaten/Kota' },
  { key: 'districts', label: 'Kecamatan' },
  { key: 'villages', label: 'Kelurahan/Desa' },
];

const PHASE_LABEL = { 1: 'provinsi', 2: 'kabupaten/kota', 3: 'kecamatan', 4: 'kelurahan/desa' };

const STATUS_CLS = {
  running: 'bg-blue-100 text-blue-800 border-blue-300',
  staged: 'bg-amber-100 text-amber-900 border-amber-300',
  saved: 'bg-green-100 text-green-800 border-green-300',
  discarded: 'bg-gray-100 text-gray-700 border-gray-300',
  cancelled: 'bg-gray-100 text-gray-700 border-gray-300',
  failed: 'bg-red-100 text-red-800 border-red-300',
};

export const num = (n) => (Number.isFinite(Number(n)) ? Number(n).toLocaleString('id-ID') : '-');

const fmtDate = (ymd) => {
  if (!ymd) return '-';
  const d = new Date(`${ymd}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return ymd;
  return d.toLocaleDateString('id-ID', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' });
};

const fmtBytes = (b) => {
  const n = Number(b) || 0;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toLocaleString('id-ID', { maximumFractionDigits: 1 })} MB`;
  if (n >= 1024) return `${Math.round(n / 1024).toLocaleString('id-ID')} KB`;
  return `${n} B`;
};

const fmtDuration = (ms) => {
  const s = Math.round((Number(ms) || 0) / 1000);
  if (s < 60) return `${s} detik`;
  return `${Math.floor(s / 60)} menit ${s % 60} detik`;
};

export const StatusBadge = ({ run }) => (
  <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-semibold ${STATUS_CLS[run.status] || STATUS_CLS.discarded}`}>
    {run.statusLabel || run.status}
  </span>
);

const Card = ({ title, children, right }) => (
  <section className={`${cardClass} p-4 sm:p-5`}>
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-base font-semibold text-gray-900">{title}</h2>
      {right}
    </div>
    {children}
  </section>
);

// Tabel perbandingan jumlah per tingkat: tersimpan vs hasil fetch.
export const DiffTable = ({ diff }) => (
  <div className="overflow-x-auto">
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b text-left text-gray-500">
          <th className="py-2 pr-3 font-medium">Tingkat</th>
          <th className="py-2 pr-3 text-right font-medium">Tersimpan</th>
          <th className="py-2 pr-3 text-right font-medium">Hasil fetch</th>
          <th className="py-2 text-right font-medium">Selisih</th>
        </tr>
      </thead>
      <tbody>
        {LEVELS.map((lv) => {
          const a = Number(diff?.itemsStored?.[lv.key] || 0);
          const b = Number(diff?.itemsNew?.[lv.key] || 0);
          const d = b - a;
          return (
            <tr key={lv.key} className="border-b last:border-0" data-level={lv.key}>
              <td className="py-2 pr-3">{lv.label}</td>
              <td className="py-2 pr-3 text-right tabular-nums">{num(a)}</td>
              <td className="py-2 pr-3 text-right tabular-nums font-semibold">{num(b)}</td>
              <td className={`py-2 text-right tabular-nums ${d < 0 ? 'text-red-700' : d > 0 ? 'text-green-700' : 'text-gray-500'}`}>
                {d > 0 ? `+${num(d)}` : num(d)}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  </div>
);

export const RunSummary = ({ run }) => {
  const diff = run.diff || {};
  const stats = run.stats || {};
  const warnings = diff.warnings || [];
  return (
    <div className="space-y-3" data-testid="region-summary">
      <DiffTable diff={diff} />
      <ul className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
        <li className="rounded-md bg-gray-50 px-3 py-2">
          <div className="text-gray-500">Dataset baru</div>
          <div className="text-lg font-semibold">{num(diff.new)}</div>
        </li>
        <li className="rounded-md bg-gray-50 px-3 py-2">
          <div className="text-gray-500">Dataset berubah</div>
          <div className="text-lg font-semibold">{num(diff.changed)}</div>
        </li>
        <li className="rounded-md bg-gray-50 px-3 py-2">
          <div className="text-gray-500">Dataset hilang</div>
          <div className="text-lg font-semibold">{num(diff.missing)}</div>
        </li>
        <li className="rounded-md bg-gray-50 px-3 py-2">
          <div className="text-gray-500">Tanggal data sumber</div>
          <div className="font-semibold">{fmtDate(run.sourceUpdatedAt)}</div>
        </li>
      </ul>
      <p className="text-xs text-gray-500">
        {num(stats.requests)} permintaan · {num(stats.retries)} retry · {num(stats.failed)} gagal · {fmtDuration(stats.durationMs)}
        {diff.protected > 0 ? ` · ${num(diff.protected)} dataset lama dipertahankan` : ''}
      </p>
      {warnings.length > 0 && (
        <div className="rounded-lg border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900" role="alert" data-testid="region-warnings">
          <p className="flex items-center gap-1.5 font-semibold">
            <Icon name="alert" className="h-4 w-4" /> Periksa sebelum menyimpan:
          </p>
          <ul className="ml-5 list-disc">
            {warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};

const Progress = ({ run, onCancel, busy }) => {
  const [confirm, setConfirm] = useState(false);
  const p = run.progress || {};
  const total = Number(p.total) || 0;
  const done = Number(p.done) || 0;
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
  const stats = run.stats || {};
  return (
    <div className="space-y-3" data-testid="region-progress">
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <span className="font-medium text-gray-800">
          Mengambil data {PHASE_LABEL[p.phase] || 'wilayah'}…
        </span>
        <span className="tabular-nums text-gray-600">
          {num(done)} / {num(total)} permintaan ({pct}%)
        </span>
      </div>
      <div
        className="h-3 w-full overflow-hidden rounded-full bg-gray-200"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct}
        aria-label="Kemajuan fetch data wilayah"
      >
        <div className="h-full rounded-full bg-purple-600 transition-all" style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-gray-500">
        Jumlah total permintaan bertambah saat tingkat berikutnya ditemukan. Retry {num(stats.retries)} · gagal {num(stats.failed)}. Halaman
        boleh ditutup; pengambilan tetap berjalan di server.
      </p>
      {confirm ? (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span>Batalkan pengambilan yang sedang berjalan?</span>
          <button type="button" className={btnDanger} onClick={onCancel} disabled={busy}>
            Ya, batalkan
          </button>
          <button type="button" className={btnSecondary} onClick={() => setConfirm(false)} disabled={busy}>
            Tidak
          </button>
        </div>
      ) : (
        <button type="button" className={btnSecondary} onClick={() => setConfirm(true)} disabled={busy}>
          Batalkan
        </button>
      )}
    </div>
  );
};

const Regions = ({ pollMs = POLL_MS }) => {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [modalRun, setModalRun] = useState(null);
  const prompted = useRef(new Set());

  const load = useCallback(
    () =>
      adminFetch('/regions/status')
        .then((d) => {
          setData(d);
          setError(null);
          return d;
        })
        .catch((e) => {
          setError(e);
          return null;
        }),
    []
  );

  useEffect(() => {
    load();
  }, [load]);

  const active = data?.activeRun || null;

  // Popup konfirmasi muncul sekali per run yang selesai (staged).
  useEffect(() => {
    if (active?.status === 'staged' && !prompted.current.has(active.id)) {
      prompted.current.add(active.id);
      setModalRun(active);
    }
  }, [active]);

  // Polling ringan HANYA selama run berjalan.
  useEffect(() => {
    if (active?.status !== 'running') return undefined;
    const t = setInterval(() => {
      adminFetch(`/regions/runs/${active.id}`)
        .then((r) => {
          const run = r.run;
          if (run.status === 'running') {
            setData((d) => (d ? { ...d, activeRun: run } : d));
          } else {
            load();
          }
        })
        .catch((e) => setError(e));
    }, pollMs);
    return () => clearInterval(t);
  }, [active?.id, active?.status, pollMs, load]);

  const act = async (fn, ok) => {
    setBusy(true);
    setNotice('');
    setError(null);
    try {
      await fn();
      if (ok) setNotice(ok);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const startFetch = () =>
    act(async () => {
      await adminFetch('/regions/fetch', { method: 'POST', body: {} });
      await load();
    }, 'Fetch dimulai. Kemajuan diperbarui otomatis.');

  const save = (run) =>
    act(async () => {
      const d = await adminFetch(`/regions/runs/${run.id}/save`, { method: 'POST', body: {} });
      setModalRun(null);
      setData(d);
    }, 'Data wilayah tersimpan ke database.');

  const discard = (run, msg) =>
    act(async () => {
      const d = await adminFetch(`/regions/runs/${run.id}/discard`, { method: 'POST', body: {} });
      setModalRun(null);
      setData(d);
    }, msg);

  if (!data) return error ? <ErrorBox error={error} /> : <p className="text-gray-500">Memuat...</p>;
  const stored = data.stored || {};
  const runs = data.runs || [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 sm:text-2xl">Data Wilayah</h1>
          <p className="text-sm text-gray-600">
            Provinsi, kabupaten/kota, kecamatan, dan kelurahan/desa untuk alamat checkout. Sumber:{' '}
            <a href="https://wilayah.id/" target="_blank" rel="noopener noreferrer" className="text-purple-700 underline">
              wilayah.id
            </a>
            .
          </p>
        </div>
        <button type="button" className={btnPrimary} onClick={startFetch} disabled={busy || !!active} data-testid="region-fetch">
          <Icon name="refresh" className="h-4 w-4" /> Fetch ulang dari wilayah.id
        </button>
      </div>
      <ErrorBox error={error} />
      {notice && (
        <div className="rounded-lg border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800" role="status">
          {notice}
        </div>
      )}

      {active?.status === 'running' && (
        <Card title={`Fetch #${active.id} sedang berjalan`} right={<StatusBadge run={active} />}>
          <Progress run={active} busy={busy} onCancel={() => discard(active, 'Fetch dibatalkan. Data lama tidak berubah.')} />
        </Card>
      )}

      {active?.status === 'staged' && (
        <Card title={`Hasil fetch #${active.id} menunggu keputusan`} right={<StatusBadge run={active} />}>
          <RunSummary run={active} />
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" className={btnPrimary} onClick={() => setModalRun(active)} disabled={busy}>
              Simpan ke database…
            </button>
            <button type="button" className={btnSecondary} onClick={() => discard(active, 'Hasil fetch dibuang. Data lama tidak berubah.')} disabled={busy}>
              Buang hasil
            </button>
          </div>
          <p className="mt-2 text-xs text-gray-500">Hasil yang tidak diputuskan dalam {data.stagedTtlHours || 24} jam dibuang otomatis saat fetch berikutnya.</p>
        </Card>
      )}

      <Card title="Data tersimpan">
        {stored.hasData ? (
          <div className="space-y-3" data-testid="region-stored">
            <ul className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {LEVELS.map((lv) => (
                <li key={lv.key} className="rounded-md bg-gray-50 px-3 py-2">
                  <div className="text-sm text-gray-500">{lv.label}</div>
                  <div className="text-lg font-semibold tabular-nums">{num(stored.items?.[lv.key])}</div>
                  <div className="text-xs text-gray-500">{num(stored.datasets?.[lv.key])} dataset</div>
                </li>
              ))}
            </ul>
            <dl className="grid grid-cols-1 gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
              <div className="flex gap-2">
                <dt className="text-gray-500">Tanggal data sumber:</dt>
                <dd className="font-medium">{fmtDate(stored.sourceUpdatedAt)}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="text-gray-500">Jumlah dataset:</dt>
                <dd className="font-medium">
                  {num(stored.datasetTotal)} ({fmtBytes(stored.sizeBytes)})
                </dd>
              </div>
              <div className="flex gap-2 sm:col-span-2">
                <dt className="text-gray-500">Terakhir disimpan:</dt>
                <dd className="font-medium break-all">
                  {stored.lastSaved ? `${fmtTime(stored.lastSaved.at)} oleh ${stored.lastSaved.by || '-'}` : '-'}
                </dd>
              </div>
            </dl>
          </div>
        ) : (
          <div className="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-600" data-testid="region-empty">
            Belum ada data wilayah. Checkout pelanggan belum bisa dipakai sampai data disimpan. Tekan{' '}
            <strong>Fetch ulang dari wilayah.id</strong> untuk mengambil data.
          </div>
        )}
        <p className="mt-3 text-xs text-gray-500">
          Fetch penuh mengirim ±8.000 permintaan ke wilayah.id secara pelan (±15 menit). Lakukan paling sering sebulan sekali; data lama tetap
          dipakai sampai hasil baru disimpan.
        </p>
      </Card>

      <Card title="Riwayat fetch (10 terakhir)">
        {runs.length === 0 ? (
          <p className="text-sm text-gray-500">Belum ada riwayat.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-sm" data-testid="region-history">
              <thead>
                <tr className="border-b text-left text-gray-500">
                  <th className="py-2 pr-3 font-medium">#</th>
                  <th className="py-2 pr-3 font-medium">Status</th>
                  <th className="py-2 pr-3 font-medium">Mulai</th>
                  <th className="py-2 pr-3 font-medium">Oleh</th>
                  <th className="py-2 pr-3 font-medium">Jumlah (prov / kab-kota / kec / desa)</th>
                  <th className="py-2 font-medium">Keterangan</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id} className="border-b align-top last:border-0">
                    <td className="py-2 pr-3 tabular-nums">{r.id}</td>
                    <td className="py-2 pr-3">
                      <StatusBadge run={r} />
                    </td>
                    <td className="py-2 pr-3 whitespace-nowrap">{fmtTime(r.startedAt)}</td>
                    <td className="py-2 pr-3 break-all">{r.startedBy}</td>
                    <td className="py-2 pr-3 tabular-nums">
                      {r.counts ? LEVELS.map((lv) => num(r.counts[lv.key])).join(' / ') : '-'}
                    </td>
                    <td className="py-2 text-gray-600">
                      {r.error || (r.status === 'saved' && r.savedAt ? `Disimpan ${fmtTime(r.savedAt)} oleh ${r.savedBy || '-'}` : '')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {modalRun && (
        <Modal title="Simpan data wilayah ke database?" onClose={() => setModalRun(null)} wide>
          <div className="space-y-4" data-testid="region-modal">
            <p className="text-sm text-gray-700">
              Hasil fetch #{modalRun.id} akan menggantikan data wilayah yang dipakai checkout. Hanya dataset yang berubah yang ditulis.
            </p>
            <RunSummary run={modalRun} />
            <p className="text-xs text-gray-500">Batal = hasil fetch dibuang; data lama tidak berubah.</p>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                className={btnSecondary}
                onClick={() => discard(modalRun, 'Hasil fetch dibuang. Data lama tidak berubah.')}
                disabled={busy}
              >
                Batal
              </button>
              <button type="button" className={btnPrimary} onClick={() => save(modalRun)} disabled={busy}>
                {busy ? 'Menyimpan...' : 'Simpan'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
};

export default Regions;
