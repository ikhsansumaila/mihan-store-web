import React, { useState, useEffect, useCallback, useRef } from 'react';
import { BrowserRouter as Router, Routes, Route, Link, useNavigate, useLocation } from 'react-router-dom';
import axios from 'axios';
import Login from './components/Login';
import Register from './components/Register';
import CompleteProfile from './components/CompleteProfile';
import { PrivacyPolicy, TermsOfService } from './components/Legal';
import AdminApp from './admin/AdminApp';
import { getStoredUser, verifySession, logoutRequest, clearSession, saveSession, setReturnTo, errorMessage } from './auth';
import { CartProvider, useCart } from './shop/CartContext';
import { addCartItem, isUnauthorized } from './shop/api';
import Cart from './shop/Cart';
import Checkout from './shop/Checkout';
import { MyOrders, OrderDetail } from './shop/Orders';
import { perUnit, rupiah } from './shop/format';
import { safeImageUrl } from './shop/productImage';

export { safeImageUrl };

const API_BASE_URL = '/api';

// Ikon keranjang (SVG inline, dekoratif) untuk tombol "Tambah" di kartu produk (HP).
const CartGlyph = ({ className }) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className}
    aria-hidden="true"
    focusable="false"
    data-testid="add-to-cart-icon"
  >
    <circle cx="9" cy="21" r="1" />
    <circle cx="20" cy="21" r="1" />
    <path d="M1 1h4l2.68 13.39a2 2 0 0 0 2 1.61h9.72a2 2 0 0 0 2-1.61L23 6H6" />
  </svg>
);

// URL gambar kartu produk: thumbnail (sisi terpanjang 400 px, dari /api/products `thumb`) bila ada,
// selain itu foto utama (`image`).
export const productImageUrl = (product) => safeImageUrl(product?.thumb) || safeImageUrl(product?.image);

// Area gambar kartu: gambar (object-cover) bila ada; placeholder bila tidak ada atau gagal dimuat.
// >= 640px: tinggi h-48 seperti sebelumnya; HP: rasio 4/3 dan ikon lebih kecil. Ukuran area ditentukan wadah
// (bukan gambar), jadi gambar yang dimuat belakangan (lazy) tidak menggeser tata letak.
export const ProductImage = ({ product }) => {
  const src = productImageUrl(product);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);
  const showImg = src && !failed;
  return (
    <div
      className={`w-full h-48 bg-gradient-to-br from-pink-400 to-red-500 rounded-lg flex items-center justify-center text-5xl mb-4 max-sm:h-auto max-sm:aspect-[4/3] max-sm:text-3xl max-sm:mb-2 max-sm:rounded-md${
        showImg ? ' overflow-hidden' : ''
      }`}
    >
      {showImg ? (
        <img
          src={src}
          alt={product.name}
          loading="lazy"
          decoding="async"
          width={400}
          height={300}
          className="w-full h-full object-cover"
          onError={() => setFailed(true)}
        />
      ) : (
        <span data-testid="product-image-placeholder" role="img" aria-label={`Gambar ${product.name} belum tersedia`}>
          🖼️
        </span>
      )}
    </div>
  );
};

