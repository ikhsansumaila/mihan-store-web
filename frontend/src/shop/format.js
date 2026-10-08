// Utilitas tampilan bersama (toko & admin): format rupiah, harga per satuan, status, tautan & teks WhatsApp.
// Satu-satunya tempat format rupiah untuk UI (admin/api.js mengekspor ulang dari sini).

export const rupiah = (n) => `Rp ${Number(n || 0).toLocaleString('id-ID')}`;

// "Rp 42.000 / pak" (tanpa satuan: "Rp 42.000").
export const perUnit = (price, unit) => (unit ? `${rupiah(price)} / ${unit}` : rupiah(price));

// Catatan harga grosir untuk item pesanan/keranjang yang memakai jenjang.
export const tierNote = (minQty) => (minQty ? `harga grosir (min. ${minQty})` : '');

// Urutan = urutan tab status admin (setelah "Semua").
export const STATUS = {
  pending_confirmation: { label: 'Menunggu konfirmasi', cls: 'bg-orange-100 text-orange-800 border-orange-300' },
  pending_payment: { label: 'Menunggu pembayaran', cls: 'bg-yellow-100 text-yellow-800 border-yellow-300' },
  paid: { label: 'Dibayar', cls: 'bg-blue-100 text-blue-800 border-blue-300' },
  completed: { label: 'Selesai', cls: 'bg-green-100 text-green-800 border-green-300' },
  cancelled: { label: 'Dibatalkan', cls: 'bg-gray-200 text-gray-700 border-gray-300' },
};

export const statusLabel = (s) => STATUS[s]?.label || s || '-';

export const fmtDateTime = (iso) => {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Jakarta' });
  } catch {
    return iso;
  }
};

// Alamat multi-baris menjadi satu baris ("a\nb" -> "a, b"), spasi dirapikan.
export const flattenAddress = (s) =>
  String(s || '')
    .split(/\r\n|\r|\n/)
    .map((p) => p.split(/\s+/).join(' ').trim().replace(/^[,\s]+|[,\s]+$/g, ''))
    .filter(Boolean)
    .join(', ');

// Alamat tersusun penerima (sama dengan backend composeAddress):
//   "<alamat lengkap>, <kelurahan/desa>, Kec. <kecamatan>, <kab/kota>, <provinsi> <kode pos>"
// Pesanan lama tanpa wilayah: "<alamat>, <kota> <kode pos>".
export const formatFullAddress = (recipient) => {
  const r = recipient || {};
  const parts = [];
  const a = flattenAddress(r.address);
  if (a) parts.push(a);
  const reg = r.region;
  let last;
  if (reg && reg.village?.name) {
    parts.push(reg.village.name, `Kec. ${reg.district?.name || ''}`, reg.regency?.name || '');
    last = reg.province?.name || '';
  } else {
    last = String(r.city || '').trim();
  }
  const pc = String(r.postalCode || '').trim();
  if (pc) last = `${last} ${pc}`.trim();
  if (last) parts.push(last);
  return parts.filter(Boolean).join(', ');
};

// Nomor untuk wa.me: hanya digit, format internasional 62xxxx. Mengembalikan '' bila tidak valid.
export const waNumber = (phone) => {
  if (!phone) return '';
  let d = String(phone).replace(/[^0-9]/g, '');
  if (d.startsWith('0')) d = `62${d.slice(1)}`;
  if (!/^628[0-9]{6,13}$/.test(d)) return '';
  return d;
};

export const waLink = (phone, text) => {
  const n = waNumber(phone);
  if (!n) return '';
  return `https://wa.me/${n}?text=${encodeURIComponent(text)}`;
};

// Asal (origin) situs untuk tautan (admin / halaman pesanan pelanggan); cadangan alamat produksi bila window tidak tersedia.
export const STORE_ORIGIN_FALLBACK = 'https://store.mihan.web.id';
export const siteOrigin = () => {
  const o = typeof window !== 'undefined' && window.location ? window.location.origin : '';
  return /^https?:\/\/[^\s/]+$/i.test(o || '') ? o : STORE_ORIGIN_FALLBACK;
};

// Tautan halaman pesanan di admin berdasarkan NOMOR pesanan (rute admin menerjemahkannya ke id).
export const adminOrderUrl = (orderNo, origin = siteOrigin()) => `${origin}/admin/orders/${encodeURIComponent(orderNo || '')}`;

// Tautan halaman pesanan milik PELANGGAN (rute toko /pesanan/:orderNo).
export const customerOrderUrl = (orderNo, origin = siteOrigin()) => `${origin}/pesanan/${encodeURIComponent(orderNo || '')}`;

