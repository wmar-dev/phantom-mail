# Research & Decisions: Disposable Inbox Service

All Technical Context unknowns are resolved below. Guiding priority from the user: **performance
and low footprint**, within the constitution (tests, minimal dependencies, cloud/local/Docker
friendly, documented).

## R1. Language and runtime

- **Decision**: Go 1.23+, static binary (`CGO_ENABLED=0`).
- **Rationale**: Goroutine-per-connection handles many concurrent SMTP/SSE/long-poll clients
  with a few MB of memory; a ~8 MB static binary runs in a `scratch` image; the standard library
  includes `net`, `net/http`, `net/mail`, `mime/multipart`, `log/slog`, `embed`, `testing` so
  third-party dependencies can be zero (Principle II). Fast startup suits cloud restarts.
- **Alternatives considered**:
  - Rust: smaller/faster at the margin, but the SMTP/MIME stack needs several crates and
    compile times slow local iteration; violates minimal-dependency spirit.
  - Node.js/Python: far larger runtime footprint (50-100+ MB idle, 100+ MB image) and need
    third-party SMTP/MIME packages.
- **Note**: Go 1.27.1 is installed locally (verified); the minimum supported version stays 1.23.
  Builds and tests also run through Docker (`docker compose run --rm test`), satisfying
  Principle IV for machines without Go.

## R2. SMTP receiving

- **Decision**: Implement a minimal receive-only SMTP server in-repo (HELO/EHLO, MAIL, RCPT,
  DATA, RSET, NOOP, QUIT) using `net` and `bufio`. Enforce: accepted domains only (FR-003), max
  message size via `SIZE` advertisement and a hard read limit, per-connection timeouts, max
  recipients, no AUTH, no relay, and no outbound sending (FR-020). Hand parsed DATA to the
  store, then notify the hub.
- **Rationale**: Go's stdlib has no SMTP server; the popular libraries (`emersion/go-smtp`)
  would be the project's first dependency. The subset needed to receive mail is small and fully
  testable with the stdlib `net/smtp` client. Writing it ourselves also allows streaming with a
  hard size cap, giving predictable memory.
- **Alternatives considered**: `emersion/go-smtp` (well maintained, but violates the
  fewer-dependencies principle for ~400 lines of code); Postfix + pipe (heavy, not
  local-friendly).
- **STARTTLS**: Out of scope for v1. Many senders use opportunistic TLS and fall back to
  plaintext; verification emails from test targets are non-sensitive (public mailbox model). Can
  be added later with stdlib `crypto/tls` with no new dependency.

## R3. MIME parsing

- **Decision**: `net/mail` + `mime` + `mime/multipart` + `mime/quotedprintable`, with
  charset handling limited to UTF-8, US-ASCII, ISO-8859-1, and Windows-1252 (hand-mapped,
  avoiding `golang.org/x/text`); unknown charsets fall back to lossy UTF-8 with replacement
  characters.
- **Rationale**: Covers verification emails from real services. A full charset table would need
  `x/text` (a dependency); the four charsets cover the overwhelming majority of test traffic and
  the fallback guarantees display rather than failure (spec edge case on malformed messages).
- **Alternatives considered**: `enmime` (feature-rich but adds a dependency tree).

## R4. Storage

- **Decision**: One file per message (raw RFC 5322 bytes plus a small JSON header sidecar
  generated at ingest) under `PM_DATA_DIR/<mailbox>/<id>`, with an **in-memory metadata index**
  (id, mailbox, sender, subject, received time, size, attachment list) rebuilt by scanning the
  directory at startup. Bodies and attachments are read from disk on demand. Writes are
  `write temp + fsync + rename` so a crash never leaves a half-written message that is served.
  A `Store` interface fronts it with an in-memory implementation for tests and a
  `PM_STORAGE=memory` mode for ephemeral runs.
- **Rationale**: Keeps RSS small (only metadata resident), needs no database dependency, works
  on any volume (local bind mount, cloud block/NFS volume), survives restarts (US4 scenario 2),
  and makes retention trivial (delete files). Memory is bounded: index entries are ~200 bytes,
  so 10,000 messages ≈ 2 MB.
- **Alternatives considered**: SQLite (pure-Go driver `modernc.org/sqlite` adds a large
  dependency, +10 MB binary; CGO driver breaks static builds); embedded KV like bbolt/Badger
  (dependency, needless for this access pattern); Postgres/Redis (violates local-friendliness
  and footprint, adds services).
- **Durability trade-off**: Every message is fsynced before the SMTP `250` reply, so accepted
  mail is never lost. Ingest speed therefore depends on storage speed, and a slow volume may
  miss the stretch throughput goal; that is accepted, and no fsync toggle is provided.
- **Scale limit**: Single instance; horizontal scaling and shared storage across replicas are
  out of scope (spec assumption).

## R5. Retention and bounds

- **Decision**: A single janitor goroutine ticks every 30 s (configurable): deletes messages older
  than `PM_RETENTION` (default 24h); enforces `PM_MAX_MESSAGES_PER_MAILBOX` (default 100,
  oldest first, also checked inline on ingest) and `PM_MAX_TOTAL_BYTES` (default 1 GiB,
  oldest across mailboxes first). Expired messages are filtered out of reads immediately so
  they are never served between janitor ticks (SC-008: unavailable within 10 minutes; actually
  immediate).
- **Rationale**: Hard bounds on disk and memory by construction; lazily filtering on read
  guarantees correctness independent of the timer. A fake clock makes this deterministic in
  tests.

## R6. HTTP API and live updates