// Tombol "Tambah ke keranjang": belum login -> ke halaman login lalu kembali ke toko.
const AddToCartButton = ({ product, user }) => {
  const navigate = useNavigate();
  const { setCart, onUnauthorized } = useCart();
  const [state, setState] = useState('idle'); // idle | busy | done | error
  const [msg, setMsg] = useState('');

  useEffect(() => {
    if (state !== 'done' && state !== 'error') return undefined;
    const t = setTimeout(() => setState('idle'), 2500);
    return () => clearTimeout(t);
  }, [state]);

  const add = async () => {
    if (!user) {
      setReturnTo('/');
      navigate('/login');
      return;
    }
    setState('busy');
    try {
      const c = await addCartItem(product.id, 1);
      if (c && Array.isArray(c.items)) setCart(c);
      setState('done');
    } catch (err) {
      if (isUnauthorized(err)) {
        onUnauthorized?.();
        return;
      }
      setMsg(errorMessage(err, 'Gagal menambah ke keranjang'));
      setState('error');
    }
  };

  // HP (< 640px): ikon keranjang di kiri + label pendek "Tambah" di kanan. Teks lama "Tambah ke keranjang" tetap
  // satu simpul teks (>= 640px tampil persis seperti sebelumnya) dan di HP hanya disembunyikan secara visual
  // (max-sm:sr-only); label "Tambah" di HP berasal dari ::after. Nama aksesibel dari aria-label.
  const idle = state !== 'busy' && state !== 'done';
  return (
    <div className="mt-4 max-sm:mt-2">
      <button
        type="button"
        onClick={add}
        disabled={state === 'busy'}
        aria-label={`Tambah ${product.name} ke keranjang`}
        aria-busy={state === 'busy'}
        data-testid="add-to-cart"
        className={`w-full py-2 rounded-lg font-semibold transition disabled:opacity-60 max-sm:flex max-sm:items-center max-sm:justify-center max-sm:gap-1.5 max-sm:px-2 max-sm:text-[13px] max-sm:leading-tight ${
          state === 'done' ? 'bg-green-600 text-white' : 'bg-purple-700 text-white hover:bg-purple-800'
        }${idle ? " max-sm:after:content-['Tambah']" : ''}`}
      >
        <CartGlyph className="sm:hidden h-4 w-4 shrink-0" />
        {state === 'busy' ? (
          <span className="max-sm:min-w-0 max-sm:truncate">Menambahkan...</span>
        ) : state === 'done' ? (
          <span className="max-sm:min-w-0 max-sm:truncate">Ditambahkan ✓</span>
        ) : (
          <span className="max-sm:sr-only">Tambah ke keranjang</span>
        )}
      </button>
      {state === 'error' && (
        <p role="alert" className="text-xs text-red-600 mt-1">
          {msg}
        </p>
      )}
      <span className="sr-only" aria-live="polite">
        {state === 'done' ? `${product.name} ditambahkan ke keranjang` : ''}
      </span>
    </div>
  );
};

