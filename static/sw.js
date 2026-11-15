/* Only public, explicitly listed assets enter Cache Storage. Bump for asset changes. */
const CACHE_NAME = 'yuhada-public-v1';
const CACHE_PREFIX = 'yuhada-public-';
const OFFLINE_URL = '/static/pwa/offline.html';
const ICON_URLS = [
  '/static/icons/icon.svg', '/static/icons/icon-192.png', '/static/icons/icon-512.png',
  '/static/icons/icon-maskable-512.png', '/static/icons/apple-touch-icon.png',
  '/static/icons/favicon.svg', '/static/icons/favicon-32.png', '/static/icons/favicon-16.png',
  '/static/icons/favicon.ico'
];
const PUBLIC_ASSETS = new Set([
  ...ICON_URLS, OFFLINE_URL, '/static/css/app.css',
  '/static/fonts/PretendardVariable.woff2',
  '/static/fonts/cormorant-garamond-latin.woff2',
  '/static/fonts/cormorant-garamond-latin-italic.woff2'
]);

self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const assets = await Promise.all([OFFLINE_URL, ...ICON_URLS].map(async url => {
      const request = new Request(url, { credentials: 'omit', cache: 'reload', redirect: 'error' });
      const response = await fetch(request);
      if (!publicResponse(response, request)) throw new Error('Invalid public offline asset');
      return { request, response };
    }));
    // Validate the complete precache set before storing any response.
    const cache = await caches.open(CACHE_NAME);
    await Promise.all(assets.map(({ request, response }) => cache.put(request, response)));
  })());
  // Let an update wait until existing tabs close. Never reload an active form.
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(
    keys.filter(key => key.startsWith(CACHE_PREFIX) && key !== CACHE_NAME).map(key => caches.delete(key))
  )).then(() => self.clients.claim()));
});

function publicResponse(response, request) {
  if (response.status !== 200 || response.redirected || response.type !== 'basic' || response.url !== request.url) return false;
  if (/\b(?:no-store|private)\b/i.test(response.headers.get('Cache-Control') || '')) return false;
  const path = new URL(request.url).pathname;
  const type = (response.headers.get('Content-Type') || '').split(';')[0].trim().toLowerCase();
  if (path.endsWith('.css')) return type === 'text/css';
  if (path.endsWith('.woff2')) return type === 'font/woff2' || type === 'application/font-woff';
  if (path.endsWith('.png')) return type === 'image/png';
  if (path.endsWith('.svg')) return type === 'image/svg+xml';
  if (path.endsWith('.ico')) return type === 'image/x-icon' || type === 'image/vnd.microsoft.icon';
  return path === OFFLINE_URL && type === 'text/html';
}

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  // Never handle writes, cross-origin OAuth/CDNs, auth callbacks, or API requests.
  if (request.method !== 'GET' || url.origin !== self.location.origin ||
      url.pathname.startsWith('/v2/kakao/') || url.pathname.startsWith('/api/') ||
      url.pathname.startsWith('/v2/api/')) return;

  if (request.mode === 'navigate') {
    // Neither private HTML nor redirects/JSON are saved. Fetch always reaches the server.
    event.respondWith(fetch(request, { cache: 'no-store' }).catch(async () => {
      return await caches.match(OFFLINE_URL) || new Response('인터넷 연결 후 다시 열어 주세요.', {
        status: 503, headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' }
      });
    }));
    return;
  }

  if (url.search !== '' || !PUBLIC_ASSETS.has(url.pathname)) return;
  event.respondWith((async () => {
    try {
      const response = await fetch(request, { cache: 'no-cache' });
      if (publicResponse(response, request)) {
        try {
          const cache = await caches.open(CACHE_NAME);
          await cache.put(request, response.clone());
        } catch (_) { /* Storage quota must not discard a successful network response. */ }
      }
      return response;
    } catch (error) {
      const saved = await caches.match(request);
      if (saved) return saved;
      throw error;
    }
  })());
});
