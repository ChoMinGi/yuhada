(function () {
  'use strict';
  if (window.__yhFeedbackInstalled) return;
  window.__yhFeedbackInstalled = true;
  let hideTimer;
  window.yhNotify = function (message) {
    const target = document.getElementById('toast-message');
    if (!target) return;
    if (hideTimer) clearTimeout(hideTimer);
    target.textContent = String(message);
    target.hidden = false;
    hideTimer = setTimeout(function () { target.hidden = true; }, 12000);
  };
  function reportFailure(event) {
    const detail = event.detail || {};
    const config = detail.requestConfig || {};
    const verb = String(config.verb || 'get').toLowerCase();
    const status = detail.xhr ? detail.xhr.status : 0;
    const writing = verb !== 'get' && verb !== 'head';
    if (event.type === 'htmx:responseError' && status >= 400 && status < 500) {
      window.yhNotify(status === 401
        ? '로그인이 필요합니다. 다시 로그인해 주세요.'
        : writing ? '저장하지 못했습니다. 입력 내용과 현재 상태를 확인해 주세요.'
          : '화면을 불러오지 못했습니다. 새로고침 후 다시 확인해 주세요.');
      return;
    }
    window.yhNotify(writing
      ? '처리 결과를 확인하지 못했습니다. 새로고침 후 저장 여부를 확인해 주세요.'
      : '화면을 불러오지 못했습니다. 연결 상태를 확인한 뒤 다시 시도해 주세요.');
  }
  ['htmx:responseError', 'htmx:sendError', 'htmx:timeout'].forEach(function (name) {
    document.addEventListener(name, reportFailure);
  });

  window.yhCopyText = async function (value) {
    try {
      if (typeof navigator === 'undefined' || !navigator.clipboard || typeof navigator.clipboard.writeText !== 'function') {
        throw new Error('clipboard unavailable');
      }
      await navigator.clipboard.writeText(String(value));
      return true;
    } catch (_) {
      window.yhNotify('복사하지 못했습니다. 권한을 확인하거나 직접 선택해 복사해 주세요.');
      return false;
    }
  };

  // Sorting is a write too: a login page or failed response is never success.
  window.yhSavePresetOrder = async function (endpoint, ids) {
    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
        body: JSON.stringify({ order: ids })
      });
      if (response.status === 401) {
        window.yhNotify('로그인이 필요합니다. 순서를 확인한 뒤 다시 로그인해 주세요.');
        return false;
      }
      if (response.status !== 204) throw new Error('order not confirmed');
      return true;
    } catch (_) {
      window.yhNotify('순서 저장 결과를 확인하지 못했습니다. 새로고침 후 실제 순서를 확인해 주세요.');
      return false;
    }
  };
})();
