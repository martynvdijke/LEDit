const CACHE_NAME = 'ledit-v3';
const ASSETS = [
  '/',
  '/login',
  '/remote',
  '/static/pwa/manifest.json',
  '/static/pwa/remote-manifest.json',
  '/static/assets/remote.js',
  '/static/pwa/icon.svg',
  '/static/brand/ledit-mark.svg',
  '/static/brand/ledit-lockup-dark.svg',
  '/static/brand/ledit-lockup-light.svg',
  '/static/brand/brand.css'
];

// Install: cache app shell
self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => {
      return cache.addAll(ASSETS);
    })
  );
});

// Activate: clean old caches
self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys => {
      return Promise.all(
        keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k))
      );
    })
  );
});

// Fetch: network-first, fallback to cache. Embed pages, snapshot responses and
// WebSocket upgrades must never be stored: they are per-session, short-lived
// and (for the wall) live streams.
function isUncacheable(url) {
  return url.pathname.startsWith('/embed') ||
    url.pathname.startsWith('/api/frame') ||
    url.pathname.startsWith('/ws/');
}

self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (url.origin === self.location.origin && isUncacheable(url)) {
    return; // let the browser talk to the network directly
  }
  event.respondWith(
    fetch(event.request)
      .then(response => {
        return caches.open(CACHE_NAME).then(cache => {
          cache.put(event.request, response.clone());
          return response;
        });
      })
      .catch(() => {
        return caches.match(event.request);
      })
  );
});
