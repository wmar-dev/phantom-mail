import {
  normalizeMailbox, listModel, mergeEvent, iframeAttrs, extractCodes,
  apiPath, eventsPath, parseHash,
} from './lib.js';
import { promptSignIn } from './signin.js';

// Everything that comes from an email is inserted with textContent only, never
// as HTML. The HTML body is shown in a sandboxed iframe served by the API.

const $ = (id) => document.getElementById(id);
const state = { mailbox: '', messages: [], selected: '', detail: null, view: 'rendered', stream: null, wasDown: false };

function el(tag, props = {}, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === 'text') e.textContent = v;
    else if (k in e && k !== 'list') e[k] = v;
    else e.setAttribute(k, v);
  }
  for (const kid of kids) if (kid) e.append(kid);
  return e;
}

function setStatus(text, cls = '') {
  const s = $('status');
  s.textContent = text;
  s.className = cls;
}

class AuthError extends Error {}

async function api(path, init = {}) {
  const res = await fetch(path, { credentials: 'same-origin', ...init });
  if (res.status === 401) {
    await onAuthRequired();
    throw new AuthError('authentication required');
  }
  return res;
}

// A 401 means the instance is protected by an access token. Ask for it once,
// then reload: the page re-opens the same mailbox (the hash is preserved) with
// the new session cookie, including its live stream.
async function onAuthRequired() {
  setStatus('Sign-in required.', 'warn');
  await promptSignIn();
  location.reload();
}

// ---------- list ----------

function renderList() {
  const rows = listModel(state.messages, state.selected);
  const ul = $('messages');
  ul.replaceChildren(...rows.map((r) => {
    const btn = el('button', { type: 'button' },
      el('div', { className: 'row-top' }, el('span', { text: r.from }), el('span', { text: (r.hasAttachments ? '\u{1F4CE} ' : '') + r.when })),
      el('div', { className: 'row-subject', text: r.subject }));
    if (r.selected) btn.setAttribute('aria-current', 'true');
    btn.addEventListener('click', () => { location.hash = `#/${encodeURIComponent(state.mailbox)}/${encodeURIComponent(r.id)}`; });
    return el('li', {}, btn);
  }));
  $('empty-note').hidden = rows.length > 0;
  document.title = `${rows.length ? `(${rows.length}) ` : ''}${state.mailbox} – Phantom Mail`;
}

async function refresh() {
  const res = await api(apiPath(state.mailbox));
  if (!res.ok) throw new Error(`list failed (${res.status})`);
  state.messages = (await res.json()).messages;
  renderList();
}

// ---------- live updates ----------

function closeStream() {
  if (state.stream) state.stream.close();
  state.stream = null;
}

function connect(mailbox) {
  closeStream();
  const es = new EventSource(eventsPath(mailbox));
  state.stream = es;
  es.addEventListener('open', async () => {
    setStatus('Live', 'live');
    if (state.wasDown) { // catch up on anything missed while disconnected
      state.wasDown = false;
      try { await refresh(); } catch { /* shown by api() */ }
    }
  });
  es.addEventListener('error', async () => {
    state.wasDown = true;
    setStatus('Reconnecting…', 'warn');
    if (es.readyState === EventSource.CLOSED) { // not retried by the browser: probe why
      try { await refresh(); connect(mailbox); } catch { /* auth prompt or offline */ }
    }
  });
  es.addEventListener('message', (ev) => {
    state.messages = mergeEvent(state.messages, { type: 'message', summary: JSON.parse(ev.data) });
    renderList();
  });
  es.addEventListener('deleted', (ev) => {
    const { id } = JSON.parse(ev.data);
    state.messages = mergeEvent(state.messages, { type: 'deleted', id });
    if (state.selected === id) clearDetail();
    renderList();
  });
}

// ---------- detail ----------

function clearDetail() {
  state.selected = '';
  state.detail = null;
  $('detail').hidden = true;
  $('pick').hidden = false;
}

function copy(text, btn) {
  const done = () => { const old = btn.textContent; btn.textContent = 'Copied'; setTimeout(() => { btn.textContent = old; }, 1200); };
  if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, () => {});
  else done();
}

function renderBody() {
  const d = state.detail;
  const body = $('d-body');
  $('tab-rendered').setAttribute('aria-pressed', String(state.view === 'rendered'));
  $('tab-text').setAttribute('aria-pressed', String(state.view === 'text'));
  if (state.view === 'text') {
    body.replaceChildren(el('pre', { text: d.text || '(no plain text part)' }));
    return;
  }
  const a = iframeAttrs(state.mailbox, d.id);
  const frame = el('iframe', { title: 'Message body' });
  frame.setAttribute('sandbox', a.sandbox);
  frame.setAttribute('referrerpolicy', a.referrerpolicy);
  frame.src = a.src;
  body.replaceChildren(frame);
}

