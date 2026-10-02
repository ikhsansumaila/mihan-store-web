// Utilitas tampilan pesanan: format rupiah, status, tautan & teks WhatsApp.

export const rupiah = (n) => `Rp ${Number(n || 0).toLocaleString('id-ID')}`;

export const STATUS = {
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

// Asal (origin) situs untuk tautan admin di teks WhatsApp; cadangan alamat produksi bila window tidak tersedia.
export const STORE_ORIGIN_FALLBACK = 'https://store.mihan.web.id';
export const siteOrigin = () => {
  const o = typeof window !== 'undefined' && window.location ? window.location.origin : '';
  return /^https?:\/\/[^\s/]+$/i.test(o || '') ? o : STORE_ORIGIN_FALLBACK;
};

// Tautan halaman pesanan di admin berdasarkan NOMOR pesanan (rute admin menerjemahkannya ke id).
export const adminOrderUrl = (orderNo, origin = siteOrigin()) => `${origin}/admin/orders/${encodeURIComponent(orderNo || '')}`;

// Teks konfirmasi pelanggan -> toko (nomor pesanan, item, total, nama, tautan admin).
// Nama = nama PENERIMA di pesanan; nama akun (mis. dari login Google) hanya cadangan bila penerima kosong.
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
    lines.push(`- ${it.name} x${it.qty} = ${rupiah(it.lineTotal)}`);
  });
  lines.push('');
  if (order.discount > 0) lines.push(`Diskon: -${rupiah(order.discount)}`);
  if (order.shippingFee > 0) lines.push(`Ongkir: ${rupiah(order.shippingFee)}`);
  lines.push(`Total: ${rupiah(order.total)}`);
  lines.push(`Status: ${statusLabel(order.status)}`);
  lines.push('');
  if (order.orderNo) {
    lines.push(`Buka di admin: ${adminOrderUrl(order.orderNo)}`);
    lines.push('');
  }
  lines.push('Terima kasih.');
  return lines.join('\n');
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
    lines.push(`   ${it.qty} x ${rupiah(it.unitPrice)} = ${rupiah(it.lineTotal)}`);
  });
  lines.push('');
  lines.push(`Subtotal: ${rupiah(order.subtotal)}`);
  if (order.discount > 0) {
    lines.push(`Diskon${order.discountNote ? ` (${order.discountNote})` : ''}: -${rupiah(order.discount)}`);
  }
  lines.push(`Ongkir: ${order.shippingFee > 0 ? rupiah(order.shippingFee) : 'Rp 0'}`);
  lines.push(`*Total: ${rupiah(order.total)}*`);
  lines.push(`Status: ${statusLabel(order.status)}`);
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
