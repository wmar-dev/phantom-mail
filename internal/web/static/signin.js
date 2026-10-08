// Access-token sign-in. The token is sent once in a POST body; the server
// answers with an HttpOnly session cookie. It is never put in a URL and never
// written to localStorage or sessionStorage.

const SESSION_URL = '/api/v1/session';

/** Whether a response status means the sign-in prompt is needed. */
export function needsSignIn(status) {
  return status === 401;
}

/**
 * Exchanges the token for a session cookie.
 * Resolves to { ok: true } or { ok: false, reason: 'empty'|'invalid'|'rate_limited'|'error' }.
 */
export async function signIn(fetchFn, token) {
  if (!String(token ?? '').trim()) return { ok: false, reason: 'empty' };
  let res;
  try {
    res = await fetchFn(SESSION_URL, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: String(token).trim() }),
    });
  } catch {
    return { ok: false, reason: 'error' };
  }
  if (res.ok) return { ok: true };
  if (res.status === 401) return { ok: false, reason: 'invalid' };
  if (res.status === 429) return { ok: false, reason: 'rate_limited' };
  return { ok: false, reason: 'error' };
}

/** Clears the session cookie. */
export async function signOut(fetchFn) {
  try {
    const res = await fetchFn(SESSION_URL, { method: 'DELETE', credentials: 'same-origin' });
    return { ok: res.ok };
  } catch {
    return { ok: false };
  }
}

const MESSAGES = {
  empty: 'Enter the access token.',
  invalid: 'That token is not correct.',
  rate_limited: 'Too many attempts. Wait a minute and try again.',
  error: 'Could not reach the server.',
};

let pending = null;

/**
 * Shows the sign-in dialog and resolves once the viewer has signed in. Calls
 * made while the dialog is open share the same promise.
 */
export function promptSignIn() {
  if (pending) return pending;
  const dialog = document.getElementById('signin');
  const form = document.getElementById('signin-form');
  const input = document.getElementById('token');
  const msg = document.getElementById('signin-msg');
  pending = new Promise((resolve) => {
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      msg.textContent = '';
      const result = await signIn((u, i) => fetch(u, i), input.value);
      input.value = '';
      if (result.ok) {
        dialog.close();
        pending = null;
        resolve();
      } else {
        msg.textContent = MESSAGES[result.reason];
        input.focus();
      }
    });
  });
  dialog.showModal();
  input.focus();
  return pending;
}