function renderDetail() {
  const d = state.detail;
  $('pick').hidden = true;
  $('detail').hidden = false;
  $('d-subject').textContent = d.subject || '(no subject)';
  $('d-from').textContent = d.from || '(unknown sender)';
  $('d-to').textContent = (d.to || []).join(', ') || '—';
  $('d-date').textContent = new Date(d.received_at).toLocaleString();

  const codes = extractCodes(`${d.subject}\n${d.text || ''}`);
  const box = $('d-codes');
  box.hidden = codes.length === 0;
  box.replaceChildren(...(codes.length ? [el('span', { className: 'muted', text: 'Codes (click to copy):' })] : []),
    ...codes.map((c) => { const b = el('button', { type: 'button', text: c, title: 'Copy code' }); b.addEventListener('click', () => copy(c, b)); return b; }));

  $('tab-rendered').textContent = d.html ? 'Message' : 'Message (text)';
  renderBody();

  const ul = $('d-attachments');
  ul.hidden = !d.attachments.length;
  ul.replaceChildren(...d.attachments.map((a) => {
    const link = el('a', { href: apiPath(state.mailbox, d.id, `attachments/${a.index}`), text: a.filename });
    link.setAttribute('download', a.filename);
    return el('li', {}, link, el('span', { className: 'muted', text: ` (${a.content_type}, ${a.size} bytes)` }));
  }));
}

async function select(id) {
  state.selected = id;
  renderList();
  try {
    const res = await api(apiPath(state.mailbox, id));
    if (res.status === 404) { clearDetail(); renderList(); setStatus('That message no longer exists.', 'warn'); return; }
    if (!res.ok) throw new Error(`message failed (${res.status})`);
    if (state.selected !== id) return; // user clicked something else meanwhile
    state.detail = await res.json();
    renderDetail();
  } catch (e) {
    if (!(e instanceof AuthError)) setStatus('Could not load the message.', 'warn');
  }
}

// ---------- mailbox ----------

async function openMailbox(name, id) {
  state.mailbox = name;
  state.messages = [];
  clearDetail();
  $('welcome').hidden = true;
  $('inbox').hidden = false;
  $('mailbox').value = name;
  $('address').textContent = `${name}@${location.hostname}`;
  try { localStorage.setItem('pm.mailbox', name); } catch { /* storage may be blocked */ }
  renderList();
  connect(name); // subscribe first so nothing is missed between list and stream
  try { await refresh(); } catch (e) { if (!(e instanceof AuthError)) setStatus('Could not load messages.', 'warn'); }
  if (id) select(id);
}

function route() {
  const { mailbox, id } = parseHash(location.hash);
  const n = normalizeMailbox(mailbox);
  if (!mailbox || !n.valid) {
    closeStream();
    state.mailbox = '';
    $('inbox').hidden = true;
    $('welcome').hidden = false;
    document.title = 'Phantom Mail';
    setStatus(mailbox ? 'Mailbox names use letters, digits, . _ and -' : '', mailbox ? 'warn' : '');
    return;
  }
  if (n.name !== state.mailbox) openMailbox(n.name, id);
  else if (id && id !== state.selected) select(id);
  else if (!id && state.selected) { clearDetail(); renderList(); }
}

// ---------- wiring ----------

document.querySelectorAll('.host').forEach((h) => { h.textContent = location.hostname; });

$('open-form').addEventListener('submit', (ev) => {
  ev.preventDefault();
  const n = normalizeMailbox($('mailbox').value);
  if (!n.valid) { setStatus('Mailbox names use letters, digits, . _ and - (up to 64).', 'warn'); return; }
  setStatus('');
  location.hash = `#/${encodeURIComponent(n.name)}`;
});

$('copy-address').addEventListener('click', (ev) => copy($('address').textContent, ev.currentTarget));

$('tab-rendered').addEventListener('click', () => { state.view = 'rendered'; if (state.detail) renderBody(); });
$('tab-text').addEventListener('click', () => { state.view = 'text'; if (state.detail) renderBody(); });

$('d-delete').addEventListener('click', async () => {
  const id = state.selected;
  if (!id) return;
  const res = await api(apiPath(state.mailbox, id), { method: 'DELETE' });
  if (res.ok || res.status === 404) {
    state.messages = mergeEvent(state.messages, { type: 'deleted', id });
    clearDetail();
    renderList();
    location.hash = `#/${encodeURIComponent(state.mailbox)}`;
  }
});

$('empty-mailbox').addEventListener('click', async () => {
  if (!state.messages.length || !confirm(`Delete all messages in ${state.mailbox}?`)) return;
  const res = await api(apiPath(state.mailbox), { method: 'DELETE' });
  if (res.ok) {
    state.messages = [];
    clearDetail();
    renderList();
  }
});

window.addEventListener('hashchange', route);
window.setInterval(() => { if (state.mailbox) renderList(); }, 30000); // keep "x min ago" fresh

if (!location.hash) {
  try { $('mailbox').value = localStorage.getItem('pm.mailbox') || ''; } catch { /* ignore */ }
}
route();
