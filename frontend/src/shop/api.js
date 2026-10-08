// API pelanggan (keranjang & pesanan). Token sesi dikirim lewat header Authorization;
// jangan pernah dicetak ke console.
import axios from 'axios';
import { API_BASE_URL, authHeader } from '../auth';

const cfg = () => ({ headers: authHeader() });
const data = (p) => p.then((r) => r.data);

export const getCart = () => data(axios.get(`${API_BASE_URL}/cart`, cfg()));
export const addCartItem = (productId, qty = 1) =>
  data(axios.post(`${API_BASE_URL}/cart/items`, { productId, qty }, cfg()));
export const setCartQty = (productId, qty) =>
  data(axios.put(`${API_BASE_URL}/cart/items`, { productId, qty }, cfg()));
export const removeCartItem = (productId) => data(axios.delete(`${API_BASE_URL}/cart/items/${productId}`, cfg()));
// "Mengerti": harga yang terlihat disetel ke harga terkini (penanda perubahan harga hilang).
export const ackCartPrices = () => data(axios.post(`${API_BASE_URL}/cart/ack-prices`, {}, cfg()));

export const createOrder = (body) => axios.post(`${API_BASE_URL}/orders`, body, cfg());
export const listOrders = (page = 1) => data(axios.get(`${API_BASE_URL}/orders?page=${page}&per_page=10`, cfg()));
export const getOrder = (orderNo) => data(axios.get(`${API_BASE_URL}/orders/${encodeURIComponent(orderNo)}`, cfg()));
export const cancelOrder = (orderNo, reason) =>
  data(axios.post(`${API_BASE_URL}/orders/${encodeURIComponent(orderNo)}/cancel`, { reason: reason || '' }, cfg()));
export const getStoreInfo = () => data(axios.get(`${API_BASE_URL}/store-info`, cfg()));

// Bukti transfer (pemilik pesanan). Berkas privat: diambil sebagai blob dengan header Authorization.
const proofUrl = (orderNo) => `${API_BASE_URL}/orders/${encodeURIComponent(orderNo)}/payment-proof`;
export const uploadPaymentProof = (orderNo, blob, filename = 'bukti.jpg') => {
  const fd = new FormData();
  fd.append('file', blob, filename);
  return data(axios.post(proofUrl(orderNo), fd, { headers: authHeader() }));
};
export const getPaymentProofBlob = (orderNo) => data(axios.get(proofUrl(orderNo), { headers: authHeader(), responseType: 'blob' }));
export const deletePaymentProof = (orderNo) => axios.delete(proofUrl(orderNo), { headers: authHeader() });

export const isUnauthorized = (err) => err?.response?.status === 401;
