# Development

How to build, test and change Phantom Mail. For running it, see [Running locally](local.md).

## Layout

```text
cmd/phantom-mail/        main: config -> app.Run; the "healthcheck" subcommand
internal/
  app/                   wires everything together; start and graceful shutdown
  config/                PM_* environment variables, defaults, validation
  mailbox/               mailbox name normalization, domain matching
  message/               message model, time-ordered IDs, tolerant MIME parser
  store/                 Store interface and the shared index/limits/retention logic
    memory/ fs/          the two backends (RAM, one file per message)
    storetest/           the contract test suite every backend must pass
  inbox/                 store + hub: announces every change to subscribers
  hub/                   in-process publish/subscribe
  smtpd/                 receive-only SMTP server
  httpapi/               JSON API, SSE, sessions, health, OpenAPI
  web/                   embedded web interface (static/ has the vanilla JS and CSS)
  retention/             background sweep
  limits/                token-bucket rate limiter, trusted-proxy client address
  clock/                 real and fake clocks
tests/
  integration/           SMTP -> store -> HTTP through real listeners; contract checks
  e2e/                   builds and runs the real binary and container image
  bench/                 benchmarks and the performance gates
  docs/                  runs the documentation (see below)
docs/                    this documentation, with runnable examples under examples/
specs/                   the specification, plan, tasks and API contract
```

## Rules of the road

The project constitution (`.specify/memory/constitution.md`) sets the rules; in practice:

1. **Tests first.** Every behavior change comes with automated tests that fail before the change.
   Bug fixes get a regression test.
2. **Standard library only.** Do not add a third-party Go module. If you think you need one,
   record why the standard library is not enough in the pull request; `go.mod` has no `require`
   lines and a test setup keeps it that way.
3. **Local and cloud parity.** New state goes behind an interface and lives on the data
   volume; new settings are `PM_*` environment variables with a working default.
4. **Docs in the same change.** Behavior, configuration and API changes update `docs/` (and the
   OpenAPI contract) in the same commit. The docs tests below fail when they drift.

## Everyday commands

| Command | What it does |
|---------|--------------|
| `make test` | `gofmt` check, `go vet`, `go test -race ./...`, and the web interface tests |
| `docker compose run --rm test` | The same, plus `make docs-check`, inside Docker; needs only Docker |
| `make docs-check` | Runs the documentation tests (`tests/docs`) |
| `make bench` | Benchmarks (`tests/bench`) |
| `make build` | Static binary in `bin/phantom-mail` |
| `make run` | Run locally with defaults |
| `make docker` | Build the runtime image |
| `go test -short ./...` | Skips the slow performance gates |

The web interface logic is plain JavaScript modules tested with Node's built-in runner
(`node --test internal/web/static/*.test.mjs`); there are no npm packages and no build step.

### Tests you will meet

- **Contract suite** (`internal/store/storetest`) runs against both storage backends, so they
  behave identically.
- **Receive-only guard** (`tests/integration/receive_only_test.go`) parses the source and fails if
  production code imports `net/smtp` or dials out. The only allowed exception is the health check.
- **OpenAPI checks** (`tests/integration/openapi_test.go`) fail if the registered routes differ
  from `specs/001-disposable-inbox-service/contracts/openapi.yaml`, if the served copy
  (`internal/httpapi/openapi.yaml`) differs from it, or if responses lack required fields. When
  you change the API, edit the contract and copy it:
  `cp specs/001-disposable-inbox-service/contracts/openapi.yaml internal/httpapi/openapi.yaml`.
- **Performance gates** (`tests/bench`) assert the plan's numbers and print the measurements:
  ingest rate (at least 100 messages/s with fsynced writes on one CPU), list and fetch latency
  (p95 under 10 ms and 20 ms), memory and list latency with 50 active mailboxes (heap under
  64 MiB, list p95 under 50 ms), startup with 10,000 messages (under 1 s). `make bench` runs them
  strictly (`PM_PERF_STRICT=1`, alone on the machine), then the benchmarks. In an ordinary
  `go test ./...` they run with limits relaxed 5x (ingest floor 20 messages/s) because other
  packages compete for CPU and disk; they then only catch large regressions.
- **Documentation tests** (`tests/docs`) execute every code block tagged `bash verify` against a
  freshly started instance, check that every `PM_*` setting and every API route is documented
  (and that nothing documented is missing from the code), and check that links and anchors
  resolve.
- **Container test** (`tests/e2e/container_test.go`) builds the image and checks its size, user,
  health status, idle memory and a mail round trip. It is skipped without Docker or with
  `PM_SKIP_DOCKER_TESTS=1`.

## How to add things

### A setting

1. Add the field and parsing to `internal/config/config.go`, with a default and validation.
2. Cover it in `internal/config/config_test.go` (defaults, a valid value, an invalid value).
3. Use it where needed (usually `internal/app/app.go`).
4. Document it in `docs/configuration.md`: a row in the quick-reference table **and** a
   `###` section titled with the variable's name.
   `make docs-check` fails otherwise.

### An API endpoint

1. Write tests in `internal/httpapi/` first.
2. Register the handler in `Server.routes` (`internal/httpapi/server.go`).
3. Add the path to the OpenAPI contract and copy it to `internal/httpapi/openapi.yaml`.
4. Describe it in `docs/api.md`, including errors.

### A storage backend

Implement `store.Blobs` (four methods) and call `store.NewCore`; then run
`storetest.Run` against it in a test. Limits, ordering, expiry and eviction come from `Core`.

## Design notes

- **Why a file per message?** It keeps memory small (only a summary index is resident), needs no
  database, works on any volume, and makes expiry a file delete. Writes are
  write-temp, fsync, rename, fsync-directory, so a crash never leaves a half-written message.
- **Why no dependencies?** Fewer things to patch and audit, a tiny image, and a fast build. The
  SMTP server (about 400 lines) and MIME parser are written for this narrow job, and the parser's
  tests feed it damaged and malformed messages. `tests/docs` fails if `go.mod` ever gains a
  `require` line.
- **Why SSE and long polling?** One-way updates need no library on either side, pass through
  proxies, and are scriptable with `curl`.
- **Untrusted content.** Email is rendered only inside a sandboxed frame with a restrictive
  Content-Security-Policy; attachments are always downloads.
