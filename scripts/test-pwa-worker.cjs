// Deterministic VM test of the actual worker; no browser, network or DB.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const origin = 'https://example.test';
const handlers = new Map();
const stores = new Map();
const puts = [];
const fetched = [];
let network = async request => publicAsset(request);
const key = request => new URL(typeof request === 'string' ? request : request.url, origin).href;
function response(url, body, type, extra = {}) {
  const result = new Response(body, { headers: { 'Content-Type': type, ...extra.headers } });
  Object.defineProperties(result, {
    url: { value: url }, type: { value: extra.type || 'basic' }, redirected: { value: !!extra.redirected }
  });
  return result;
}
function publicAsset(request) {
  const path = new URL(request.url).pathname;
  const mime = path.endsWith('.html') ? 'text/html' : path.endsWith('.svg') ? 'image/svg+xml' :
    path.endsWith('.ico') ? 'image/x-icon' : 'image/png';
  return response(request.url, path.endsWith('.html') ? 'public offline guide or icon' : 'public icon', mime);
}
const caches = {
  async open(name) {
    if (!stores.has(name)) stores.set(name, new Map());
    const items = stores.get(name);
    return {
      async addAll() { throw new Error('Precache must validate every response before saving'); },
      async put(request, value) { puts.push(key(request)); items.set(key(request), value); }
    };
  },
  async match(request) {
    for (const items of stores.values()) if (items.has(key(request))) return items.get(key(request)).clone();
  },
  async keys() { return [...stores.keys()]; },
  async delete(name) { return stores.delete(name); }
};
// Resolve root-relative URLs just as a worker's Request constructor does.
class LocalRequest extends Request {
  constructor(url, options) { super(new URL(url, origin), options); }
}
vm.runInNewContext(fs.readFileSync('static/sw.js', 'utf8'), {
  self: {
    location: { origin },
    clients: { claim: async () => {} },
    addEventListener: (type, handler) => handlers.set(type, handler),
    skipWaiting: () => { throw new Error('Never force-update active forms'); }
  },
  URL, Request: LocalRequest, Response, caches,
  fetch: async (request, options) => { fetched.push({ request, options }); return network(request, options); }
});

async function lifecycle(name) {
  let pending;
  handlers.get(name)({ waitUntil: promise => { pending = promise; } });
  await pending;
}
async function dispatch(path, { method = 'GET', mode = 'cors', absolute = false } = {}) {
  let handled = false, pending;
  const request = { url: absolute ? path : origin + path, method, mode };
  handlers.get('fetch')({ request, respondWith: promise => { handled = true; pending = promise; } });
  return { handled, response: pending ? await pending : undefined };
}

