import jsPDF from 'jspdf';

// Generator PDF invoice (jsPDF, di browser). Dipakai oleh:
//  - Buat Invoice manual (/admin/invoice): { customerName, items, isLunas } -> tata letak sama seperti sebelumnya.
//  - Cetak invoice dari pesanan (admin): tambahan nomor pesanan, data penerima, subtotal/diskon/ongkir,
//    dan info rekening dari Pengaturan Toko.

export const formatCurrency = (value) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', minimumFractionDigits: 0 }).format(value);

// Rekening default invoice manual (sama seperti sebelumnya).
export const DEFAULT_PAYMENT = { bankName: 'BCA', accountNumber: '3452271335', accountHolder: 'QOMARIAH AKMALA' };

const loadImage = (url) =>
  new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = 'Anonymous';
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = url;
  });

// Ubah pesanan (respons GET /api/admin/orders/{id}) menjadi parameter invoice.
export const orderToInvoice = (order, settings = {}) => {
  const payment =
    settings.bank_name && settings.bank_account_number && settings.bank_account_holder
      ? { bankName: settings.bank_name, accountNumber: settings.bank_account_number, accountHolder: settings.bank_account_holder }
      : DEFAULT_PAYMENT;
  return {
    customerName: order.recipient?.name || order.customer?.name || '-',
    items: (order.items || []).map((it) => ({ name: it.name, qty: it.qty, price: it.unitPrice, total: it.lineTotal })),
    isLunas: order.status === 'paid' || order.status === 'completed',
    orderNo: order.orderNo,
    date: order.createdAt ? new Date(order.createdAt) : new Date(),
    subtotal: order.subtotal,
    discount: order.discount || 0,
    discountNote: order.discountNote || '',
    shippingFee: order.shippingFee || 0,
    total: order.total,
    recipient: order.recipient,
    payment,
  };
};

/**
 * Membuat PDF invoice. Mengembalikan objek jsPDF (dan membuka tab baru bila open=true).
 * opts.images = { logo, lunas } untuk melewati pemuatan gambar (tes).
 */