// Teks konfirmasi pelanggan -> toko: nomor pesanan, nama, dan daftar item (jumlah + satuan + tanda grosir).
// Sengaja TANPA nominal (harga, diskon, ongkir, total), status, maupun tautan admin; toko yang menginfokan ongkir & total.
// Di bawah kalimat penutup: tautan halaman pesanan pelanggan (/pesanan/<no>) bila nomor pesanan ada.
// Nama = nama PENERIMA di pesanan; nama akun (mis. dari login Google) hanya cadangan bila penerima kosong.
export const CUSTOMER_CONFIRM_CLOSING = 'Mohon infokan terkait ongkir dan total yang harus saya bayar, Terima Kasih';
export const buildCustomerConfirmText = (order, accountName) => {
  const name = String(order.recipient?.name || '').trim() || String(accountName || '').trim() || '-';
  const lines = [
    'Halo Mihan Store, saya ingin konfirmasi pesanan:',
    '',
    `No. pesanan: ${order.orderNo}`,
    `Nama: ${name}`,
    '',
    'Item:',
  ];
  (order.items || []).forEach((it) => {
    const qty = it.unit ? `${it.qty} ${it.unit}` : `${it.qty}`;
    lines.push(`- ${it.name} x${qty}${it.tierMinQty ? ' (harga grosir)' : ''}`);
  });
  lines.push('');
  lines.push(CUSTOMER_CONFIRM_CLOSING);
  if (order.orderNo) {
    lines.push('');
    lines.push(customerOrderUrl(order.orderNo));
  }
  return lines.join('\n');
};

// Teks WhatsApp pelanggan -> toko setelah mengunggah bukti transfer (status menunggu pembayaran).
// Nama = nama penerima (cadangan nama akun) seperti buildCustomerConfirmText. TANPA nominal dan TANPA tautan
// (admin melihat bukti di panel admin).
export const buildPaymentConfirmText = (order, accountName) => {
  const name = String(order.recipient?.name || '').trim() || String(accountName || '').trim() || '-';
  return `Halo Mihan Store, saya sudah transfer untuk pesanan ${order.orderNo} atas nama ${name}. Tolong periksa bukti pembayaran. Terima kasih.`;
};

// Ringkasan admin -> pelanggan (nomor penerima).
export const buildAdminSummaryText = (order, store = {}) => {
  const lines = [
    `*Mihan Store - Pesanan ${order.orderNo}*`,
    '',
    `Halo ${order.recipient?.name || ''},`.trim(),
    'Berikut ringkasan pesanan Anda:',
    '',
  ];
  (order.items || []).forEach((it, i) => {
    lines.push(`${i + 1}. ${it.name}`);
    lines.push(`   ${it.qty} x ${perUnit(it.unitPrice, it.unit)} = ${rupiah(it.lineTotal)}${it.tierMinQty ? ` (harga grosir min. ${it.tierMinQty})` : ''}`);
  });
  lines.push('');
  lines.push(`Subtotal: ${rupiah(order.subtotal)}`);
  if (order.discount > 0) {
    lines.push(`Diskon${order.discountNote ? ` (${order.discountNote})` : ''}: -${rupiah(order.discount)}`);
  }
  lines.push(`Ongkir: ${order.shippingFee > 0 ? rupiah(order.shippingFee) : 'Rp 0'}`);
  lines.push(`*Total: ${rupiah(order.total)}*`);
  lines.push(`Status: ${statusLabel(order.status)}`);
  const addr = formatFullAddress(order.recipient);
  if (addr) {
    lines.push('');
    lines.push('Alamat pengiriman:');
    lines.push(addr);
  }
  // Rekening hanya setelah admin mengonfirmasi (menunggu pembayaran); TIDAK untuk menunggu konfirmasi.
  if (order.status === 'pending_payment' && store.bank_name && store.bank_account_number) {
    lines.push('');
    lines.push('Pembayaran ke rekening:');
    lines.push(`${store.bank_name} ${store.bank_account_number}`);
    if (store.bank_account_holder) lines.push(`a.n. ${store.bank_account_holder}`);
  }
  lines.push('');
  lines.push('Terima kasih telah berbelanja di Mihan Store!');
  return lines.join('\n');
};

// UUID v4 untuk idempotencyKey (crypto.randomUUID bila tersedia).
export const newIdempotencyKey = () => {
  const c = typeof window !== 'undefined' ? window.crypto : undefined;
  if (c?.randomUUID) return c.randomUUID();
  const b = new Uint8Array(16);
  if (c?.getRandomValues) c.getRandomValues(b);
  else for (let i = 0; i < 16; i += 1) b[i] = Math.floor(Math.random() * 256);
  b[6] = (b[6] & 0x0f) | 0x40; // eslint-disable-line no-bitwise
  b[8] = (b[8] & 0x3f) | 0x80; // eslint-disable-line no-bitwise
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
};
