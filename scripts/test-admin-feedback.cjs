'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

const source = fs.readFileSync(path.join(__dirname, '../static/admin/feedback.js'), 'utf8');
const listeners = new Map();
const toast = { textContent: '', hidden: true };
let expires;
let calls = 0;
let response = { status: 204 };
let lastRequest;
const context = vm.createContext({
  window: {},
  navigator: { clipboard: { async writeText() {} } },
  document: {
    getElementById(id) { return id === 'toast-message' ? toast : null; },
    addEventListener(name, handler) { assert(!listeners.has(name), 'duplicate listener'); listeners.set(name, handler); }
  },
  setTimeout(fn) { expires = fn; return 1; },
  clearTimeout() {},
  async fetch(endpoint, request) {
    calls++;
    lastRequest = { endpoint, request };
    if (response instanceof Error) throw response;
    return response;
  }
});
vm.runInContext(source, context);
vm.runInContext(source, context);
assert.equal(listeners.size, 3, 'install must be idempotent');
function event(type, verb, status) {
  listeners.get(type)({ type, detail: { requestConfig: { verb }, xhr: { status, responseText: '<h1>SECRET_DATABASE_ERROR</h1>' } } });
  assert(!toast.hidden);
  assert(!toast.textContent.includes('SECRET_DATABASE_ERROR'), 'never expose server HTML/errors');
}
event('htmx:responseError', 'patch', 422);
assert(toast.textContent.includes('입력 내용과 현재 상태'));
event('htmx:responseError', 'post', 500);
assert(toast.textContent.includes('처리 결과를 확인하지 못했습니다'));
event('htmx:sendError', 'delete', 0);
assert(toast.textContent.includes('저장 여부를 확인'));
event('htmx:timeout', 'POST', 0);
assert(toast.textContent.includes('저장 여부를 확인'));
event('htmx:responseError', 'get', 500);
assert(toast.textContent.includes('화면을 불러오지 못했습니다'));
assert(!toast.textContent.includes('저장 여부'));
expires();
assert(toast.hidden, 'notification should expire');

(async function () {
  const copy = context.window.yhCopyText;
  let finishCopy;
  let copiedValue;
  context.navigator.clipboard.writeText = value => new Promise(resolve => { copiedValue = value; finishCopy = resolve; });
  let copyFinished = false;
  const pendingCopy = copy('CARD-123');
  pendingCopy.then(() => { copyFinished = true; });
  await Promise.resolve();
  assert.equal(copyFinished, false, 'must not report copied before clipboard promise resolves');
  finishCopy();
  assert.equal(await pendingCopy, true);
  assert.equal(copiedValue, 'CARD-123');
  context.navigator.clipboard.writeText = async () => { throw new Error('denied'); };
  assert.equal(await copy('CARD-456'), false, 'permission rejection is not copy success');
  assert(toast.textContent.includes('복사하지 못했습니다'));
  context.navigator.clipboard = undefined;
  assert.equal(await copy('CARD-789'), false, 'missing clipboard API must fail visibly');
  const save = context.window.yhSavePresetOrder;
  assert.equal(await save('/v2/admin/settings/deduct-amounts/reorder', ['a', 'b']), true);
  assert.equal(lastRequest.request.credentials, 'same-origin');
  assert.equal(lastRequest.request.headers.Accept, 'application/json');
  assert.equal(lastRequest.request.body, '{"order":["a","b"]}');
  response = { status: 200 };
  assert.equal(await save('/v2/admin/settings/deduct-amounts/reorder', ['b', 'a']), false, 'HTML200/login page is not saved');
  assert(toast.textContent.includes('순서 저장 결과'));
  response = { status: 401 };
  assert.equal(await save('/v2/admin/settings/service-memos/reorder', ['a']), false);
  assert(toast.textContent.includes('다시 로그인'));
  response = new Error('network failed');
  assert.equal(await save('/v2/admin/settings/service-memos/reorder', ['a']), false);
  assert.equal(calls, 4, 'writes must never be retried automatically');
  console.log('Admin feedback and preset-save checks passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
