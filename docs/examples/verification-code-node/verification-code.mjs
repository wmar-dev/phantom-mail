// Wait for a verification email in a Phantom Mail mailbox and print the code.
//
// The Node.js version of the pattern in docs/testing-verification-flows.md. It
// uses only Node's built-ins (Node.js 18 or newer); there is nothing to install.
//
//     node docs/examples/verification-code-node/verification-code.mjs --mailbox signup-123
//
// Exit status 0 means a code was printed; 1 means no email arrived in time or it
// contained no code; 2 means a usage or connection error.
import { parseArgs } from "node:util";

const UNITS = { ms: 0.001, s: 1, m: 60 };

class NoCode extends Error {}

function duration(text) {
  const m = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(text);
  if (!m) throw new Error(`bad --timeout: ${JSON.stringify(text)} is not a duration such as 30s`);
  return Number(m[1]) * UNITS[m[2]];
}

async function get(url, token, timeoutSecs) {
  const headers = token ? { Authorization: `Bearer ${token}` } : {};
  const resp = await fetch(url, { headers, signal: AbortSignal.timeout(timeoutSecs * 1000) });
  if (resp.status === 204) return null;
  const body = await resp.text();
  if (!resp.ok) throw new Error(`${url} returned ${resp.status} ${resp.statusText}: ${body.trim()}`);
  return JSON.parse(body);
}

async function waitForCode(api, box, timeout, pattern, token) {
  const secs = Math.min(Math.max(Math.trunc(timeout), 1), 60);
  const base = `${api.replace(/\/+$/, "")}/api/v1/mailboxes/${encodeURIComponent(box)}/messages`;
  const listing = await get(`${base}/wait?timeout=${secs}`, token, secs + 10);
  if (!listing || !listing.messages?.length) {
    throw new NoCode(`no email arrived in mailbox "${box}" within ${secs}s`);
  }
  const msg = await get(`${base}/${listing.messages[0].id}`, token, secs + 10);
  for (const field of [msg.subject, msg.text, msg.html]) {
    const m = pattern.exec(field ?? "");
    if (m) return m[0];
  }
  throw new NoCode(`no code matching "${pattern.source}" in the email "${msg.subject}"`);
}

function usage(message) {
  console.error(message);
  console.error("usage: verification-code.mjs --mailbox NAME [--api URL] [--timeout 30s] [--pattern REGEXP]");
  process.exit(2);
}

let args;
try {
  args = parseArgs({
    options: {
      mailbox: { type: "string" },
      api: { type: "string", default: "http://localhost:8080" },
      timeout: { type: "string", default: "30s" },
      pattern: { type: "string", default: "\\b\\d{6}\\b" },
    },
  }).values;
} catch (e) {
  usage(e.message);
}
if (!args.mailbox) usage("--mailbox is required");

let timeout, pattern;
try {
  timeout = duration(args.timeout);
  pattern = new RegExp(args.pattern);
} catch (e) {
  usage(e.message.startsWith("bad --timeout") ? e.message : `bad --pattern: ${e.message}`);
}

try {
  console.log(await waitForCode(args.api, args.mailbox, timeout, pattern, process.env.PM_API_TOKEN ?? ""));
} catch (e) {
  console.error(e instanceof NoCode ? e.message : `${e.message}${e.cause ? `: ${e.cause.message ?? e.cause}` : ""}`);
  process.exit(e instanceof NoCode ? 1 : 2);
}
