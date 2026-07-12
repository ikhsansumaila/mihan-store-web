import jsPDF from 'jspdf';
import { useState } from 'react';

const InvoiceCreate = () => {
  const [customerName, setCustomerName] = useState('');
  const [items, setItems] = useState([]);
  const [isLunas, setIsLunas] = useState(false);

  // Form inputs
  const [productName, setProductName] = useState('');
  const [qty, setQty] = useState('');
  const [price, setPrice] = useState('');

  // Format currency ke Rupiah
  const formatCurrency = (value) => {
    return new Intl.NumberFormat('id-ID', {
      style: 'currency',
      currency: 'IDR',
      minimumFractionDigits: 0,
    }).format(value);
  };

  // Hitung total
  const calculateTotal = () => {
    return items.reduce((sum, item) => sum + item.total, 0);
  };

  // Tambah item ke list
  const addItem = () => {
    const qtyNum = parseInt(qty.replace(/[^0-9]/g, '')) || 0;
    const priceNum = parseInt(price.replace(/[^0-9]/g, '')) || 0;

    if (!productName || qtyNum <= 0 || priceNum <= 0) {
      alert('Mohon isi nama produk, qty, dan harga dengan benar.');
      return;
    }

    const newItem = {
      name: productName,
      qty: qtyNum,
      price: priceNum,
      total: qtyNum * priceNum,
    };

    setItems([...items, newItem]);
    setProductName('');
    setQty('');
    setPrice('');
  };

  // Hapus item dari list
  const removeItem = (index) => {
    setItems(items.filter((_, i) => i !== index));
  };

  // Generate PDF menggunakan jsPDF (Frontend-side)
  const previewPDF = () => {
    if (!customerName || items.length === 0) {
      alert('Mohon isi nama customer dan minimal masukkan 1 item');
      return;
    }

    const doc = new jsPDF({
      orientation: 'portrait',
      unit: 'mm',
      format: 'a4'
    });

    const darkGray = '#404040';
    const orangeColor = '#F1A038';

    // Helper: Draw Orange Line
    const drawLine = (y) => {
      doc.setDrawColor(orangeColor);
      doc.setLineWidth(0.5);
      doc.line(10, y, 200, y);
    };

    // Fungsi utama render PDF
    const generate = (logoImgData, lunasImgData) => {
      // === HEADER ===
      if (logoImgData) {
        // Hitung aspek rasio logo MihanStore agar tidak gepeng
        const targetWidth = 35; // Lebar logo di PDF (mm)
        // Rumus: (Tinggi Asli / Lebar Asli) * Lebar Target
        const proportionalHeight = (logoImgData.naturalHeight / logoImgData.naturalWidth) * targetWidth;

        doc.addImage(logoImgData, 'PNG', 15, 12, targetWidth, proportionalHeight);
      }
      const adjustHeight = 5

      doc.setFontSize(28);
      doc.setFont('helvetica', 'bold');
      doc.setTextColor(darkGray);
      doc.text("I N V O I C E", 105, 25 + adjustHeight, { align: 'center' });

      drawLine(42 + adjustHeight);

      // === BILLING INFO ===
      doc.setFontSize(11);
      doc.setFont('helvetica', 'bold');
      doc.text("INVOICE TO", 15, 50 + adjustHeight);  // Ubah dari 45 jadi 50 (tambah jarak 5mm)
      doc.text(`: ${customerName.toUpperCase()}`, 50, 50 + adjustHeight);

      const dateStr = new Date().toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
      doc.text("DATE", 15, 57 + adjustHeight);  // Ubah dari 52 jadi 57
      doc.text(`: ${dateStr.toUpperCase()}`, 50, 57 + adjustHeight);

      // === TABLE HEADER ===
      doc.setFillColor(orangeColor); // Orange
      doc.rect(10, 63 + adjustHeight, 190, 10, 'F');

      doc.setTextColor('#FFFFFF');
      doc.setFontSize(11);
      doc.text("NAMA PRODUK", 15, 70 + adjustHeight);
      doc.text("JUMLAH", 100, 70 + adjustHeight, { align: 'center' });
      doc.text("HARGA", 140, 70 + adjustHeight, { align: 'center' });
      doc.text("TOTAL", 195, 70 + adjustHeight, { align: 'right' });

      // === TABLE ITEMS ===
      doc.setTextColor(darkGray);
      doc.setFont('helvetica', 'normal');
      doc.setFontSize(10);

      let currentY = 80 + adjustHeight;
      items.forEach((item) => {
        // Check page break
        if (currentY > 230) {
          doc.addPage();
          currentY = 20;
        }

        // text can be long, so split if necessary
        const splitName = doc.splitTextToSize(item.name.toUpperCase(), 70);
        doc.text(splitName, 15, currentY);

        doc.text(`${item.qty} PCS`, 100, currentY, { align: 'center' });
        doc.text(formatCurrency(item.price), 140, currentY, { align: 'center' });
        doc.text(formatCurrency(item.total), 195, currentY, { align: 'right' });

        currentY += (splitName.length * 5) + 3;
      });

      currentY += 5;
      drawLine(currentY);

      // === TOTAL ===
      currentY += 8;
      doc.setFont('helvetica', 'bold');
      doc.setFontSize(12);
      doc.text("TOTAL", 140, currentY, { align: 'center' });
      doc.text(formatCurrency(calculateTotal()), 195, currentY, { align: 'right' });

      currentY += 5;
      drawLine(currentY);

      // === PAYMENT INFO ===
      currentY += 15;
      doc.setFontSize(11);
      doc.text("PEMBAYARAN KE:", 15, currentY);
      currentY += 7;
      doc.text("BCA", 15, currentY);
      currentY += 6;
      doc.text("3452271335", 15, currentY);
      currentY += 6;
      doc.text("QOMARIAH AKMALA", 15, currentY);

      // === LUNAS STAMP ===
      if (isLunas) {
        if (lunasImgData) {
          // Hitung aspek rasio agar tidak gepeng
          const targetWidthLunas = 45;
          const proportionalHeightLunas = (lunasImgData.naturalHeight / lunasImgData.naturalWidth) * targetWidthLunas;

          doc.addImage(lunasImgData, 'PNG', 150, currentY - 25, targetWidthLunas, proportionalHeightLunas);
        } else {
          doc.setTextColor(220, 53, 69);
          doc.setFontSize(28);
          doc.text("LUNAS", 195, currentY - 5, { align: 'right', angle: 10 });
        }
      }

      // === FOOTER ===
      const pageHeight = doc.internal.pageSize.height;

      doc.setFillColor(orangeColor);
      doc.rect(0, pageHeight - 24, 210, 24, 'F');

      doc.setTextColor('#000000');
      doc.setFontSize(10);
      doc.setFont('helvetica', 'bold');
      doc.text("PT. MIHAN JAYA BERKAH", 105, pageHeight - 14, { align: 'center' });

      doc.setFontSize(9);
      doc.setFont('helvetica', 'normal');
      doc.text("Jl. Masjid Raudhatul Jannah, Sudimara Pinang, Pinang, Tangerang", 105, pageHeight - 8, { align: 'center' });

      // Output PDF
      const pdfUrl = doc.output('bloburl');
      window.open(pdfUrl, '_blank');
    };

    // Helper loading image
    const loadImage = (url) => {
      return new Promise((resolve) => {
        const img = new Image();
        img.crossOrigin = 'Anonymous';
        img.onload = () => resolve(img);
        img.onerror = () => resolve(null); // Jika error, return null
        img.src = url;
      });
    };

    // Load both images sequentially
    Promise.all([
      loadImage('/mihan-store-logo.png'),
      isLunas ? loadImage('/lunas-logo.png') : Promise.resolve(null)
    ]).then(([logo, lunas]) => {
      generate(logo, lunas);
    });
  };

  // Kirim WhatsApp
  const sendWhatsApp = () => {
    if (!customerName || items.length === 0) {
      alert('Mohon isi nama customer dan minimal masukkan 1 item');
      return;
    }

    let text = `*INVOICE MIHANSTORE*\n\n`;
    text += `*Pelanggan:* ${customerName}\n`;
    text += `*Status:* ${isLunas ? 'LUNAS' : 'BELUM LUNAS'}\n\n`;
    text += `*Detail Pesanan:*\n`;

    items.forEach((item, index) => {
      text += `${index + 1}. ${item.name}\n`;
      text += `   ${item.qty} x ${formatCurrency(item.price)} = ${formatCurrency(item.total)}\n`;
    });

    text += `\n*TOTAL:* ${formatCurrency(calculateTotal())}\n\n`;
    text += `Terima kasih telah berbelanja di MihanStore!`;

    const encodedText = encodeURIComponent(text);
    window.open(`https://wa.me/?text=${encodedText}`, '_blank');
  };

  // Handle format currency input
  const handlePriceChange = (e) => {
    // Hanya ambil angka
    const value = e.target.value.replace(/[^0-9]/g, '');
    setPrice(value);
  };

  // Format angka dengan titik pemisah ribuan untuk input
  const formatNumber = (numString) => {
    if (!numString) return '';
    return numString.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  };

  return (
    <div className="max-w-4xl mx-auto px-4 py-8">
      <div className="bg-white rounded-xl shadow-xl p-8">
        <h2 className="text-3xl font-bold mb-6 text-gray-800 border-b pb-4">Buat Invoice Baru</h2>

        {/* Customer Name */}
        <div className="mb-6">
          <label className="block mb-2 font-medium text-gray-700">Nama Pelanggan / Customer</label>
          <input
            type="text"
            value={customerName}
            onChange={(e) => setCustomerName(e.target.value)}
            placeholder="Nama Pelanggan"
            className="w-full px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
          />
        </div>

        {/* Input Item */}
        <div className="mb-6 bg-purple-50 p-6 rounded-lg border border-purple-100">
          <h3 className="text-lg font-semibold text-purple-800 mb-4">Tambah Item Belanja</h3>
          <div className="grid grid-cols-1 md:grid-cols-12 gap-4 items-end">
            <div className="md:col-span-5">
              <label className="block mb-1 text-sm font-medium text-gray-700">Nama Produk</label>
              <input
                type="text"
                value={productName}
                onChange={(e) => setProductName(e.target.value)}
                placeholder="Misal: Tepung Terigu"
                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:border-purple-600 bg-white"
              />
            </div>
            <div className="md:col-span-2">
              <label className="block mb-1 text-sm font-medium text-gray-700">Qty</label>
              <input
                type="number"
                value={qty}
                onChange={(e) => setQty(e.target.value)}
                placeholder="1"
                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:border-purple-600 bg-white"
              />
            </div>
            <div className="md:col-span-5">
              <label className="block mb-1 text-sm font-medium text-gray-700">Harga Satuan (Rp)</label>
              <div className="flex gap-2 relative">
                <span className="absolute left-3 top-2.5 text-gray-500 font-medium">Rp</span>
                <input
                  type="text"
                  value={formatNumber(price)}
                  onChange={handlePriceChange}
                  placeholder="0"
                  className="w-full pl-9 pr-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:border-purple-600 bg-white font-medium"
                />
                <button
                  type="button"
                  onClick={addItem}
                  className="bg-purple-600 text-white px-2 py-2 rounded-md font-bold hover:bg-purple-700 transition flex-shrink-0"
                >
                  Tambah
                </button>
              </div>
            </div>
          </div>
        </div>

        {/* List Items */}
        <div className="mb-6 border rounded-lg overflow-hidden">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="bg-gray-100 text-gray-700 font-semibold border-b">
                <th className="p-4">Nama Produk</th>
                <th className="p-4 text-center">Qty</th>
                <th className="p-4 text-right">Harga Satuan</th>
                <th className="p-4 text-right">Subtotal</th>
                <th className="p-4 text-center">Aksi</th>
              </tr>
            </thead>
            <tbody>
              {items.length === 0 ? (
                <tr>
                  <td colSpan="5" className="p-8 text-center text-gray-500">
                    Belum ada item belanja. Tambahkan produk di atas.
                  </td>
                </tr>
              ) : (
                items.map((item, index) => (
                  <tr key={index} className="border-b hover:bg-gray-50 transition-colors">
                    <td className="p-4 font-medium text-gray-800">{item.name}</td>
                    <td className="p-4 text-center text-gray-600">{item.qty}</td>
                    <td className="p-4 text-right text-gray-600">{formatCurrency(item.price)}</td>
                    <td className="p-4 text-right font-semibold text-gray-800">{formatCurrency(item.total)}</td>
                    <td className="p-4 text-center">
                      <button
                        onClick={() => removeItem(index)}
                        className="text-red-600 hover:text-red-800 font-medium transition"
                      >
                        Hapus
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Total & Checkbox */}
        <div className="flex flex-col md:flex-row justify-between items-start md:items-center mb-8 gap-4 border-t pt-6">
          <div className="flex items-center">
            <input
              id="lunas-checkbox"
              type="checkbox"
              checked={isLunas}
              onChange={(e) => setIsLunas(e.target.checked)}
              className="w-5 h-5 text-purple-600 border-gray-300 rounded focus:ring-purple-500 cursor-pointer"
            />
            <label htmlFor="lunas-checkbox" className="ml-2 font-medium text-gray-700 cursor-pointer">
              Pembayaran Sudah Lunas?
            </label>
          </div>
          <div className="text-right">
            <span className="text-gray-600 block text-sm">Total Tagihan:</span>
            <span className="text-3xl font-extrabold text-purple-700">{formatCurrency(calculateTotal())}</span>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-col sm:flex-row gap-4">
          <button
            onClick={previewPDF}
            className="flex-1 bg-purple-600 text-white py-3 rounded-lg font-semibold hover:bg-purple-700 transition shadow-md hover:shadow-lg flex items-center justify-center gap-2"
          >
            📄 Preview PDF
          </button>
          <button
            onClick={sendWhatsApp}
            className="flex-1 bg-green-500 text-white py-3 rounded-lg font-semibold hover:bg-green-600 transition shadow-md hover:shadow-lg flex items-center justify-center gap-2"
          >
            💬 Kirim WhatsApp
          </button>
        </div>
      </div>
    </div>
  );
};

export default InvoiceCreate;