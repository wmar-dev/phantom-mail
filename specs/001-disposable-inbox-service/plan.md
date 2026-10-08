# Implementation Plan: Disposable Inbox Service

**Branch**: `001-disposable-inbox-service` | **Date**: 2026-10-07 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-disposable-inbox-service/spec.md`

## Summary

A receive-only disposable mail service: an SMTP listener accepts mail for any mailbox at
configured domains, a file-backed store keeps messages for a limited time, and a single HTTP
server provides a JSON API (list, read, wait-for, delete, attachments, live events) and a
small web interface. Per the user's direction, the design prioritizes **performance and low
footprint**: one statically linked Go binary, standard library only, bounded memory by
construction, a tiny scratch-based container image, and a zero-build vanilla web UI embedded in
the binary. The same image runs locally (Docker Compose or plain binary) and in the cloud
with environment-only configuration. See [research.md](research.md) for the decision log.

## Technical Context

**Language/Version**: Go 1.23+ (single static binary, `CGO_ENABLED=0`)

**Primary Dependencies**: None beyond the Go standard library (SMTP server, MIME parsing, HTTP, SSE, embedded assets, structured logging via `log/slog`). The only external tool is Docker for the container build and the containerized test run.

**Storage**: One file per message on a mountable data directory (`PM_DATA_DIR`) with an in-memory metadata index rebuilt on startup. Hidden behind a `Store` interface with an in-memory implementation used by tests.

**Testing**: `go test` (unit, integration with real SMTP client from stdlib and `httptest`, end-to-end against the built binary), `go test -race`, benchmarks for the hot paths; docs verification test that executes documented commands/examples. Run via `make test` (local Go) or `docker compose run --rm test` (only Docker required). Web UI logic is tested with Node's built-in test runner (`node --test`, no npm packages). Test-time tools (Go toolchain, Node, Docker) are permitted; they never enter the runtime image or `go.mod`, and the Docker `test` service bundles them so only Docker is needed.

**Target Platform**: Linux container (amd64/arm64), also runnable natively on macOS/Linux for development.

**Project Type**: web-service (single process: SMTP + HTTP API + static web UI)

**Performance Goals**: Ingest ≥ 100 messages/s sustained with durable (fsynced) writes on one vCPU, measured but not gated above that (storage speed varies widely, so a higher rate is a stretch goal, not a requirement); message visible to list/wait/SSE within 1 s of SMTP `DATA` completion (p95); mailbox list p95 < 10 ms and message fetch p95 < 20 ms at 100 messages per mailbox; cold start to ready < 1 s with 10,000 stored messages.

**Constraints**: Idle resident memory < 20 MB and < 64 MB under sustained load at 50 active mailboxes; container image < 15 MB; web UI initial payload < 30 KB uncompressed with no third-party requests; hard memory bounds from message-size cap, per-mailbox count cap, and total-store byte cap (oldest evicted first); no outbound mail ever. Runtime image and `go.mod` stay free of Node and third-party modules. Security and operability rules: HTML bodies render only in a sandboxed frame (no `allow-same-origin`) behind a strict CSP; attachments are served as downloads with `X-Content-Type-Options: nosniff`; client IPs come from `X-Forwarded-For` only when the peer is in `PM_TRUSTED_PROXIES`; the SMTP server answers `452` (temporary failure) when storage is full; mailbox names are not scoped by domain.

**Scale/Scope**: Single instance, up to ~10,000 stored messages / 1 GB on disk by default, 50+ simultaneously active mailboxes (SC-007).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] I. Well Tested: Test-first for each user story; unit tests for parsing, mailbox normalization, limits, retention; integration tests drive real SMTP and HTTP; e2e test against the built binary and container; race detector and benchmarks guard performance goals.
- [x] II. Minimal Dependencies: Zero third-party Go modules. SMTP server is ~400 lines on `net`/`bufio`; MIME via `net/mail`, `mime`, `mime/multipart`; web UI is vanilla JS/CSS embedded with `go:embed`. Justification in research.md (R1, R2, R3).
- [x] III. Cloud Friendly: Stateless process plus a data volume behind a `Store` interface; all config from `PM_*` environment variables; JSON logs to stdout; `/healthz`; graceful SIGTERM drain; non-root user; no vendor-specific code.
- [x] IV. Local Testing Friendly: `docker compose up` runs everything with no account or network; `make test` / `docker compose run --rm test` run the full suite offline; SMTP and HTTP are tested over loopback; in-memory store and fake clock for deterministic tests. Go (1.27.1 verified locally) runs the suite natively; the Docker path remains the documented zero-install route.
- [x] V. Thoroughly Documented: `docs/` tree planned (see Project Structure); OpenAPI served by the app and checked against routes in tests; docs-verification test executes documented commands/examples.

**Post-design re-check**: All gates still pass; no Complexity Tracking entries required.

## Project Structure

### Documentation (this feature)

```text
specs/001-disposable-inbox-service/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/
│   └── openapi.yaml     # Phase 1 output: HTTP API contract
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
cmd/
└── phantom-mail/
    ├── main.go              # config -> app.Run, signal handling; "healthcheck" subcommand
    └── healthcheck.go       # probe of /healthz for the container HEALTHCHECK

