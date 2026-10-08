# API reference

Everything the web interface does is available over a small JSON API. The authoritative,
machine-readable description is the OpenAPI document, which every running instance serves at
`/openapi.yaml` (the same file lives in the repository at
`specs/001-disposable-inbox-service/contracts/openapi.yaml`, and the test suite fails if the
server and the document disagree).

Examples below assume a local instance at `http://localhost:8080` and use `curl` and `jq`.

- [Concepts](#concepts)
- [Authentication](#authentication)
- [List messages](#list-messages)
- [Wait for a message](#wait-for-a-message)
- [Get a message](#get-a-message)
- [Delete a message / empty a mailbox](#delete)
- [Attachments](#attachments)
- [HTML body](#html-body)
- [Live events (SSE)](#live-events-sse)
- [Health check and OpenAPI](#health-check-and-openapi)
- [Errors](#errors)
- [Limits](#limits)

## Concepts

- **Mailbox.** The part before the `@` in the address mail was sent to. Mailboxes exist as soon as
  mail arrives and need no registration. Names are case-insensitive and anything after a `+` is
  ignored, so `Alice+shop@mail.example.com` is mailbox `alice`. Valid names are 1 to 64 characters
  from letters, digits, `.`, `_` and `-`; anything else is rejected with `400 invalid_mailbox`.
  Mailbox names are not scoped by domain: `alice@a.example` and `alice@b.example` are the same
  mailbox when an instance serves both.
- **Message ID.** A 32-character hex string that sorts in arrival order (newer IDs are
  greater). IDs are what `after` parameters take.
- Times are RFC 3339 in UTC. Message lists are newest first.
- All API paths start with `/api/v1`. Responses are `application/json; charset=utf-8` unless noted.

## Authentication

By default no authentication is needed. If the operator set `PM_API_TOKEN`, every `/api/v1`
request must be authenticated, otherwise it gets `401`. `/healthz`, `/openapi.yaml` and the web
interface pages stay open.

**Scripts** send the token as a bearer header:

```bash
curl -H "Authorization: Bearer $PM_API_TOKEN" http://localhost:8080/api/v1/mailboxes/alice/messages
```

**Browsers** cannot add headers to event streams, frames or download links, so the web interface
exchanges the token once for a session cookie:

| Request | Result |
|---------|--------|
| `POST /api/v1/session` with body `{"token":"..."}` | `204` and a `pm_session` cookie (`HttpOnly`, `SameSite=Strict`, `Secure` over HTTPS, lasts for the browser session). `401` if the token is wrong, `400` for a malformed body, `429` after repeated failures. |
| `DELETE /api/v1/session` | `204`, clears the cookie. |

The cookie value is a keyed hash, not the token. Tokens in query strings are never accepted, so
`?token=...` does not work (it would leak into logs and history). Wrong guesses are rate limited
per client address (`PM_RATE_AUTH_FAIL`, default 10 per minute); while locked out even the right
token is refused for a short time. Behind a reverse proxy set `PM_TRUSTED_PROXIES` so the real
client address is used (see [configuration](configuration.md)).

## List messages

`GET /api/v1/mailboxes/{mailbox}/messages`

| Query | Meaning |
|-------|---------|
| `limit` | 1 to 100, default 100 |
| `after` | a message ID; only messages newer than it are returned |

```bash verify
curl -s http://localhost:8080/api/v1/mailboxes/alice/messages | jq
```

```json
{
  "messages": [
    {
      "id": "01a11985cc8c6da812921ae279508525",
      "mailbox": "alice",
      "from": "Shop <no-reply@shop.example>",
      "to": ["alice@localhost"],
      "subject": "Your verification code",
      "received_at": "2026-10-08T03:19:21.484Z",
      "size": 476,
      "attachment_count": 0
    }
  ]
}
```

An unused mailbox returns `{"messages":[]}`, not an error. `to` lists the addresses the mail was
sent to as written, including any `+tag`.

## Wait for a message

`GET /api/v1/mailboxes/{mailbox}/messages/wait`

A long poll built for tests. It answers immediately if the mailbox already has a matching message,
otherwise it holds the request open until one arrives.

| Query | Meaning |
|-------|---------|
| `timeout` | seconds to wait, 1 to 60, default 30 |
| `after` | a message ID; only messages newer than it count. **Omit it to count every message already in the mailbox**, so a message that arrived just before you started waiting is never missed. |

Responses: `200` with `{"messages":[...]}` (same shape as the list), or `204` with no body if
nothing arrived within the timeout. Several clients may wait on the same mailbox; all are released
by the same message.

Because an omitted `after` includes old mail, give each test run its own mailbox
(`test-$RANDOM`) or empty the mailbox first. To wait for the *next* message instead, remember the
newest ID and pass it as `after`.

```bash
# Wait up to 30 s for the verification email, then print the 6-digit code.
BOX="signup-$RANDOM"
# ... trigger the sign-up using $BOX@your.domain ...
ID=$(curl -sf "http://localhost:8080/api/v1/mailboxes/$BOX/messages/wait?timeout=30" | jq -r '.messages[0].id')
curl -s "http://localhost:8080/api/v1/mailboxes/$BOX/messages/$ID" | jq -r '.text' | grep -oE '[0-9]{6}' | head -1
```

## Get a message

`GET /api/v1/mailboxes/{mailbox}/messages/{id}`

Returns the summary fields above plus the decoded bodies and attachment metadata:

```json
{
  "id": "01a11985cc8c6da812921ae279508525",
  "mailbox": "alice",
  "from": "Shop <no-reply@shop.example>",
  "to": ["alice@localhost"],
  "subject": "Your verification code",
  "received_at": "2026-10-08T03:19:21.484Z",
  "size": 476,
  "attachment_count": 1,
  "text": "Your code is 482913",
  "html": "<h1>Code <b>482913</b></h1>",
  "attachments": [
    { "index": 0, "filename": "report.pdf", "content_type": "application/pdf", "size": 8 }
  ]
}
```

`text` and `html` are empty strings when the message has no such part. Bodies are decoded from
quoted-printable/base64 and from UTF-8, US-ASCII, ISO-8859-1 and Windows-1252; text in any other
character set is shown with replacement characters rather than failing. Malformed messages are
stored and shown on a best-effort basis.

## Delete

| Request | Result |
|---------|--------|
| `DELETE /api/v1/mailboxes/{mailbox}/messages/{id}` | `204`, or `404` if it does not exist |
| `DELETE /api/v1/mailboxes/{mailbox}/messages` | `204` and the mailbox is empty. Safe to repeat; an unused mailbox also returns `204`. |

Deletions are announced on the [event stream](#live-events-sse), so open browser tabs update.

## Attachments

`GET /api/v1/mailboxes/{mailbox}/messages/{id}/attachments/{index}`

`index` comes from the `attachments` list of the message. The response body is the decoded file
with its original `Content-Type` and a `Content-Disposition: attachment; filename=...` header.
Attachments are always served as downloads with `X-Content-Type-Options: nosniff` and a sandbox
Content-Security-Policy, so even an HTML attachment cannot run. File names are sanitized
(directories removed, control characters dropped). An unknown message or index returns `404`.

```bash
curl -sOJ http://localhost:8080/api/v1/mailboxes/alice/messages/$ID/attachments/0
```

## HTML body

`GET /api/v1/mailboxes/{mailbox}/messages/{id}/html`

Returns the message's HTML body (or its text wrapped in an escaped `<pre>`) as `text/html` with a
Content-Security-Policy that forbids scripts, remote images, fonts, styles, frames and forms. The
web interface shows it in a sandboxed `<iframe>`. If you embed it yourself, keep the sandbox:

```html
<iframe sandbox src="/api/v1/mailboxes/alice/messages/ID/html"></iframe>
```

## Live events (SSE)

`GET /api/v1/mailboxes/{mailbox}/events` returns a `text/event-stream`:

```text
retry: 3000

event: message
data: {"id":"...","mailbox":"alice","subject":"Your verification code", ...summary fields...}

event: deleted
data: {"id":"..."}

: keep-alive
```

- `message` events carry the same summary object as the list. `deleted` events carry the ID of a
  message that was deleted, expired or evicted.
- A comment line (`: keep-alive`) is sent every 15 seconds so proxies do not close an idle stream.
- Browsers reconnect automatically (`retry: 3000`). Events missed while disconnected are not
  replayed; after reconnecting, list the mailbox again.
- At most 100 streams per client address (`PM_MAX_STREAMS_PER_CLIENT`; `429 rate_limited` beyond
  that). To wait for mail from a script, prefer the wait endpoint, which is not counted.

```bash
curl -N http://localhost:8080/api/v1/mailboxes/alice/events
```

## Health check and OpenAPI

- `GET /healthz` returns `{"status":"ok","uptime_seconds":N,"messages":N,"store_bytes":N}`. It
  needs no authentication and is not rate limited.
- `GET /openapi.yaml` returns the OpenAPI 3 document.

## Errors

Errors are JSON with a stable `code` and a human-readable `message`:

```json
{ "error": { "code": "invalid_mailbox", "message": "mailbox names use 1-64 letters, digits, '.', '_' or '-'" } }
```

| HTTP status | `code` | When |
|-------------|--------|------|
| 400 | `invalid_mailbox` | The mailbox name is not valid |
| 400 | `bad_request` | A bad `limit`, `timeout`, `after` or request body; also a wrong method on a known path (405) |
| 401 | `unauthorized` | An access token is required, or is wrong |
| 404 | `not_found` | No such message, attachment or endpoint |
| 429 | `rate_limited` | Too many requests, failed sign-ins, or open event streams (see `Retry-After`) |
| 500 | `internal` | Unexpected server error (details are in the server log) |

## Limits

| Limit | Default | Setting |
|-------|---------|---------|
| Messages kept per mailbox (oldest dropped first) | 100 | `PM_MAX_MESSAGES_PER_MAILBOX` |
| Message retention | 24 hours | `PM_RETENTION` |
| Largest accepted message | 10 MiB | `PM_MAX_MESSAGE_BYTES` |
| API requests per client address | 6000 per minute | `PM_RATE_HTTP` |

See the [configuration reference](configuration.md) for all settings.
