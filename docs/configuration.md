# Configuration reference

All settings are environment variables beginning with `PM_`. Every one has a default that works
on a laptop, so an empty environment is a valid configuration. A blank value means "use the
default". Invalid values stop the service at startup with a message naming the variable, for
example `PM_RETENTION: "tomorrow" is not a duration such as 90m or 24h`; all problems are
reported together.

- Durations use Go syntax: `90s`, `15m`, `24h`, `1h30m`.
- Sizes are a plain number of bytes or a number with a unit: `KiB`, `MiB`, `GiB` (powers of 1024)
  or `KB`, `MB`, `GB` (powers of 1000). `10MiB` and `10485760` are the same.
- Rates are "per minute" and allow a burst of up to that many at once.

With Docker Compose, put variables in the environment before `docker compose up` (the supplied
`compose.yaml` forwards the common ones) or edit the `environment:` section. With `docker run`,
use `-e NAME=value`.

## Quick reference

| Variable | Default | Summary |
|----------|---------|---------|
| [`PM_DOMAINS`](#pm_domains) | `localhost` | Mail domains this instance accepts mail for |
| [`PM_SMTP_ADDR`](#pm_smtp_addr) | `:2525` | Where the mail receiver listens |
| [`PM_HTTP_ADDR`](#pm_http_addr) | `:8080` | Where the web interface and API listen |
| [`PM_DATA_DIR`](#pm_data_dir) | `./data` (`/data` in the image) | Where messages are stored |
| [`PM_STORAGE`](#pm_storage) | `fs` | `fs` (on disk) or `memory` (not persisted) |
| [`PM_RETENTION`](#pm_retention) | `24h` | How long a message is kept |
| [`PM_JANITOR_INTERVAL`](#pm_janitor_interval) | `30s` | How often expired mail is physically removed |
| [`PM_MAX_MESSAGES_PER_MAILBOX`](#pm_max_messages_per_mailbox) | `100` | Newest messages kept per mailbox |
| [`PM_MAX_MAILBOXES`](#pm_max_mailboxes) | `10000` | Mailboxes that may exist at once |
| [`PM_MAX_STREAMS_PER_CLIENT`](#pm_max_streams_per_client) | `100` | Open live-event streams per client address |
| [`PM_MAX_MESSAGE_BYTES`](#pm_max_message_bytes) | `10MiB` | Largest accepted message |
| [`PM_MAX_TOTAL_BYTES`](#pm_max_total_bytes) | `1GiB` | Total storage cap |
| [`PM_RATE_SMTP_CONN`](#pm_rate_smtp_conn) | `600` | SMTP connections per minute per sender address |
| [`PM_RATE_SMTP_MESSAGE`](#pm_rate_smtp_message) | `3000` | Messages per minute per sender address |
| [`PM_RATE_MAILBOX`](#pm_rate_mailbox) | `600` | Messages per minute per mailbox |
| [`PM_RATE_HTTP`](#pm_rate_http) | `6000` | HTTP requests per minute per client address |
| [`PM_RATE_AUTH_FAIL`](#pm_rate_auth_fail) | `10` | Failed sign-ins per minute per client address |
| [`PM_API_TOKEN`](#pm_api_token) | _(none)_ | Require this token for the web interface and API |
| [`PM_TRUSTED_PROXIES`](#pm_trusted_proxies) | _(none)_ | Reverse proxies whose forwarded headers are believed |
| [`PM_LOG_LEVEL`](#pm_log_level) | `info` | `debug`, `info`, `warn` or `error` |

## Addresses and domains

### PM_DOMAINS

Comma-separated list of mail domains, e.g. `inbox.example.com,test.example.org`. Mail addressed
to any other domain is refused with `550 Relay access denied`, which is also what prevents the
service from being used as an open relay. Matching is case-insensitive. Each entry must be a valid
host name. Mailbox names are **not** scoped by domain: with several domains, `alice@a.example`
and `alice@b.example` are the same mailbox.

Default `localhost`. Example: `PM_DOMAINS=inbox.example.com`.

### PM_SMTP_ADDR

Listen address of the SMTP receiver as `host:port`. The default `:2525` listens on all
interfaces. Use `127.0.0.1:2525` to accept only local mail. The container runs as a non-root user
and cannot bind ports below 1024, so publish host port 25 to container port 2525
(`-p 25:2525`).

### PM_HTTP_ADDR

Listen address of the web interface and API, default `:8080`. The service speaks plain HTTP; put
an HTTPS proxy in front of it in production (see [deployment](deployment.md)). The container
health check reads this variable to find its own port.

## Storage and retention

### PM_DATA_DIR

Directory for stored messages, default `./data` (the image sets `/data`). Each message is one file
`<mailbox>/<id>.eml`. It is created if missing. Use a persistent volume in containers. Run only
one instance per directory.

### PM_STORAGE

`fs` (default) keeps messages on disk and rebuilds its index at startup; `memory` keeps them in
RAM and loses everything on exit. `memory` is for demos and tests where nothing needs to survive.
Limits and retention apply to both.

### PM_RETENTION

How long a message is kept, counted from its arrival. Default `24h`. Expired messages disappear
from every read (list, fetch, wait, events) the moment they expire, even before the next sweep
physically deletes them.

### PM_JANITOR_INTERVAL

How often the background sweep deletes expired files and enforces the total size cap. Default
`30s`. A sweep also runs once at startup, so a restart after downtime cleans up immediately.
Raising it only delays disk reclamation; it never changes what clients can see.

### PM_MAX_MESSAGES_PER_MAILBOX

The most messages a mailbox keeps, default `100`. When a new message arrives in a full mailbox
the oldest is deleted.

### PM_MAX_MAILBOXES

The most mailboxes that may exist at once, default `10000`. A mailbox exists while it holds at
least one message and disappears (including its directory) when its last message is deleted or
expires. When the cap is reached, mail for a **new** mailbox name is refused with a temporary
failure (`452`) while existing mailboxes keep receiving. This stops a flood of random addresses
from creating unbounded directories.

### PM_MAX_STREAMS_PER_CLIENT

How many live-event streams (`/api/v1/mailboxes/{mailbox}/events`) one client address may keep
open at once, default `100`. Each open mailbox in the web interface uses one stream. Beyond the
limit the API answers `429`. Scripts that only need to wait for mail should use the
[wait endpoint](api.md#wait-for-a-message), which is not counted. Behind a proxy set
[`PM_TRUSTED_PROXIES`](#pm_trusted_proxies) so each real client is counted separately.

### PM_MAX_MESSAGE_BYTES

The largest message accepted, default `10MiB`, including attachments. It is advertised in the
SMTP `SIZE` extension; a bigger declared size is refused immediately (`552`) and a bigger actual
transfer is discarded as it streams, so memory stays bounded.

### PM_MAX_TOTAL_BYTES

Cap on all stored messages together, default `1GiB`. When an arrival would exceed it, the oldest
messages across all mailboxes are deleted until it fits. Set this below the free space of the
volume. If the disk itself fills up, senders get a temporary failure (`452`) and retry later.

## Rate limits

Limits protect availability when an instance is exposed to the internet. Each is a token bucket
per key: up to the configured number of events may happen at once, refilling evenly over a
minute. When a limit is hit, SMTP answers with a temporary `421`/`451` (senders retry) and HTTP
answers `429` with `Retry-After`.

### PM_RATE_SMTP_CONN

SMTP connections per minute from one remote address. Default `600`.

### PM_RATE_SMTP_MESSAGE

Messages (`MAIL FROM` transactions) per minute from one remote address. Default `3000`.

### PM_RATE_MAILBOX

Messages per minute delivered to one mailbox (all `+tag` variants count together). Default `600`.
This keeps one flooded mailbox from affecting others.

### PM_RATE_HTTP

HTTP requests per minute per client address, excluding `/healthz`. Default `6000`.

### PM_RATE_AUTH_FAIL

Failed sign-in attempts per minute per client address. Default `10`. When exhausted, sign-in is
refused (even with the right token) until the budget refills, so a token cannot be guessed by
brute force.

## Access control and proxies

### PM_API_TOKEN

When set, the API and web interface require this token; mail reception is unaffected, and
`/healthz`, `/openapi.yaml` and the static page itself stay open so the sign-in prompt can load.
Scripts send `Authorization: Bearer <token>`; browsers sign in once and receive a session cookie
(see [API authentication](api.md#authentication)). Use a long random string, for example
`openssl rand -hex 24`. The token is never logged and never accepted in a URL. Unset (default)
means anyone can read any mailbox.

### PM_TRUSTED_PROXIES

Comma-separated IP addresses or CIDR ranges of reverse proxies in front of the HTTP port, e.g.
`10.0.0.0/8, 192.168.1.5`. Only requests whose direct peer is in this list may supply
`X-Forwarded-For` (the real client address, used for rate limiting) and `X-Forwarded-Proto`
(whether to mark the session cookie `Secure`). Leave empty if no proxy is used. Never include a
range untrusted clients can connect from, because they could then fake their address.

## Logging

### PM_LOG_LEVEL

`debug`, `info` (default), `warn` or `error`. Logs are JSON, one object per line, on standard
output. Fields always include `time`, `level` and `msg`. Accepted and rejected mail, HTTP requests
(not health checks), startup and shutdown are logged at `info`; rate-limit and storage problems
at `warn`/`error`. Message content, passwords and the access token are never logged.

## Examples

Local development with a short retention:

```bash
PM_RETENTION=15m PM_MAX_MESSAGES_PER_MAILBOX=20 make run
```

A public instance behind Caddy on a single host:

```bash
PM_DOMAINS=inbox.example.com
PM_API_TOKEN=$(openssl rand -hex 24)
PM_TRUSTED_PROXIES=172.16.0.0/12
PM_RETENTION=6h
PM_MAX_TOTAL_BYTES=2GiB
```

Ephemeral CI instance that keeps nothing:

```bash
PM_STORAGE=memory PM_HTTP_ADDR=127.0.0.1:8080 PM_SMTP_ADDR=127.0.0.1:2525 phantom-mail
```
