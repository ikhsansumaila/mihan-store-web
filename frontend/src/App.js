import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Routes, Route, Link, useNavigate } from 'react-router-dom';
import axios from 'axios';
import InvoiceCreate from './components/InvoiceCreate';
import Login from './components/Login';
import Register from './components/Register';

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

  useEffect(() => {
    fetchProducts();
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
            <option key={cat} value={cat}>{cat}</option>
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

const App = () => {
  const [user, setUser] = useState(null);

  useEffect(() => {
    const storedUser = localStorage.getItem('user');
    if (storedUser) {
      setUser(JSON.parse(storedUser));
    }
  }, []);

  return (
    <Router>
      <AppContent user={user} setUser={setUser} />
    </Router>
  );
};

const AppContent = ({ user, setUser }) => {
  const navigate = useNavigate();

  const handleLogout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    setUser(null);
    navigate('/');
  };

  const handleLoginSuccess = (userData) => {
    setUser(userData);
  };

  return (
    <div className="min-h-screen flex flex-col">
      <nav className="bg-gradient-to-r from-purple-600 to-purple-800 text-white shadow-lg">
        <div className="max-w-7xl mx-auto px-4 py-4 flex justify-between items-center">
          <div 
            onClick={() => navigate('/')} 
            className="text-2xl font-bold cursor-pointer hover:opacity-80 transition"
          >
            MihanStore
          </div>
          <div className="flex gap-6 items-center">
            <Link to="/" className="hover:opacity-80 transition font-medium">
              Home
            </Link>
            <Link to="/invoice/create" className="hover:opacity-80 transition font-medium text-yellow-300 font-bold">
              Buat Invoice
            </Link>
            {user ? (
              <>
                <span className="font-medium">Halo, {user.name}</span>
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
          <Route path="/invoice/create" element={<InvoiceCreate />} />
        </Routes>
      </main>
    </div>
  );
};

export default App;