export async function generateInvoicePdf(inv, opts = {}) {
  const { customerName, items, isLunas } = inv;
  const isOrder = !!inv.orderNo;
  const payment = inv.payment || DEFAULT_PAYMENT;
  const calculateTotal = () => items.reduce((sum, item) => sum + item.total, 0);

  const [logoImgData, lunasImgData] = opts.images
    ? [opts.images.logo || null, isLunas ? opts.images.lunas || null : null]
    : await Promise.all([loadImage('/mihan-store-logo.png'), isLunas ? loadImage('/lunas-logo.png') : Promise.resolve(null)]);

  const doc = new jsPDF({ orientation: 'portrait', unit: 'mm', format: 'a4' });
  const darkGray = '#404040';
  const orangeColor = '#F1A038';
  const drawLine = (y) => {
    doc.setDrawColor(orangeColor);
    doc.setLineWidth(0.5);
    doc.line(10, y, 200, y);
  };

  // === HEADER ===
  if (logoImgData) {
    const targetWidth = 35;
    const proportionalHeight = (logoImgData.naturalHeight / logoImgData.naturalWidth) * targetWidth;
    doc.addImage(logoImgData, 'PNG', 15, 12, targetWidth, proportionalHeight);
  }
  const adjustHeight = 5;

  doc.setFontSize(28);
  doc.setFont('helvetica', 'bold');
  doc.setTextColor(darkGray);
  doc.text('I N V O I C E', 105, 25 + adjustHeight, { align: 'center' });

  drawLine(42 + adjustHeight);

  // === BILLING INFO ===
  doc.setFontSize(11);
  doc.setFont('helvetica', 'bold');
  doc.text('INVOICE TO', 15, 50 + adjustHeight);
  doc.text(`: ${customerName.toUpperCase()}`, 50, 50 + adjustHeight);

  const dateStr = (inv.date || new Date()).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
  doc.text('DATE', 15, 57 + adjustHeight);
  doc.text(`: ${dateStr.toUpperCase()}`, 50, 57 + adjustHeight);

  // Tambahan khusus invoice pesanan: nomor pesanan + alamat kirim.
  let extra = 0;
  if (isOrder) {
    doc.text('NO. PESANAN', 15, 64 + adjustHeight);
    doc.text(`: ${inv.orderNo}`, 50, 64 + adjustHeight);
    extra = 7;
    const r = inv.recipient;
    if (r) {
      doc.setFont('helvetica', 'normal');
      doc.setFontSize(10);
      doc.text('KIRIM KE', 120, 50 + adjustHeight);
      const lines = [
        r.name,
        r.phone,
        ...doc.splitTextToSize(String(r.address || '').replace(/\s*\n\s*/g, ', '), 75),
        `${r.city || ''}${r.postalCode ? ` ${r.postalCode}` : ''}`,
      ].filter(Boolean);
      lines.slice(0, 7).forEach((l, i) => doc.text(String(l), 120, 55 + adjustHeight + i * 4.5));
      extra = Math.max(extra, Math.min(lines.length, 7) * 4.5 - 2);
      doc.setFont('helvetica', 'bold');
      doc.setFontSize(11);
    }
  }

  // === TABLE HEADER ===
  const tableTop = 63 + adjustHeight + extra;
  doc.setFillColor(orangeColor);
  doc.rect(10, tableTop, 190, 10, 'F');

  doc.setTextColor('#FFFFFF');
  doc.setFontSize(11);
  doc.text('NAMA PRODUK', 15, tableTop + 7);
  doc.text('JUMLAH', 100, tableTop + 7, { align: 'center' });
  doc.text('HARGA', 140, tableTop + 7, { align: 'center' });
  doc.text('TOTAL', 195, tableTop + 7, { align: 'right' });

  // === TABLE ITEMS ===
  doc.setTextColor(darkGray);
  doc.setFont('helvetica', 'normal');
  doc.setFontSize(10);

  let currentY = tableTop + 17;
  items.forEach((item) => {
    if (currentY > 230) {
      doc.addPage();
      currentY = 20;
    }
    const splitName = doc.splitTextToSize(item.name.toUpperCase(), 70);
    doc.text(splitName, 15, currentY);
    doc.text(`${item.qty} PCS`, 100, currentY, { align: 'center' });
    doc.text(formatCurrency(item.price), 140, currentY, { align: 'center' });
    doc.text(formatCurrency(item.total), 195, currentY, { align: 'right' });
    currentY += splitName.length * 5 + 3;
  });

  currentY += 5;
  drawLine(currentY);

  // === SUBTOTAL / DISKON / ONGKIR (pesanan) ===
  if (isOrder) {
    doc.setFont('helvetica', 'normal');
    doc.setFontSize(10);
    currentY += 7;
    doc.text('SUBTOTAL', 140, currentY, { align: 'center' });
    doc.text(formatCurrency(inv.subtotal ?? calculateTotal()), 195, currentY, { align: 'right' });
    if (inv.discount > 0) {
      currentY += 6;
      const label = inv.discountNote ? `DISKON (${inv.discountNote})` : 'DISKON';
      doc.text(doc.splitTextToSize(label.toUpperCase(), 70)[0], 140, currentY, { align: 'center' });
      doc.text(`-${formatCurrency(inv.discount)}`, 195, currentY, { align: 'right' });
    }
    currentY += 6;
    doc.text('ONGKIR', 140, currentY, { align: 'center' });
    doc.text(formatCurrency(inv.shippingFee || 0), 195, currentY, { align: 'right' });
  }

  // === TOTAL ===
  currentY += 8;
  doc.setFont('helvetica', 'bold');
  doc.setFontSize(12);
  doc.text('TOTAL', 140, currentY, { align: 'center' });
  doc.text(formatCurrency(isOrder ? inv.total : calculateTotal()), 195, currentY, { align: 'right' });

  currentY += 5;
  drawLine(currentY);

  // === PAYMENT INFO ===
  currentY += 15;
  doc.setFontSize(11);
  doc.text('PEMBAYARAN KE:', 15, currentY);
  currentY += 7;
  doc.text(String(payment.bankName).toUpperCase(), 15, currentY);
  currentY += 6;
  doc.text(String(payment.accountNumber), 15, currentY);
  currentY += 6;
  doc.text(String(payment.accountHolder).toUpperCase(), 15, currentY);

  // === LUNAS STAMP ===
  if (isLunas) {
    if (lunasImgData) {
      const targetWidthLunas = 45;
      const proportionalHeightLunas = (lunasImgData.naturalHeight / lunasImgData.naturalWidth) * targetWidthLunas;
      doc.addImage(lunasImgData, 'PNG', 150, currentY - 25, targetWidthLunas, proportionalHeightLunas);
    } else {
      doc.setTextColor(220, 53, 69);
      doc.setFontSize(28);
      doc.text('LUNAS', 195, currentY - 5, { align: 'right', angle: 10 });
    }
  }

  // === FOOTER ===
  const pageHeight = doc.internal.pageSize.height;
  doc.setFillColor(orangeColor);
  doc.rect(0, pageHeight - 24, 210, 24, 'F');
  doc.setTextColor('#000000');
  doc.setFontSize(10);
  doc.setFont('helvetica', 'bold');
  doc.text('PT. MIHAN JAYA BERKAH', 105, pageHeight - 14, { align: 'center' });
  doc.setFontSize(9);
  doc.setFont('helvetica', 'normal');
  doc.text('Jl. Masjid Raudhatul Jannah, Sudimara Pinang, Pinang, Tangerang', 105, pageHeight - 8, { align: 'center' });

  if (opts.open !== false) {
    const pdfUrl = doc.output('bloburl');
    window.open(pdfUrl, '_blank');
  }
  return doc;
}