(async () => {
  await lifecycle('install');
  assert.equal(stores.size, 1);
  const seeded = [...stores.values()][0];
  assert.ok([...seeded.keys()].every(url => url.includes('/static/icons/') || url.endsWith('/static/pwa/offline.html')));
  for (const { request } of fetched) {
    assert.equal(request.credentials, 'omit');
    assert.equal(request.redirect, 'error');
  }
  puts.length = 0;

  // Installation must also reject private/redirected/mis-typed public assets.
  for (const invalid of [
    request => response(request.url, 'login HTML', 'text/html'),
    request => response(request.url, 'redirect', 'image/png', { redirected: true }),
    request => response(request.url, 'private', 'image/png', { headers: { 'Cache-Control': 'private' } }),
    request => response(request.url, 'private', 'image/png', { headers: { 'Cache-Control': 'no-store' } }),
    request => response(origin + '/login', 'redirect target', 'image/png'),
    request => response(request.url, 'opaque', 'image/png', { type: 'opaque' })
  ]) {
    network = async request => request.url.endsWith('/icon-192.png') ? invalid(request) : publicAsset(request);
    await assert.rejects(lifecycle('install'), /Invalid public offline asset/);
    assert.deepEqual(puts, []);
  }

  // Private navigation always goes to the network without browser HTTP cache.
  const privateBody = 'PRIVATE MEMBER/BALANCE SENTINEL';
  network = async (request, options) => {
    assert.equal(options.cache, 'no-store');
    return response(request.url, privateBody, 'text/html');
  };
  for (const path of ['/admin', '/v2/admin/members/private-id', '/m/private-card']) {
    const result = await dispatch(path, { mode: 'navigate' });
    assert.equal(result.handled, true);
    assert.equal(await result.response.text(), privateBody);
  }
  assert.deepEqual(puts, []);

  // POST, data fetches, callbacks and external OAuth/CDNs are untouched.
  const fetchCount = fetched.length;
  for (const [path, options] of [
    ['/v2/admin/members/private-id/charge', { method: 'POST' }],
    ['/v2/admin/members/private-id', {}], ['/v2/admin/sales', {}],
    ['/api/private', { mode: 'navigate' }], ['/v2/api/private', {}],
    ['/v2/kakao/login', { mode: 'navigate' }], ['/v2/kakao/callback?code=secret', { mode: 'navigate' }],
    ['https://kauth.kakao.com/oauth/authorize', { mode: 'navigate', absolute: true }],
    ['https://cdn.example.test/library.js', { absolute: true }]
  ]) assert.equal((await dispatch(path, options)).handled, false, path);
  assert.equal(fetched.length, fetchCount);

  // Offline navigation exposes only the generic public guide, never prior HTML.
  network = async () => { throw new TypeError('offline'); };
  const offline = await dispatch('/v2/admin/members/private-id', { mode: 'navigate' });
  assert.equal(await offline.response.text(), 'public offline guide or icon');
  assert.deepEqual(puts, []);

  // Explicit public assets are network-first and can fall back to their own cache.
  network = async request => response(request.url, 'public stylesheet', 'text/css');
  const css = await dispatch('/static/css/app.css');
  assert.equal(await css.response.text(), 'public stylesheet');
  assert.deepEqual(puts, [origin + '/static/css/app.css']);
  assert.equal(fetched.at(-1).options.cache, 'no-cache');
  network = async () => { throw new TypeError('offline'); };
  assert.equal(await (await dispatch('/static/css/app.css')).response.text(), 'public stylesheet');
  assert.equal((await dispatch('/static/css/app.css?private=value')).handled, false);
  assert.equal((await dispatch('/static/private.json')).handled, false);

  // Redirected/private/HTML substitutes for an allowlisted asset are never saved.
  const count = puts.length;
  for (const invalid of [
    request => response(request.url, privateBody, 'text/html'),
    request => response(request.url, privateBody, 'text/css', { redirected: true }),
    request => response(request.url, privateBody, 'text/css', { headers: { 'Cache-Control': 'private, no-store' } })
  ]) {
    network = async request => invalid(request);
    await dispatch('/static/css/app.css');
  }
  assert.equal(puts.length, count);

  // A cache-open/quota failure must not replace a successful online response
  // with the older cached stylesheet or turn it into an application failure.
  const originalOpen = caches.open;
  for (const operation of ['open', 'put']) {
    let failed = false;
    caches.open = async name => {
      if (operation === 'open') {
        failed = true;
        throw new Error('Cache storage unavailable');
      }
      const cache = await originalOpen(name);
      return {
        ...cache,
        async put() { failed = true; throw new Error('Quota exceeded'); }
      };
    };
    const freshBody = 'fresh online stylesheet despite cache ' + operation + ' failure';
    network = async request => response(request.url, freshBody, 'text/css');
    try {
      const online = await dispatch('/static/css/app.css');
      assert.equal(failed, true, operation + ' failure must be exercised');
      assert.equal(online.response.status, 200);
      assert.equal(await online.response.text(), freshBody);
      assert.equal(puts.length, count);
    } finally {
      caches.open = originalOpen;
    }
  }

  stores.set('other-application-cache', new Map());
  stores.set('yuhada-public-obsolete', new Map());
  await lifecycle('activate');
  assert.equal(stores.has('other-application-cache'), true);
  assert.equal(stores.has('yuhada-public-obsolete'), false);
  console.log('PWA worker privacy/offline/static allowlist/lifecycle: PASS');
})().catch(error => { console.error(error); process.exitCode = 1; });
