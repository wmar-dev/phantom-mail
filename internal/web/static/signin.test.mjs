import { test } from 'node:test';
import assert from 'node:assert/strict';
import { needsSignIn, signIn, signOut } from './signin.js';

function fakeFetch(status, record) {
  return async (url, init) => {
    record.push({ url, init });
    return { status, ok: status >= 200 && status < 300 };
  };
}

test('a 401 means the sign-in prompt is needed; other statuses do not', () => {
  assert.equal(needsSignIn(401), true);
  for (const s of [200, 204, 403, 404, 429, 500]) assert.equal(needsSignIn(s), false);
});

test('signIn posts the token as JSON in the body, never in the URL', async () => {
  const calls = [];
  const result = await signIn(fakeFetch(204, calls), 's3cret');
  assert.deepEqual(result, { ok: true });
  assert.equal(calls.length, 1);
  const { url, init } = calls[0];
  assert.equal(url, '/api/v1/session');
  assert.ok(!url.includes('s3cret'), 'token must not appear in the URL');
  assert.equal(init.method, 'POST');
  assert.equal(init.credentials, 'same-origin');
  assert.equal(init.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(init.body), { token: 's3cret' });
});

test('signIn maps failures to reasons the dialog can show', async () => {
  assert.deepEqual(await signIn(fakeFetch(401, []), 'x'), { ok: false, reason: 'invalid' });
  assert.deepEqual(await signIn(fakeFetch(429, []), 'x'), { ok: false, reason: 'rate_limited' });
  assert.deepEqual(await signIn(fakeFetch(500, []), 'x'), { ok: false, reason: 'error' });
  const down = async () => { throw new TypeError('network down'); };
  assert.deepEqual(await signIn(down, 'x'), { ok: false, reason: 'error' });
});

test('signIn rejects an empty token without calling the server', async () => {
  const calls = [];
  assert.deepEqual(await signIn(fakeFetch(204, calls), '   '), { ok: false, reason: 'empty' });
  assert.equal(calls.length, 0);
});

test('the token is never written to browser storage', async () => {
  const touched = [];
  const trap = { setItem: (...a) => touched.push(a), getItem: () => null };
  globalThis.localStorage = trap;
  globalThis.sessionStorage = trap;
  try {
    await signIn(fakeFetch(204, []), 'topsecret');
  } finally {
    delete globalThis.localStorage;
    delete globalThis.sessionStorage;
  }
  assert.deepEqual(touched, []);
});

test('signOut sends DELETE to the session endpoint', async () => {
  const calls = [];
  const result = await signOut(fakeFetch(204, calls));
  assert.deepEqual(result, { ok: true });
  assert.equal(calls[0].url, '/api/v1/session');
  assert.equal(calls[0].init.method, 'DELETE');
  assert.equal(calls[0].init.credentials, 'same-origin');
});
