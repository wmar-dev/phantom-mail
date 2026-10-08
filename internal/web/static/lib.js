// Pure helpers for the web interface. No DOM access here so they can be
// unit tested with Node's built-in test runner.

export const MAX_MESSAGES = 100;

const enc = encodeURIComponent;

/** Normalizes what a person types (name or full address) like the server does. */
export function normalizeMailbox(input) {
  let s = String(input ?? '').trim().toLowerCase();
  const at = s.indexOf('@');
  if (at >= 0) s = s.slice(0, at);
  s = s.split('+')[0];
  const valid = /^[a-z0-9._-]{1,64}$/.test(s) && s !== '.' && s !== '..';
  return { name: s, valid };
}

/** The friendly part of a From header: "Sender <a@b.c>" -> "Sender". */
export function displayName(from) {
  const f = String(from ?? '').trim();
  if (!f) return '(unknown sender)';
  const m = f.match(/^"?([^"<]*?)"?\s*<[^>]*>$/);
  return m && m[1].trim() ? m[1].trim() : f;
}

/** Short relative age such as "3 min ago". Returns '' for unparsable input. */
export function relativeTime(iso, now = new Date()) {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const s = Math.max(0, (now.getTime() - t) / 1000);
  if (s < 10) return 'just now';
  if (s < 60) return `${Math.floor(s)} s ago`;
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

/** Rows to render for the message list. */
export function listModel(messages, selectedId, now = new Date()) {
  return messages.map((m) => ({
    id: m.id,
    subject: m.subject || '(no subject)',
    from: displayName(m.from),
    when: relativeTime(m.received_at, now),
    selected: m.id === selectedId,
    hasAttachments: (m.attachment_count || 0) > 0,
  }));
}

/** Applies a live event to the (newest-first) list without mutating it. */
export function mergeEvent(list, ev) {
  if (ev && ev.type === 'message' && ev.summary && ev.summary.id) {
    if (list.some((m) => m.id === ev.summary.id)) return list.slice();
    return [...list, ev.summary]
      .sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0))
      .slice(0, MAX_MESSAGES);
  }
  if (ev && ev.type === 'deleted') return list.filter((m) => m.id !== ev.id);
  return list.slice();
}

/** URL helpers. Every path segment is encoded. */
export function apiPath(mailbox, id, suffix) {
  let p = `/api/v1/mailboxes/${enc(mailbox)}/messages`;
  if (id) p += `/${enc(id)}`;
  if (suffix) p += `/${suffix}`;
  return p;
}

export function eventsPath(mailbox) {
  return `/api/v1/mailboxes/${enc(mailbox)}/events`;
}

/**
 * Attributes for the iframe showing an email body. The empty sandbox grants
 * nothing: no scripts, no same-origin access, no forms, no popups.
 */
export function iframeAttrs(mailbox, id) {
  return { src: apiPath(mailbox, id, 'html'), sandbox: '', referrerpolicy: 'no-referrer' };
}

/** Standalone 4-8 digit numbers, which is what verification codes look like. */
export function extractCodes(text) {
  const out = [];
  for (const m of String(text ?? '').matchAll(/(?<![\w-])\d{4,8}(?![\w-])/g)) {
    if (!out.includes(m[0])) out.push(m[0]);
  }
  return out;
}

/** Reads "#/mailbox" or "#/mailbox/messageId". */
export function parseHash(hash) {
  const m = String(hash ?? '').match(/^#\/([^/]*)(?:\/(.*))?$/);
  if (!m) return { mailbox: '', id: '' };
  const dec = (s) => {
    try {
      return decodeURIComponent(s || '');
    } catch {
      return '';
    }
  };
  return { mailbox: dec(m[1]), id: dec(m[2]) };
}