internal/
├── app/                     # wiring of store, hub, SMTP, HTTP, janitor; graceful shutdown
├── clock/                   # real and fake clocks
├── config/                  # PM_* env parsing, defaults, validation
├── mailbox/                 # name normalization (case, plus-addressing), domain matching
├── message/                 # Message/Attachment types, IDs, tolerant MIME parsing
├── store/                   # Store interface + shared Core (index, limits, retention)
│   ├── fs/                  # volume-backed blobs: one .eml file per message
│   ├── memory/              # in-memory blobs
│   └── storetest/           # backend-agnostic contract test suite
├── inbox/                   # store + hub: announces every change to subscribers
├── hub/                     # in-process pub/sub powering wait and SSE
├── smtpd/                   # minimal receive-only SMTP server (no relay, no AUTH, no STARTTLS in v1)
├── retention/               # background sweep: expiry + total-size cap
├── limits/                  # per-IP / per-mailbox rate limiting (token bucket) + trusted-proxy client-IP resolution (PM_TRUSTED_PROXIES)
├── httpapi/                 # REST handlers, SSE, sessions, health, OpenAPI serving; attachments served as downloads with nosniff
└── web/                     # embedded static UI
    └── static/              # index.html, app.js, lib.js, signin.js, app.css, plus *.test.mjs (not served)

tests/
├── integration/             # real SMTP client -> store -> HTTP client; receive-only guard; OpenAPI contract
├── e2e/                     # runs the built binary / container
├── docs/                    # executes "bash verify" examples; checks settings, routes, links
└── bench/                   # ingest, latency, memory and startup gates plus benchmarks

docs/
├── index.md                 # overview + quick start
├── local.md                 # native and Docker Compose setup
├── configuration.md         # every PM_* setting, default, effect
├── api.md                   # API reference with examples and errors
├── web-ui.md                # web interface guide
├── deployment.md            # cloud deployment, DNS (MX/A), TLS via reverse proxy (SSE buffering off), port 25 notes, troubleshooting
├── testing-verification-flows.md   # end-to-end example of a code-verification sign-up test
├── development.md           # running tests, benchmarks, contributing
└── examples/
    ├── Caddyfile            # example HTTPS reverse proxy in front of port 8080
    ├── compose.prod.yaml    # single-host production layout (app + Caddy)
    └── verification-code/   # Go program that waits for an email and prints the code

README.md                    # short intro and quick start, links to docs/index.md
.github/workflows/ci.yml     # runs `make test` and `make docs-check` (same commands as local)
.dockerignore
Dockerfile                   # targets: build, runtime (scratch), test (toolchain for compose)
compose.yaml                 # app + `test` service profile
Makefile                     # build, test, bench, docker, docs-check
```

**Structure Decision**: A single Go module with one binary. The SMTP listener, HTTP server, and
UI share one process and one in-memory index, which avoids inter-service communication, keeps
the footprint minimal, and makes local and cloud runs identical. `internal/` packages are small
and interface-separated (store, clock, hub) so tests need no network beyond loopback.

## Complexity Tracking

No constitution violations; no entries required.
