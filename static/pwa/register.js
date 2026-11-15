/* No automatic reload, install prompt, notification permission or write retry. */
(() => {
  if (!('serviceWorker' in navigator) || !window.isSecureContext) return;
  // A partial page replacement must not register duplicate lifecycle listeners.
  if (window.yuhadaPWARegistered) return;
  window.yuhadaPWARegistered = true;
  const register = () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' })
      .catch(() => { /* App operations remain usable if installation is unavailable. */ });
  };
  if (document.readyState === 'complete') register();
  else window.addEventListener('load', register, { once: true });
})();
