import { generateInvoicePdf, formatCurrency as fmtIDR } from '../invoicePdf';
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
  const formatCurrency = fmtIDR;

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

  // Generate PDF menggunakan jsPDF (Frontend-side), generator bersama di src/invoicePdf.js
  const previewPDF = () => {
    if (!customerName || items.length === 0) {
      alert('Mohon isi nama customer dan minimal masukkan 1 item');
      return;
    }
    generateInvoicePdf({ customerName, items, isLunas });
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

    text += `\n*TOTAL:* ${formatCurrency(calculateTotal())}\n`;
    text += `\nPembayaran Ke Rekening:`;
    text += `\nBank BCA`;
    text += `\n3452271335`;
    text += `\nA.n: Qomariah Akmala\n\n`;
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
    // Dirender di dalam layout admin (/admin/invoice); padding luar sudah dari layout.
    <div className="max-w-4xl">
      <h1 className="text-2xl font-bold text-gray-800 mb-4">Buat Invoice</h1>
      <div className="bg-white rounded-xl shadow p-4 sm:p-8">

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
        <div className="mb-6 bg-purple-50 p-4 sm:p-6 rounded-lg border border-purple-100">
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
                  aria-label="Harga Satuan (Rp)"
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
        <div className="mb-6 border rounded-lg overflow-x-auto">
          <table className="w-full min-w-[560px] text-left border-collapse">
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
          <div className="w-full md:w-auto text-right">
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