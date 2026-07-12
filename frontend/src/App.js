import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Routes, Route, Link, useNavigate } from 'react-router-dom';
import axios from 'axios';

const API_BASE_URL = process.env.REACT_APP_API_URL || 'http://localhost:8080/api';

const ProductCard = ({ product }) => {
  return (
    <div className="product-card">
      <div className="product-image">🖼️</div>
      <div className="product-name">{product.Name}</div>
      <div className="product-category">{product.Category}</div>
      <div className="product-price">Rp {product.Price.toLocaleString('id-ID')}</div>
      <div className="product-description">{product.Description}</div>
    </div>
  );
};

const Navbar = ({ user, onLogout }) => {
  const navigate = useNavigate();

  const handleBrandClick = () => {
    navigate('/');
  };

  return (
    <nav className="navbar">
      <div className="navbar-brand" onClick={handleBrandClick}>MihanStore</div>
      <div className="navbar-nav">
        <Link to="/" className="nav-link">Home</Link>
        {user ? (
          <>
            <span className="nav-link">Hello, {user.Name}</span>
            <button onClick={onLogout} className="btn btn-outline">Logout</button>
          </>
        ) : (
          <>
            <Link to="/login" className="btn btn-outline">Login</Link>
            <Link to="/register" className="btn btn-primary">Register</Link>
          </>
        )}
      </div>
    </nav>
  );
};

const ProductsList = ({ user }) => {
  const [products, setProducts] = useState([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const categories = ['', 'kerupuk', 'tepung', 'saos', 'sambal', 'sendok_plastik', 'box_hampers'];

  useEffect(() => {
    fetchProducts();
  }, [searchQuery, selectedCategory]);

  const fetchProducts = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await axios.get(`${API_BASE_URL}/products/search`, {
        params: {
          q: searchQuery,
          category: selectedCategory,
        },
      });
      setProducts(response.data || []);
    } catch (err) {
      setError('Gagal memuat produk. Silakan coba lagi nanti.');
      console.error('Error fetching products:', err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="container">Memuat produk...</div>;
  if (error) return <div className="container alert alert-error">{error}</div>;

  return (
    <div className="container">
      <div className="search-bar">
        <input
          type="text"
          placeholder="Cari produk..."
          className="search-input"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
        <select
          className="filter-select"
          value={selectedCategory}
          onChange={(e) => setSelectedCategory(e.target.value)}
        >
          {categories.map((cat) => (
            <option key={cat} value={cat}>
              {cat === '' ? 'Semua Kategori' : cat.replace('_', ' ').toUpperCase()}
            </option>
          ))}
        </select>
      </div>
      {products.length === 0 ? (
        <div className="empty-state">
          <h3>Tidak ada produk ditemukan.</h3>
          <p>Coba sesuaikan pencarian atau filter Anda.</p>
        </div>
      ) : (
        <div className="product-grid">
          {products.map((product) => (
            <ProductCard key={product.ID} product={product} />
          ))}
        </div>
      )}
    </div>
  );
};

export default App;