// Lencana "Grosir" di kartu produk: membuka daftar jenjang kecil (jumlah minimal -> harga per satuan).
export const TierBadge = ({ product }) => {
  const [open, setOpen] = useState(false);
  const tiers = Array.isArray(product.tiers) ? product.tiers : [];
  if (!tiers.length) return null;
  const unit = product.unit || 'pcs';
  const id = `tiers-${product.id}`;
  const minPrice = tiers[tiers.length - 1].unitPrice;
  return (
    // HP: lencana selebar kartu, huruf 11px, label ringkas tanpa "/ satuan" (teks lengkap tetap untuk pembaca layar);
    // daftar jenjang membungkus baris agar kartu tidak melebar. >= 640px tidak berubah.
    <div className="mt-2 max-sm:mt-1.5">
      <button
        type="button"
        className="inline-flex items-center gap-1 rounded-full border border-amber-300 bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-900 hover:bg-amber-200 max-sm:flex max-sm:w-full max-sm:justify-between max-sm:rounded-md max-sm:px-2 max-sm:py-0.5 max-sm:text-left max-sm:!text-[11px] max-sm:!leading-[14px]"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
        data-testid="tier-badge"
      >
        {/* >= 640px: satu simpul teks persis seperti sebelumnya (di HP hanya tersembunyi visual). */}
        <span className="max-sm:sr-only">Grosir · mulai {perUnit(minPrice, unit)}</span>
        {/* HP: label ringkas tanpa satuan, harga tidak terpotong di tengah. */}
        <span className="sm:hidden min-w-0" aria-hidden="true" data-testid="tier-badge-short">
          Grosir · mulai <span className="whitespace-nowrap">{rupiah(minPrice)}</span>
        </span>
        <span aria-hidden="true">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <ul
          id={id}
          className="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-gray-800 max-sm:mt-1 max-sm:rounded-md max-sm:px-2 max-sm:py-1 max-sm:!text-[11px] max-sm:!leading-[15px]"
          data-testid="tier-list"
        >
          {tiers.map((t) => (
            <li key={t.minQty} className="flex justify-between gap-3 py-0.5 max-sm:flex-wrap max-sm:gap-x-1.5 max-sm:gap-y-0">
              <span>
                Beli {t.minQty}+ {unit}
              </span>
              <strong className="whitespace-nowrap max-sm:whitespace-normal">{perUnit(t.unitPrice, unit)}</strong>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

// Kartu produk. Kelas tanpa awalan = tampilan >= 640px (tidak berubah); `max-sm:` = penyesuaian HP (< 640px)
// agar muat 2 kolom: padding, gambar, dan huruf lebih kecil, nama maks. 2 baris, kategori 1 baris, tombol di dasar.
export const ProductCard = ({ product, user }) => {
  return (
    <div
      className="bg-white rounded-xl shadow-lg p-6 hover:shadow-2xl hover:-translate-y-2 transition-all duration-300 flex flex-col max-sm:h-full max-sm:min-w-0 max-sm:rounded-lg max-sm:p-2 max-sm:hover:translate-y-0 max-sm:hover:shadow-lg"
      data-testid="product-card"
    >
      <ProductImage product={product} />
      <h3
        className="text-lg font-semibold text-gray-800 mb-2 max-sm:mb-0.5 max-sm:text-[13px] max-sm:leading-[17px] max-sm:font-medium max-sm:line-clamp-2 max-sm:break-words"
        title={product.name}
      >
        {product.name}
      </h3>
      <span className="inline-block text-xs font-medium text-purple-600 uppercase mb-2 tracking-wide max-sm:mb-0 max-sm:block max-sm:truncate max-sm:!text-[11px] max-sm:!leading-[15px] max-sm:tracking-normal">
        {product.category}
      </span>
      <p className="text-2xl font-bold text-purple-700 mt-2 max-sm:mt-1 max-sm:text-[15px] max-sm:leading-5 max-sm:break-words">
        <span className="max-sm:whitespace-nowrap">Rp {product.price?.toLocaleString('id-ID')}</span>
        <span className="ml-1 text-base font-medium text-gray-500 max-sm:ml-0.5 max-sm:inline-block max-sm:max-w-full max-sm:text-[11px] max-sm:leading-4 max-sm:break-words">
          / {product.unit || 'pcs'}
        </span>
      </p>
      <TierBadge product={product} />
      <p className="text-sm text-gray-600 mt-2 flex-1 max-sm:mt-1 max-sm:!text-[11px] max-sm:!leading-[15px] max-sm:line-clamp-2 max-sm:break-words">
        {product.description}
      </p>
      <AddToCartButton product={product} user={user} />
    </div>
  );
};

const Home = ({ user }) => {
  const [products, setProducts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState('');
  const [categoryNames, setCategoryNames] = useState({});

  useEffect(() => {
    fetchProducts();
    // Nama kategori dari database (opsional; bila gagal, slug tetap dipakai).
    axios
      .get(`${API_BASE_URL}/categories`)
      .then((res) => {
        const m = {};
        (res.data || []).forEach((c) => {
          m[c.slug] = c.name;
        });
        setCategoryNames(m);
      })
      .catch(() => {});
  }, []);

  const fetchProducts = async () => {
    try {
      const response = await axios.get(`${API_BASE_URL}/products`);
      setProducts(response.data);
      setLoading(false);
    } catch (err) {
      console.error('Error fetching products:', err);
      setError('Gagal memuat produk. Silakan coba lagi nanti.');
      setLoading(false);
    }
  };

  const filteredProducts = products.filter(product => {
    const matchesSearch = product.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
                          product.description.toLowerCase().includes(searchQuery.toLowerCase());
    const matchesCategory = !selectedCategory || product.category.toUpperCase() === selectedCategory;
    return matchesSearch && matchesCategory;
  });

  const categories = [...new Set(products.map(p => p.category.toUpperCase()))];

  if (loading) {
    return (
      <div className="flex justify-center items-center h-screen">
        <div className="text-xl text-gray-600">Memuat produk...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex justify-center items-center h-screen">
        <div className="text-xl text-red-600">{error}</div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto px-4 py-8 max-sm:px-3 max-sm:py-4">
      <div className="flex flex-col md:flex-row gap-4 mb-8 max-sm:mb-0">
        <input
          type="text"
          placeholder="Cari produk..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="flex-1 px-4 py-3 border-2 border-gray-300 rounded-lg text-base focus:outline-none focus:border-purple-600 transition"
        />
        <select
          value={selectedCategory}
          onChange={(e) => setSelectedCategory(e.target.value)}
          className="px-4 py-3 border-2 border-gray-300 rounded-lg text-base cursor-pointer focus:outline-none focus:border-purple-600 transition"
        >
          <option value="">Semua Kategori</option>
          {categories.map(cat => (
            <option key={cat} value={cat}>{categoryNames[cat.toLowerCase()] || cat}</option>
          ))}
        </select>
      </div>

      {filteredProducts.length === 0 ? (
        <div className="text-center py-16 text-gray-600">
          <h3 className="text-2xl mb-4">Tidak ada produk ditemukan</h3>
          <p>Coba ubah kata kunci pencarian atau filter kategori</p>
        </div>
      ) : (
        // HP: 2 kolom rapat; >= 640px: kolom & jarak seperti sebelumnya.
        <div className="grid grid-cols-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6 mt-8 max-sm:gap-2 max-sm:mt-4" data-testid="product-grid">
          {filteredProducts.map(product => (
            <ProductCard key={product.id} product={product} user={user} />
          ))}
        </div>
      )}
    </div>
  );
};

// Halaman invoice sudah pindah ke /admin/invoice. Pakai muat ulang penuh
// (BUKAN navigasi react-router) agar Cloudflare Access mencegat dan meminta login admin.
export const LegacyInvoiceRedirect = () => {
  useEffect(() => {
    window.location.replace('/admin/invoice');
  }, []);
  return (
    <p className="max-w-3xl mx-auto px-4 py-10 text-gray-600">
      Halaman invoice sudah dipindah ke area admin. Mengalihkan...
    </p>
  );
};

const App = () => {
  // Dibaca langsung saat render pertama agar halaman yang wajib login tidak salah mengalihkan.
  const [user, setUser] = useState(() => getStoredUser());

  useEffect(() => {
    // Periksa sesi ke server (token dikirim lewat header Authorization).
    verifySession().then(({ valid, user: fresh }) => {
      if (valid === true && fresh) {
        saveSession(null, fresh);
        setUser(fresh);
      } else if (valid === false) {
        clearSession();
        setUser(null);
      }
    });
  }, []);

  return (
    <Router>
      <AppContent user={user} setUser={setUser} />
    </Router>
  );
};

// Halaman yang wajib login: belum login -> /login, lalu kembali ke halaman ini.
const RequireLogin = ({ user, children }) => {
  const navigate = useNavigate();
  const location = useLocation();
  useEffect(() => {
    if (!user) {
      setReturnTo(location.pathname + location.search);
      navigate('/login', { replace: true });
    }
  }, [user, navigate, location.pathname, location.search]);
  return user ? children : null;
};

const CartIcon = () => {
  const { count } = useCart();
  return (
    <Link to="/keranjang" className="relative inline-flex items-center hover:opacity-80 transition" aria-label={`Keranjang (${count} item)`}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="w-7 h-7" aria-hidden="true">
        <circle cx="9" cy="21" r="1" />
        <circle cx="20" cy="21" r="1" />
        <path d="M1 1h4l2.68 13.39a2 2 0 0 0 2 1.61h9.72a2 2 0 0 0 2-1.61L23 6H6" />
      </svg>
      {count > 0 && (
        <span className="absolute -top-2 -right-2 min-w-[20px] h-5 px-1 rounded-full bg-yellow-400 text-purple-900 text-xs font-bold flex items-center justify-center">
          {count > 99 ? '99+' : count}
        </span>
      )}
    </Link>
  );
};

const AppContent = ({ user, setUser }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const locRef = useRef(location);
  locRef.current = location;

  // Sesi kedaluwarsa saat memanggil API keranjang/pesanan.
  const handleUnauthorized = useCallback(() => {
    clearSession();
    setUser(null);
    setReturnTo(locRef.current.pathname + locRef.current.search);
    navigate('/login');
  }, [setUser, navigate]);

  return (
    <CartProvider user={user} onUnauthorized={handleUnauthorized}>
      <Shell user={user} setUser={setUser} />
    </CartProvider>
  );
};

const Shell = ({ user, setUser }) => {
  const navigate = useNavigate();
  const { pathname } = useLocation();

  const handleLogout = async () => {
    await logoutRequest();
    clearSession();
    setUser(null);
    navigate('/');
  };

  const handleLoginSuccess = (userData) => {
    setUser(userData);
  };

  // Area admin memakai layout sendiri (sidebar bergaya cPanel), tanpa navbar & footer toko.
  if (pathname === '/admin' || pathname.startsWith('/admin/')) {
    return (
      <Routes>
        <Route path="/admin/*" element={<AdminApp />} />
      </Routes>
    );
  }

  return (
    <div className="min-h-screen flex flex-col">
      <nav className="bg-gradient-to-r from-purple-600 to-purple-800 text-white shadow-lg">
        <div className="max-w-7xl mx-auto px-4 py-4 flex flex-wrap gap-3 justify-between items-center">
          <div 
            onClick={() => navigate('/')} 
            className="text-2xl font-bold cursor-pointer hover:opacity-80 transition"
          >
            MihanStore
          </div>
          <div className="flex flex-wrap gap-x-6 gap-y-2 items-center">
            <Link to="/" className="hover:opacity-80 transition font-medium">
              Home
            </Link>
            {user?.role === 'admin' && (
              // Anchor biasa (muat ulang penuh), BUKAN navigasi react-router,
              // agar Cloudflare Access bisa mencegat /admin.
              <a href="/admin" className="hover:opacity-80 transition font-medium">
                Admin
              </a>
            )}
            {user && (
              <Link to="/pesanan" className="hover:opacity-80 transition font-medium">
                Pesanan Saya
              </Link>
            )}
            {user && <CartIcon />}
            {user ? (
              <>
                <span className="font-medium">
                  Halo, {user.name}
                  {user.username && <span className="ml-1 text-sm opacity-80">(@{user.username})</span>}
                </span>
                <button
                  onClick={handleLogout}
                  className="bg-white text-purple-600 px-4 py-2 rounded-lg font-medium hover:bg-gray-100 transition"
                >
                  Logout
                </button>
              </>
            ) : (
              <>
                <Link to="/login" className="hover:opacity-80 transition font-medium">
                  Login
                </Link>
                <Link 
                  to="/register" 
                  className="bg-white text-purple-600 px-4 py-2 rounded-lg font-medium hover:bg-gray-100 transition"
                >
                  Register
                </Link>
              </>
            )}
          </div>
        </div>
      </nav>

      <main className="flex-1">
        <Routes>
          <Route path="/" element={<Home user={user} />} />
          <Route path="/keranjang" element={<RequireLogin user={user}><Cart /></RequireLogin>} />
          <Route path="/checkout" element={<RequireLogin user={user}><Checkout user={user} /></RequireLogin>} />
          <Route path="/pesanan" element={<RequireLogin user={user}><MyOrders /></RequireLogin>} />
          <Route path="/pesanan/:orderNo" element={<RequireLogin user={user}><OrderDetail user={user} /></RequireLogin>} />
          <Route path="/login" element={<Login onLoginSuccess={handleLoginSuccess} />} />
          <Route path="/register" element={<Register onRegisterSuccess={handleLoginSuccess} />} />
          {/* Rute lama: dialihkan penuh ke area admin (dijaga Cloudflare Access). */}
          <Route path="/invoice/create" element={<LegacyInvoiceRedirect />} />
          <Route path="/lengkapi-profil" element={<CompleteProfile onLoginSuccess={handleLoginSuccess} />} />
          <Route path="/privasi" element={<PrivacyPolicy />} />
          <Route path="/syarat" element={<TermsOfService />} />
        </Routes>
      </main>

      <footer className="border-t bg-white">
        <div className="max-w-7xl mx-auto px-4 py-6 flex flex-col sm:flex-row gap-3 justify-between items-center text-sm text-gray-600">
          <span>&copy; {new Date().getFullYear()} Mihan Store</span>
          <nav className="flex gap-4">
            <Link to="/privasi" className="hover:text-purple-700">Kebijakan Privasi</Link>
            <Link to="/syarat" className="hover:text-purple-700">Syarat &amp; Ketentuan</Link>
          </nav>
        </div>
      </footer>
    </div>
  );
};

export default App;
