import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  normalizeMailbox, listModel, mergeEvent, iframeAttrs, extractCodes,
  displayName, relativeTime, apiPath, parseHash, MAX_MESSAGES,
} from './lib.js';

test('normalizeMailbox lowercases, strips domain and +tag, and validates', () => {
  assert.deepEqual(normalizeMailbox('Alice'), { name: 'alice', valid: true });
  assert.deepEqual(normalizeMailbox('  alice  '), { name: 'alice', valid: true });
  assert.deepEqual(normalizeMailbox('Alice+Shop@Mail.Example.com'), { name: 'alice', valid: true });
  assert.deepEqual(normalizeMailbox('a.b_c-d9'), { name: 'a.b_c-d9', valid: true });
  for (const bad of ['', '   ', '../x', 'a/b', 'a b', '..', '.', 'ü', '+tag', '@host', 'x'.repeat(65)]) {
    assert.equal(normalizeMailbox(bad).valid, false, `expected ${JSON.stringify(bad)} to be invalid`);
  }
});

test('displayName extracts the friendly part of a From header', () => {
  assert.equal(displayName('Sender <sender@example.com>'), 'Sender');
  assert.equal(displayName('"Big Co." <no-reply@big.co>'), 'Big Co.');
  assert.equal(displayName('plain@example.com'), 'plain@example.com');
  assert.equal(displayName(''), '(unknown sender)');
});

test('relativeTime renders short human-readable ages', () => {
  const now = new Date('2026-10-07T12:00:00Z');
  assert.equal(relativeTime('2026-10-07T11:59:58Z', now), 'just now');
  assert.equal(relativeTime('2026-10-07T11:58:00Z', now), '2 min ago');
  assert.equal(relativeTime('2026-10-07T09:00:00Z', now), '3 h ago');
  assert.equal(relativeTime('2026-10-04T12:00:00Z', now), '3 d ago');
  assert.equal(relativeTime('garbage', now), '');
});

test('listModel builds rows with fallbacks and selection', () => {
  const now = new Date('2026-10-07T12:00:00Z');
  const rows = listModel([
    { id: 'b', subject: '', from: 'Sender <s@e.com>', received_at: '2026-10-07T11:59:00Z', attachment_count: 2 },
    { id: 'a', subject: 'Hello', from: '', received_at: '2026-10-07T11:00:00Z', attachment_count: 0 },
  ], 'a', now);
  assert.equal(rows.length, 2);
  assert.deepEqual(rows[0], { id: 'b', subject: '(no subject)', from: 'Sender', when: '1 min ago', selected: false, hasAttachments: true });
  assert.equal(rows[1].selected, true);
  assert.equal(rows[1].hasAttachments, false);
  assert.equal(rows[1].from, '(unknown sender)');
});

test('mergeEvent adds new messages newest-first without duplicates', () => {
  let list = [{ id: '0002' }, { id: '0001' }];
  list = mergeEvent(list, { type: 'message', summary: { id: '0003' } });
  assert.deepEqual(list.map((m) => m.id), ['0003', '0002', '0001']);
  const again = mergeEvent(list, { type: 'message', summary: { id: '0003' } });
  assert.deepEqual(again.map((m) => m.id), ['0003', '0002', '0001']);
  const mid = mergeEvent(list, { type: 'message', summary: { id: '0002a' } });
  assert.deepEqual(mid.map((m) => m.id), ['0003', '0002a', '0002', '0001']);
});

test('mergeEvent removes deleted messages and does not mutate its input', () => {
  const list = Object.freeze([{ id: 'b' }, { id: 'a' }]);
  const out = mergeEvent(list, { type: 'deleted', id: 'a' });
  assert.deepEqual(out.map((m) => m.id), ['b']);
  assert.equal(list.length, 2);
  assert.deepEqual(mergeEvent(list, { type: 'deleted', id: 'zzz' }).map((m) => m.id), ['b', 'a']);
  assert.deepEqual(mergeEvent(list, { type: 'weird' }).map((m) => m.id), ['b', 'a']);
});

test('mergeEvent caps the list size', () => {
  let list = [];
  for (let i = 0; i < MAX_MESSAGES + 20; i++) {
    list = mergeEvent(list, { type: 'message', summary: { id: String(i).padStart(6, '0') } });
  }
  assert.equal(list.length, MAX_MESSAGES);
  assert.equal(list[0].id, String(MAX_MESSAGES + 19).padStart(6, '0'));
});

test('iframeAttrs never grants scripts or same-origin access', () => {
  const a = iframeAttrs('alice', 'abc123');
  assert.equal(a.src, '/api/v1/mailboxes/alice/messages/abc123/html');
  assert.equal(a.sandbox, '');
  assert.equal(a.referrerpolicy, 'no-referrer');
  assert.ok(!/allow-/.test(a.sandbox));
  assert.equal(iframeAttrs('a b', 'x/y').src, '/api/v1/mailboxes/a%20b/messages/x%2Fy/html');
});

test('apiPath encodes every segment', () => {
  assert.equal(apiPath('alice'), '/api/v1/mailboxes/alice/messages');
  assert.equal(apiPath('alice', 'id1'), '/api/v1/mailboxes/alice/messages/id1');
  assert.equal(apiPath('alice', 'id1', 'attachments/0'), '/api/v1/mailboxes/alice/messages/id1/attachments/0');
});

test('extractCodes finds standalone verification codes', () => {
  assert.deepEqual(extractCodes('Your code is 482913.'), ['482913']);
  assert.deepEqual(extractCodes('Use 1234 or 567890'), ['1234', '567890']);
  assert.deepEqual(extractCodes('code 111111 and again 111111'), ['111111']);
  assert.deepEqual(extractCodes('order 12345678901 and phone 555-1234'), []);
  assert.deepEqual(extractCodes('no digits here'), []);
  assert.deepEqual(extractCodes(''), []);
});

test('parseHash reads #/mailbox and #/mailbox/id', () => {
  assert.deepEqual(parseHash(''), { mailbox: '', id: '' });
  assert.deepEqual(parseHash('#/alice'), { mailbox: 'alice', id: '' });
  assert.deepEqual(parseHash('#/alice/abc'), { mailbox: 'alice', id: 'abc' });
  assert.deepEqual(parseHash('#/a%20b/x'), { mailbox: 'a b', id: 'x' });
  assert.deepEqual(parseHash('#nonsense'), { mailbox: '', id: '' });
});
