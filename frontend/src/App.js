import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Routes, Route, Link, useNavigate } from 'react-router-dom';
import axios from 'axios';
import Login from './components/Login';
import Register from './components/Register';
import CompleteProfile from './components/CompleteProfile';
import { PrivacyPolicy, TermsOfService } from './components/Legal';
import AdminApp from './admin/AdminApp';
import { getStoredUser, verifySession, logoutRequest, clearSession, saveSession } from './auth';

const API_BASE_URL = '/api';

const ProductCard = ({ product }) => {
  return (
    <div className="bg-white rounded-xl shadow-lg p-6 hover:shadow-2xl hover:-translate-y-2 transition-all duration-300 cursor-pointer">
      <div className="w-full h-48 bg-gradient-to-br from-pink-400 to-red-500 rounded-lg flex items-center justify-center text-5xl mb-4">
        🖼️
      </div>
      <h3 className="text-lg font-semibold text-gray-800 mb-2">{product.name}</h3>
      <span className="inline-block text-xs font-medium text-purple-600 uppercase mb-2 tracking-wide">
        {product.category}
      </span>
      <p className="text-2xl font-bold text-purple-700 mt-2">
        Rp {product.price?.toLocaleString('id-ID')}
      </p>
      <p className="text-sm text-gray-600 mt-2">{product.description}</p>
    </div>
  );
};

const Home = () => {
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
    <div className="max-w-7xl mx-auto px-4 py-8">
      <div className="flex flex-col md:flex-row gap-4 mb-8">
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
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6 mt-8">
          {filteredProducts.map(product => (
            <ProductCard key={product.id} product={product} />
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
  const [user, setUser] = useState(null);

  useEffect(() => {
    const storedUser = getStoredUser();
    if (storedUser) {
      setUser(storedUser);
    }
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

const AppContent = ({ user, setUser }) => {
  const navigate = useNavigate();

  const handleLogout = async () => {
    await logoutRequest();
    clearSession();
    setUser(null);
    navigate('/');
  };

  const handleLoginSuccess = (userData) => {
    setUser(userData);
  };

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
          <Route path="/" element={<Home />} />
          <Route path="/login" element={<Login onLoginSuccess={handleLoginSuccess} />} />
          <Route path="/register" element={<Register onRegisterSuccess={handleLoginSuccess} />} />
          {/* Rute lama: dialihkan penuh ke area admin (dijaga Cloudflare Access). */}
          <Route path="/invoice/create" element={<LegacyInvoiceRedirect />} />
          <Route path="/lengkapi-profil" element={<CompleteProfile onLoginSuccess={handleLoginSuccess} />} />
          <Route path="/privasi" element={<PrivacyPolicy />} />
          <Route path="/syarat" element={<TermsOfService />} />
          <Route path="/admin/*" element={<AdminApp />} />
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