- **Decision**: REST/JSON under `/api/v1`, using `net/http` with Go 1.22+ pattern routing (no
  router dependency). Live updates via **Server-Sent Events** per mailbox; API clients use a
  **long-poll wait endpoint** (`GET .../messages/wait?timeout=`). Both are fed by an in-process
  hub (one buffered channel per subscriber, dropped on slow consumers) so a message becomes
  visible within milliseconds of ingest.
- **Rationale**: SSE needs no library on server or browser (`EventSource`), traverses proxies,
  is cheaper than WebSockets, and one-way is all that is required. Long-poll is trivially
  scriptable with `curl` in CI.
- **Alternatives considered**: WebSockets (dependency or a large hand-rolled implementation,
  bidirectional unnecessary); client polling only (slower, wasteful, fails SC-001 feel).

## R7. Web interface

- **Decision**: Static `index.html`, `app.js`, `app.css` (vanilla, no framework, no build step,
  no external requests) embedded with `go:embed`, served with gzip and long cache headers. HTML
  email bodies render inside a `<iframe sandbox>` pointing at a dedicated endpoint that returns
  the body with `Content-Security-Policy: default-src 'none'; img-src data:; style-src
  'unsafe-inline'` so no scripts or remote content ever load (FR-006). Remote images are
  blocked by default.
- **Rationale**: Zero build tooling, tiny payload, no JS dependency supply chain, fast first
  paint. The iframe+CSP combination is the standard defense for untrusted HTML without a
  sanitizer library.
- **Alternatives considered**: React/Vite (build step, hundreds of dependencies, large bundle);
  server-side sanitization library (dependency, weaker guarantees than sandbox+CSP).

## R8. Security and access model

- **Decision**: Public mailboxes (per spec assumption). Optional `PM_API_TOKEN`: when set, API
  and UI requests must be authenticated; SMTP is unaffected. Scripts send `Authorization:
  Bearer <token>`. Browsers cannot attach that header to `EventSource`, `<iframe src>`, or plain
  download links, so the UI posts the token once to `POST /api/v1/session`, which sets an
  `HttpOnly; SameSite=Strict` cookie (`Secure` when the request arrived over HTTPS, determined
  via a trusted proxy's `X-Forwarded-Proto`). The cookie value is `HMAC-SHA256(token, "session")`
  (stdlib `crypto/hmac`), so the raw token is never stored client-side, and comparisons use
  `subtle.ConstantTimeCompare`. SameSite=Strict covers CSRF for the delete endpoints. The token
  is never accepted in query strings and never logged. Failed attempts share the per-IP
  rate limit. Rate
  limits: token bucket per remote IP on SMTP connections/messages and per mailbox on ingest;
  HTTP requests per IP. All limits are configurable. Mailbox names validated to
  `[a-z0-9._-]{1,64}` after lowercasing and `+tag` stripping, which also prevents path
  traversal in the file store.
- **Rationale**: Matches maildrop.cc semantics while giving operators a simple lock when
  exposing an instance publicly; strict names make the storage layer safe.

## R9. Container image and Docker

- **Decision**: Multi-stage `Dockerfile`: `golang:1.23-alpine` builder (`-trimpath -ldflags
  "-s -w"`) to a `scratch` final stage containing the binary and CA certificates only; runs as
  a numeric non-root user; `VOLUME /data`; `EXPOSE 8080 2525`; `HEALTHCHECK` implemented by
  the binary itself (`phantom-mail healthcheck`, since scratch has no curl). Image target
  < 15 MB. `compose.yaml` maps `8080:8080` and `25:2525` (local default `2525:2525` to avoid
  privileged ports) and defines a `test` service that runs `go test ./...` in the builder image.
- **Rationale**: Smallest attack surface and footprint; one image for local and cloud
  (FR-023); no shell required. Non-root users cannot bind port 25, so the container listens on
  2525 and the host/platform maps port 25.
- **Alternatives considered**: distroless/alpine runtime (a few MB larger, adds a shell/libc
  surface we do not need).

## R10. Cloud deployment and port 25

- **Decision**: Document deployment as "any host that can run a container, expose TCP 25
  inbound, and an HTTPS front". Primary documented path: small VM or container host with a public
  IP, MX record pointing to a host name with an A record, HTTPS terminated by a reverse proxy
  or platform (e.g., Caddy or the platform's load balancer) in front of port 8080. Application
  itself speaks plain HTTP, keeping TLS (and its certificate-renewal dependency) out of the
  binary.
- **Rationale**: Many serverless/PaaS platforms only route HTTP; inbound SMTP requires raw TCP
  on port 25, so VM-style or TCP-capable container hosts are the realistic targets. Some
  clouds restrict *outbound* 25 but inbound is generally allowed; since we never send mail this
  is not an obstacle. Documented with troubleshooting steps (FR-024).
- **Alternatives considered**: Built-in ACME/autocert (requires `x/crypto`, adds renewal
  concerns); an HTTP-to-email webhook provider (not a faithful clone, external dependency,
  not locally testable).

## R11. Observability

- **Decision**: `log/slog` JSON handler to stdout; one log line per accepted/rejected message
  and per HTTP request (health checks excluded); `/healthz` reports status, uptime, message
  count and store bytes. A metrics endpoint is deferred. No metrics library.
- **Rationale**: Meets FR-018/FR-019 with zero dependencies; Prometheus text format can be
  added by hand later if needed.

## R12. Performance validation approach

- **Decision**: Go benchmarks in `tests/bench` for SMTP ingest throughput, list/get latency, and
  hub fan-out; a memory test that ingests a burst and asserts heap stays under a bound;
  `go test -race` in CI; image size check in the docs/e2e test. Goals from plan.md are encoded
  as assertions with generous margins to avoid flaky CI, plus documented benchmark commands for
  real measurements.
- **Rationale**: Principle I requires performance claims to be verified, not asserted.
