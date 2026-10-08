---

description: "Task list for the Disposable Inbox Service"
---

# Tasks: Disposable Inbox Service

**Input**: Design documents from `/specs/001-disposable-inbox-service/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/openapi.yaml, quickstart.md

**Tests**: MANDATORY per the constitution (Principle I). Within every phase, write the test tasks first and confirm they FAIL before starting the implementation tasks. Documentation tasks are also mandatory (Principle V).

**Organization**: Tasks are grouped by user story. Foundational work (ingest pipeline) blocks all stories.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story the task belongs to (US1-US5)
- Paths are relative to the repository root. Go module name: `phantom-mail`. Standard library only (Principle II): do not add any third-party module.

## Phase 1: Setup (Shared Infrastructure)

- [X] T001 Initialize the Go module (`go mod init phantom-mail`, `go 1.23`) in go.mod and create the directory skeleton from plan.md (cmd/phantom-mail/, internal/{config,mailbox,message,store,smtpd,hub,retention,limits,httpapi,web,clock}/, tests/{integration,e2e,docs,bench}/, docs/)
- [X] T002 [P] Create .gitignore (Go build output, `bin/`, `data/`, `*.test`, `.DS_Store`)
- [X] T003 [P] Create Makefile with targets `build`, `run`, `test` (go vet + `go test -race ./...` + web tests), `bench`, `docs-check` (passes vacuously until tests/docs contains tests, so early CI runs are green), `docker`, and `lint` (`gofmt -l`, `go vet`)
- [X] T004 [P] Create CI workflow in .github/workflows/ci.yml that runs `make test` and `make docs-check` (same commands developers run locally)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The ingest pipeline: SMTP in, parsed message stored, subscribers notified. No story can be demonstrated until this works.

**CRITICAL**: No user story work can begin until this phase is complete.

### Tests for Foundational (write first, must FAIL)

- [X] T005 [P] Config tests (defaults, every `PM_*` variable, invalid values rejected with clear errors) in internal/config/config_test.go
- [X] T006 [P] Mailbox/domain tests (lowercasing, `+tag` stripping, `^[a-z0-9._-]{1,64}$` validation, path-traversal inputs like `../x`, case-insensitive domain match) in internal/mailbox/mailbox_test.go
- [X] T007 [P] MIME parse tests with fixtures in internal/message/testdata/*.eml: text-only, HTML-only, multipart/alternative, attachments, no subject, quoted-printable, base64, ISO-8859-1, Windows-1252, unknown charset fallback, malformed headers, in internal/message/parse_test.go
- [X] T008 [P] Reusable Store contract test suite (put/get/list newest-first/delete/empty mailbox/per-mailbox cap eviction/total-bytes eviction/mailbox-count cap refuses a new mailbox but still accepts mail for existing ones/empty mailbox directory removed after its last message is deleted, expired, or evicted/expiry filtering with fake clock) in internal/store/storetest/suite.go, invoked from internal/store/memory/memory_test.go and internal/store/fs/fs_test.go (fs adds: restart rebuilds index, corrupt file ignored, atomic write leaves no partial message)
- [X] T009 [P] Hub tests (subscribe/publish, multiple subscribers each receive, slow subscriber dropped without blocking publisher, unsubscribe frees resources, no goroutine leaks) in internal/hub/hub_test.go
- [X] T010 [P] Rate limiter and client-IP tests (token bucket per key, refill with fake clock, burst, idle key cleanup; trusted-proxy resolution: `X-Forwarded-For` honored only when the peer is in `PM_TRUSTED_PROXIES`, ignored otherwise, spoofed leftmost entries not trusted) in internal/limits/limits_test.go and internal/limits/proxy_test.go
- [X] T011 [P] SMTP server tests using stdlib `net/smtp` client over loopback: accepted domain stored, unknown domain `550`, invalid mailbox `550`, oversize `552`, plus-address stored in base mailbox with original recipient kept, multiple recipients, RSET/NOOP/QUIT, no AUTH or relay offered, read timeouts, store full yields `452`, rate limit yields `421`/`451`, in internal/smtpd/smtpd_test.go
- [X] T012 [P] Receive-only guard test (FR-020) using stdlib `go/parser`: fail if any non-test Go file under cmd/ or internal/ imports `net/smtp`, or calls `net.Dial`, `net.DialTimeout`, or `(*net.Dialer).Dial*` outside an allowlist (cmd/phantom-mail/healthcheck.go); plus a behavior check that a server given mail for an unserved domain rejects it with `550` and never advertises AUTH or relay, in tests/integration/receive_only_test.go
- [X] T013 Integration test: SMTP delivery through the fs store with notification via hub, verifying persisted file and hub event, in tests/integration/ingest_test.go

### Implementation for Foundational

- [X] T014 [P] Implement injectable clock (`Clock` interface, real and fake) in internal/clock/clock.go
- [X] T015 [P] Implement config loading from `PM_*` environment variables with defaults from data-model.md and plan.md (`PM_DOMAINS`, `PM_HTTP_ADDR`, `PM_SMTP_ADDR`, `PM_DATA_DIR`, `PM_STORAGE`, `PM_RETENTION`, `PM_MAX_MESSAGES_PER_MAILBOX`, `PM_MAX_MAILBOXES`, `PM_MAX_MESSAGE_BYTES`, `PM_MAX_TOTAL_BYTES`, rate-limit settings, `PM_API_TOKEN`, `PM_TRUSTED_PROXIES`, `PM_LOG_LEVEL`) in internal/config/config.go
- [X] T016 [P] Implement mailbox name normalization and domain matching in internal/mailbox/mailbox.go
- [X] T017 [P] Implement Message/Attachment types and time-ordered ID generation (48-bit ms timestamp + 80 random bits, hex) in internal/message/message.go
- [X] T018 Implement MIME parsing (text/HTML bodies, attachments with sanitized filenames, hand-mapped UTF-8/ASCII/ISO-8859-1/Windows-1252 charsets with lossy fallback, tolerant of malformed input) in internal/message/parse.go
- [X] T019 [P] Define the `Store` interface and implement the in-memory store in internal/store/store.go and internal/store/memory/memory.go
- [X] T020 Implement the file-backed store (`<data>/<mailbox>/<id>.eml`, temp-write + fsync + rename, in-memory metadata index rebuilt at startup from file names, sizes, and header blocks only, attachment count cached on first listing, bodies read on demand, per-mailbox and total-bytes eviction, expired messages filtered on read, empty mailbox directories removed with their last message, `PM_MAX_MAILBOXES` enforced) in internal/store/fs/fs.go
- [X] T021 [P] Implement the in-process pub/sub hub in internal/hub/hub.go
- [X] T022 [P] Implement the token-bucket rate limiter in internal/limits/limits.go and trusted-proxy client-IP resolution in internal/limits/proxy.go
- [X] T023 Implement the receive-only SMTP server (EHLO/HELO, MAIL, RCPT, DATA, RSET, NOOP, QUIT, `SIZE`, streaming read with hard size cap, timeouts, domain and mailbox validation, rate limiting, `452` on store failure, no relay/AUTH) in internal/smtpd/smtpd.go
- [X] T024 Wire config, store, hub, limiter, and SMTP server with `log/slog` JSON logging to stdout and graceful SIGTERM/SIGINT shutdown in cmd/phantom-mail/main.go

**Checkpoint**: Mail sent over SMTP is persisted and published. T013 passes.

---

## Phase 3: User Story 1 - Receive and read a verification email in the web interface (Priority: P1) MVP

**Goal**: A tester types a mailbox name in the browser and reads incoming messages, live.

**Independent Test**: Send an email to a never-used address, open the web UI, enter the mailbox name, confirm the message is listed, openable, and that a second email appears without reloading.

### Tests for User Story 1 (MANDATORY, write first)

- [X] T025 [P] [US1] HTTP handler tests for `GET /api/v1/mailboxes/{mailbox}/messages` (empty mailbox returns `[]`, newest first, limit, invalid name `400` with error schema) and `GET .../messages/{id}` (full body, `404`) in internal/httpapi/messages_test.go
- [X] T026 [P] [US1] Handler tests for `GET .../messages/{id}/html` asserting the CSP header (`default-src 'none'`, no scripts, no remote content), `X-Content-Type-Options: nosniff`, and HTML-only/text-only handling in internal/httpapi/html_test.go
- [X] T027 [P] [US1] SSE tests (`message` event on ingest, heartbeat comment, client disconnect releases subscription) in internal/httpapi/events_test.go
- [X] T028 [P] [US1] Web UI logic tests with the Node built-in runner (`node --test`) for pure functions: mailbox name normalization, list rendering model, SSE event merge/dedupe, sandbox iframe attribute construction, in internal/web/static/app.test.mjs
- [X] T029 [US1] End-to-end test: start the server on loopback ports, deliver mail with `net/smtp`, fetch static UI, list and get via HTTP, receive the SSE event, asserting the message is visible in the list and delivered as an SSE event within 1 s of SMTP completion (stricter than SC-001's 5 s, to avoid flakiness), in tests/e2e/us1_web_test.go

### Implementation for User Story 1

- [X] T030 [P] [US1] Implement shared HTTP plumbing (router with Go 1.22 pattern routes, JSON error helper using the contract's error schema, request logging excluding `/healthz`, mailbox parameter validation) in internal/httpapi/server.go
- [X] T031 [US1] Implement list and get message handlers in internal/httpapi/messages.go
- [X] T032 [US1] Implement the sandboxed HTML endpoint (strict CSP, nosniff, no cookies) in internal/httpapi/html.go
- [X] T033 [US1] Implement the per-mailbox SSE endpoint (`message` and `deleted` events, 15 s heartbeat) in internal/httpapi/events.go
- [X] T034 [US1] Implement `/healthz` (status, uptime, message count, store bytes) in internal/httpapi/health.go
- [X] T035 [P] [US1] Build the vanilla web UI (mailbox input, message list, message view with `<iframe sandbox>` without `allow-same-origin`, `EventSource` live updates, delete buttons wired to the delete endpoints once US2 lands, no external requests, payload < 30 KB) in internal/web/static/index.html, internal/web/static/app.js, internal/web/static/app.css
- [X] T036 [US1] Embed static assets with `go:embed`, serve with gzip and cache headers, and mount the UI and API on the HTTP server in internal/web/web.go and cmd/phantom-mail/main.go
- [X] T037 [P] [US1] Write the web interface guide in docs/web-ui.md

**Checkpoint**: User Story 1 is fully functional and testable on its own (T029 passes).

---

## Phase 4: User Story 2 - Retrieve messages and verification codes through an API (Priority: P1)

**Goal**: Scripts can wait for, read, and delete mail and download attachments over a documented API.

**Independent Test**: Deliver an email, then via HTTP only: wait for it, fetch it, download an attachment, delete it, and confirm errors for bad input.

### Tests for User Story 2 (MANDATORY, write first)

- [X] T038 [P] [US2] Tests for `GET .../messages/wait` (returns existing messages immediately when `after` omitted, holds until arrival, `204` on timeout, `after` filtering, timeout bounds 1-60, multiple concurrent waiters all released) in internal/httpapi/wait_test.go
- [X] T039 [P] [US2] Tests for `DELETE .../messages/{id}` (`204`, then `404`) and `DELETE .../messages` (`204`, idempotent), including `deleted` SSE event, in internal/httpapi/delete_test.go
- [X] T040 [P] [US2] Tests for attachment download (correct bytes, `Content-Disposition: attachment`, `nosniff`, `404` for bad index) in internal/httpapi/attachments_test.go
- [X] T041 [P] [US2] Tests for optional `PM_API_TOKEN` auth (`401` without/with wrong bearer; `POST /api/v1/session` sets an `HttpOnly; SameSite=Strict` cookie that then authorizes list, SSE, HTML, and attachment requests; wrong token `401` and repeated failures `429`; `DELETE /api/v1/session` signs out; token in a query string is rejected; token never appears in logs; `Secure` flag set behind a trusted HTTPS proxy; `/healthz`, `/openapi.yaml`, and SMTP stay open), plus UI sign-in logic tests with `node --test` (a 401 shows the token prompt; the token is never put in a URL or kept in `localStorage`) in internal/web/static/signin.test.mjs and HTTP per-IP rate limiting with trusted-proxy handling, in internal/httpapi/auth_test.go
- [X] T042 [P] [US2] Contract test: every path and method in contracts/openapi.yaml has a registered route and vice versa, and `/openapi.yaml` is served and equals the contract, in tests/integration/openapi_test.go
- [X] T043 [US2] End-to-end scripted flow (send, wait, extract six-digit code, delete) mirroring quickstart.md, with no manual steps (SC-003), and asserting the full send-to-code-extracted path completes within 2 s on loopback (well inside SC-002's 30 s), in tests/e2e/us2_api_test.go

### Implementation for User Story 2

- [X] T044 [US2] Implement the long-poll wait handler on top of the hub in internal/httpapi/wait.go
- [X] T045 [P] [US2] Implement delete-message and empty-mailbox handlers in internal/httpapi/delete.go
- [X] T046 [P] [US2] Implement attachment download handler in internal/httpapi/attachments.go
- [X] T047 [P] [US2] Implement bearer-token and session-cookie middleware (cookie = HMAC-SHA256 of the token, constant-time comparison, `POST`/`DELETE /api/v1/session` handlers) and HTTP per-IP rate limiting including failed sign-in attempts (using client-IP resolution from internal/limits/proxy.go, T022) in internal/httpapi/auth.go
- [X] T048 [US2] Embed contracts/openapi.yaml (copy into internal/httpapi/openapi.yaml with a test asserting equality to the source contract) and serve it at `/openapi.yaml` in internal/httpapi/openapi.go
- [X] T049 [US2] Hook the UI delete buttons to the delete endpoints in internal/web/static/app.js, and add the token sign-in prompt (shown only when the server answers 401, posts to `POST /api/v1/session`, FR-027) in internal/web/static/signin.js
- [X] T050 [P] [US2] Write the API reference with request/response examples, error cases, long-poll, SSE, and auth (bearer header for scripts, session cookie for browsers) in docs/api.md, and add the sign-in section to docs/web-ui.md

**Checkpoint**: User Stories 1 and 2 both work independently.

---

## Phase 5: User Story 3 - Run the whole service locally (Priority: P2)

**Goal**: One command starts everything on a laptop with no cloud and no network; the whole suite runs offline, natively and in Docker.

**Independent Test**: On a clean checkout with the network disabled, run the documented start command, deliver a test email, read it via web and API; run the test command and see it pass.

### Tests for User Story 3 (MANDATORY, write first)

- [X] T051 [P] [US3] Test for the `healthcheck` subcommand (exit 0 when `/healthz` is ok, non-zero otherwise) in cmd/phantom-mail/healthcheck_test.go
- [X] T052 [P] [US3] E2E test that builds the binary, starts it with only environment variables on random ports, delivers mail with `net/smtp`, and reads it back, with no outbound network, asserting cold start to a delivered and readable message takes under 30 s (the automatable part of SC-004; the 5-minute figure also covers reading the docs and pulling images), in tests/e2e/us3_local_test.go
- [X] T053 [US3] Container test script (skipped when Docker is unavailable) that builds the image, asserts size < 15 MB, non-root user, health status `healthy`, and mail round-trip, in tests/e2e/container_test.go

### Implementation for User Story 3

- [X] T054 [P] [US3] Implement `phantom-mail healthcheck` subcommand (needed because the runtime image has no shell or curl) in cmd/phantom-mail/healthcheck.go
- [X] T055 [P] [US3] Create multi-stage Dockerfile (golang builder with `CGO_ENABLED=0 -trimpath -ldflags "-s -w"` to `scratch`, numeric non-root user, `VOLUME /data`, `EXPOSE 8080 2525`, `HEALTHCHECK`) in Dockerfile
- [X] T056 [US3] Create compose.yaml (app service with `8080:8080` and `2525:2525`, named volume for `/data`, env defaults, and a `test` service that runs the full suite in a builder image containing Go, Node, `curl`, and `jq`, which the docs-verification test needs) in compose.yaml
- [X] T057 [P] [US3] Add a `.dockerignore` and wire `make run`, `make docker`, and `make test` to the documented commands in .dockerignore and Makefile
- [X] T058 [P] [US3] Write local setup guide (native and Docker Compose, sending a test email with `curl`, running tests offline) in docs/local.md

**Checkpoint**: User Story 3 passes; the quickstart sections 1-3 and 6 run as written.

---

## Phase 6: User Story 4 - Serve the service on a public domain in the cloud (Priority: P2)

**Goal**: The same image, configured only by environment, receives real mail on the owner's domain and survives restarts.

**Independent Test**: Restart the container with a volume and confirm stored mail is still readable; SIGTERM drains cleanly; deployment guide steps are followed on a real host.

### Tests for User Story 4 (MANDATORY, write first)

- [X] T059 [P] [US4] Persistence test: ingest messages, stop the server, start a new instance on the same data dir, messages are still listed, in tests/integration/restart_test.go
- [X] T060 [P] [US4] Graceful shutdown test: on SIGTERM, in-flight SMTP `DATA` completes and is persisted, new connections are refused, process exits 0 within the timeout, in tests/e2e/shutdown_test.go
- [X] T061 [P] [US4] Config-only deployment test: unset/blank environment yields local defaults; a production-style environment (`PM_DOMAINS=mail.example.com`, custom ports, token) is honored with no code changes, in internal/config/deploy_test.go
- [X] T062 [P] [US4] Test that structured JSON logs are written to stdout (one parseable JSON object per line, secrets such as `PM_API_TOKEN` never logged), in cmd/phantom-mail/logging_test.go

### Implementation for User Story 4

- [X] T063 [US4] Implement shutdown sequencing (stop accepting SMTP, drain in-flight, close SSE streams, flush index, HTTP shutdown with timeout) in cmd/phantom-mail/main.go
- [X] T064 [P] [US4] Write the cloud deployment guide (host requirements: container host with inbound TCP 25, DNS `MX` and `A` records, HTTPS via reverse proxy or platform load balancer with SSE buffering disabled, volume for `/data`, security notes for public mailboxes and `PM_API_TOKEN`, port-25 restrictions, troubleshooting table, and a manual end-to-end verification checklist for SC-006: sign up on an external site with an address at the domain, confirm the email appears in the web interface, record the elapsed time) in docs/deployment.md
- [X] T065 [P] [US4] Provide an example reverse-proxy configuration for HTTPS in front of port 8080 with SSE-friendly settings in docs/examples/Caddyfile and reference it from docs/deployment.md

**Checkpoint**: Quickstart section 8 can be followed on a real host.

---

## Phase 7: User Story 5 - Mailboxes clean themselves up (Priority: P3)

**Goal**: Old messages disappear on schedule and storage stays bounded.

**Independent Test**: With a fake clock, ingest, advance past retention, and confirm the message is unavailable via every read path.

### Tests for User Story 5 (MANDATORY, write first)

- [X] T066 [P] [US5] Janitor tests with the fake clock: expiry deletes files, per-mailbox cap evicts oldest, total-bytes cap evicts oldest across mailboxes, reads never return expired messages between ticks, in internal/retention/retention_test.go
- [X] T067 [P] [US5] Integration test across SMTP, store, and HTTP: message past retention returns `404` and disappears from list, wait, and SSE, in tests/integration/retention_test.go

### Implementation for User Story 5

- [X] T068 [US5] Implement the janitor goroutine (30 s tick, configurable, stops on shutdown) and its store hooks in internal/retention/retention.go
- [X] T069 [US5] Start the janitor from main and emit `deleted` hub events for evicted messages in cmd/phantom-mail/main.go
- [X] T070 [P] [US5] Document retention and limit settings in docs/configuration.md (retention, per-mailbox cap, message size, total bytes)

**Checkpoint**: All five user stories are independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Performance proof, documentation completeness, and docs verification.

- [X] T071 [P] Ingest throughput benchmark (gate: at least 100 messages/s with fsynced writes on one vCPU; report the measured rate above that without failing) and list/get latency benchmarks (p95 under 10 ms / 20 ms at 100 messages) in tests/bench/bench_test.go
- [X] T072 [P] Memory bound test: burst-ingest and 50 active mailboxes with SSE subscribers keeps heap under the plan's bound with zero lost messages and list requests under 50 ms at p95 (SC-007), and idle RSS stays under 20 MB in the container, in tests/bench/memory_test.go
- [X] T073 [P] Startup test: 10,000 stored messages rebuild the index and become ready in under 1 s in tests/bench/startup_test.go
- [X] T074 [P] Write docs/index.md (overview, quick start, doc map)
- [X] T075 [P] Write the complete configuration reference (every `PM_*` setting, default, effect, example, including `PM_STORAGE=memory`: ephemeral mode where nothing is persisted, for demos and tests) in docs/configuration.md, including the deliberate choice that mailbox names are not scoped by domain
- [X] T076 [P] Write the end-to-end verification-code testing guide (unique mailbox per run, wait, extract code, examples in curl and Go) in docs/testing-verification-flows.md
- [X] T077 [P] Write development docs (running tests and benchmarks natively and in Docker, repo layout, dependency policy, adding a setting or endpoint with its docs) in docs/development.md
- [X] T078 Docs verification test: extract fenced `bash` blocks tagged for verification from docs/ and quickstart.md, run them against a started instance, and assert documented settings and endpoints exist in config and openapi.yaml, in tests/docs/docs_test.go
- [X] T079 [P] Add a README.md linking docs/index.md with the three-command quick start
- [X] T080 Security hardening review: path traversal in the fs store, header injection in `Content-Disposition`, SSE connection limits per IP, SMTP connection and command limits (outbound mail paths are guarded by the automated test T012), in internal/ (as findings dictate)
- [X] T081 Run `go vet`, `gofmt -l`, `go test -race ./...`, `make bench`, `make docs-check`, and the full quickstart.md validation; fix any failure

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup. BLOCKS all user stories.
- **US1 (Phase 3) and US2 (Phase 4)**: Depend on Foundational. US2 reuses the HTTP plumbing from T030-T034 in US1, so run US1 first or share T030 early. Both are P1; together they are the real MVP.
- **US3 (Phase 5)**: Depends on Foundational; needs US1's `/healthz` (T034) for the health check.
- **US4 (Phase 6)**: Depends on Foundational; best after US3 (reuses the image).
- **US5 (Phase 7)**: Depends on Foundational; independent of US1-US4 apart from sharing `main.go` wiring.
- **Polish (Phase 8)**: Depends on the stories being complete; the docs-verification test (T078) requires the other docs.

### Within Each Phase

- Tests are written first and must FAIL before implementation.
- Models and types before stores, stores before servers, servers before UI.
- Tasks touching the same file (for example cmd/phantom-mail/main.go: T024, T036, T063, T069) are not parallel.

### Parallel Opportunities

- Setup T002-T004 in parallel.
- Foundational tests T005-T011 in parallel; implementations T014-T017, T019, T021, T022 in parallel.
- US1 tests T025-T028 in parallel; US2 tests T038-T042 in parallel; US2 implementations T045-T047 in parallel.
- Documentation tasks T074-T077 in parallel.
- US3, US4, and US5 can proceed in parallel after Foundational if staffed.

### Parallel Example: Foundational Tests

```text
Task: "Config tests in internal/config/config_test.go"
Task: "Mailbox tests in internal/mailbox/mailbox_test.go"
Task: "MIME parse tests in internal/message/parse_test.go"
Task: "Store contract suite in internal/store/storetest/suite.go"
Task: "Hub tests in internal/hub/hub_test.go"
Task: "SMTP server tests in internal/smtpd/smtpd_test.go"
```

---

## Implementation Strategy

### MVP First

1. Phase 1 Setup, Phase 2 Foundational (validate with T013).
2. Phase 3 US1 (web UI) and Phase 4 US2 (API): both are P1 and together deliver the stated purpose (testing verification-code sign-ups by hand or by script).
3. Stop and validate with quickstart sections 1-5.

### Incremental Delivery

1. Add US3 (Docker and one-command local run), validate offline.
2. Add US4 (cloud-ready: restarts, shutdown, deployment guide), then deploy for real mail.
3. Add US5 (retention). Until then, `PM_MAX_TOTAL_BYTES` and the per-mailbox cap in the store (T020) still bound storage.
4. Finish with Phase 8: benchmarks, remaining docs, docs verification.

### Notes

- Keep the dependency list empty: `go.mod` must have no `require` lines at any point (verify in T081).
- Commit after each task or logical group; each checkpoint is a good commit boundary.
- Stop at any checkpoint to validate the story independently.
