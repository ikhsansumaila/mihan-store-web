/* Service worker Mihan Store — HANYA untuk notifikasi push admin.
 *
 * Sengaja TIDAK ada handler "fetch" dan TIDAK meng-cache aset/halaman apa pun, agar deploy baru
 * langsung terpakai (tidak tertahan cache service worker).
 *
 * Payload push (JSON dari backend, paket push): {title, body, url, tag}. Isi minimal (judul + nomor
 * pesanan). url hanya diterima bila path relatif same-origin yang diawali /admin.
 */
'use strict';

var DEFAULT_URL = '/admin';

self.addEventListener('install', function () {
  self.skipWaiting();
});

self.addEventListener('activate', function (event) {
  event.waitUntil(self.clients.claim());
});

function str(v, max) {
  return typeof v === 'string' ? v.slice(0, max) : '';
}

// safeUrl: hanya "/admin" atau "/admin/..." (path relatif, tanpa skema/host, tanpa "//" atau "\").
function safeUrl(u) {
  if (typeof u !== 'string' || u.length > 300) return DEFAULT_URL;
  if (!/^\/admin(?:[/?#]|$)/.test(u) || /[\\\s]/.test(u) || u.indexOf('//') !== -1) return DEFAULT_URL;
  try {
    var url = new URL(u, self.location.origin);
    if (url.origin !== self.location.origin || !/^\/admin(?:\/|$)/.test(url.pathname)) return DEFAULT_URL;
    return url.pathname + url.search;
  } catch (e) {
    return DEFAULT_URL;
  }
}

function parsePayload(data) {
  if (!data) return {};
  try {
    var p = data.json();
    return p && typeof p === 'object' ? p : {};
  } catch (e) {
    return {};
  }
}

self.addEventListener('push', function (event) {
  var p = parsePayload(event.data);
  var title = str(p.title, 80) || 'Mihan Store';
  var tag = str(p.tag, 64);
  var options = {
    body: str(p.body, 200),
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    lang: 'id',
    data: { url: safeUrl(p.url) },
  };
  if (tag) {
    // Satu notifikasi per nomor pesanan: "Pesanan dibatalkan" menggantikan "Pesanan baru".
    options.tag = tag;
    options.renotify = true;
  }
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
  event.notification.close();
  var path = safeUrl(event.notification.data && event.notification.data.url);
  var target = new URL(path, self.location.origin).href;
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
      for (var i = 0; i < list.length; i++) {
        var c = list[i];
        var sameOrigin = false;
        try {
          sameOrigin = new URL(c.url).origin === self.location.origin;
        } catch (e) {
          sameOrigin = false;
        }
        if (!sameOrigin) continue;
        if (c.url === target) return c.focus();
        if (typeof c.navigate === 'function') {
          return c.navigate(target).then(function (nc) {
            return (nc || c).focus();
          }).catch(function () {
            return self.clients.openWindow(target);
          });
        }
      }
      return self.clients.openWindow(target);
    })
  );
});

// Diekspor hanya untuk tes jest (tidak berpengaruh di browser).
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { safeUrl: safeUrl, parsePayload: parsePayload };
}
