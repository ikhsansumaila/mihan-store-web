import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { getCart, isUnauthorized } from './api';

// Status keranjang bersama (lencana navbar, halaman keranjang, tombol tambah).
const CartContext = createContext({ cart: null, count: 0, refresh: () => {}, setCart: () => {}, onUnauthorized: () => {} });

export const CartProvider = ({ user, onUnauthorized, children }) => {
  const [cart, setCart] = useState(null);

  const refresh = useCallback(async () => {
    if (!user) {
      setCart(null);
      return null;
    }
    try {
      const c = await getCart();
      const safe = c && Array.isArray(c.items) ? c : null;
      setCart(safe);
      return safe;
    } catch (err) {
      if (isUnauthorized(err)) onUnauthorized?.();
      return null;
    }
  }, [user, onUnauthorized]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const value = useMemo(
    () => ({ cart, count: cart?.itemCount || 0, refresh, setCart, onUnauthorized }),
    [cart, refresh, onUnauthorized]
  );
  return <CartContext.Provider value={value}>{children}</CartContext.Provider>;
};

export const useCart = () => useContext(CartContext);
