# Running locally

Everything runs on your own machine with no cloud account, no credentials and no internet access
(after the first image or toolchain download). Pick one of the two ways below.

| | Needs | One command |
|-|-------|-------------|
| **Docker** | Docker with Compose | `docker compose up --build` |
| **Native** | Go 1.23 or newer | `make run` |

Both start the web interface on <http://localhost:8080>, the API under
<http://localhost:8080/api/v1>, and the mail receiver on `localhost:2525`. The default mail domain
is `localhost`, so the address for mailbox `alice` is `alice@localhost`.

## With Docker

```bash
docker compose up --build
```

- Data is kept in the named volume `phantom-mail-data`, so mail survives restarts. Remove it with
  `docker compose down -v`.
- Change settings by exporting variables before starting, for example
  `PM_RETENTION=1h docker compose up`. See [configuration](configuration.md) for every setting.
- The container image is tiny (about 3 MB), has no shell, runs as a non-root user, and is the
  same image you [deploy to the cloud](deployment.md).

## Natively

```bash
make run
```

This runs `go run ./cmd/phantom-mail` with the defaults; messages go in `./data`. To build a
binary instead: `make build`, then `./bin/phantom-mail`. Stop with `Ctrl+C` (the server finishes
any message it is receiving first).

```bash
PM_HTTP_ADDR=127.0.0.1:9000 PM_RETENTION=1h make run     # settings are environment variables
```

## Send a test email

Any SMTP client works. With `curl`:

```bash verify
curl --url smtp://localhost:2525 \
  --mail-from sender@example.com --mail-rcpt alice@localhost \
  -T - <<'EOF'
From: Sender <sender@example.com>
To: alice@localhost
Subject: Your verification code

Your code is 482913
EOF
```

Then open <http://localhost:8080/#/alice>, or read it over the API:

```bash verify
curl -s http://localhost:8080/api/v1/mailboxes/alice/messages
```

To point a system under test at your local instance, set its SMTP server to `localhost:2525`
(from inside another container use `host.docker.internal:2525`) and use addresses like
`anything@localhost`. Mail for any other domain is refused with `550 Relay access denied`, which
is how the service guarantees it can never be used to send spam.

## Run the tests

The whole suite runs offline, because everything it talks to is a loopback listener inside the
test.

```bash
make test               # go vet, gofmt check, go test -race, and the web UI tests
docker compose run --rm test    # the same, inside Docker; only Docker is required
make bench              # performance benchmarks
```

`make test` needs Go and, for the web interface tests, Node 20 or newer (skipped with a notice if
Node is missing). The Docker route bundles both. The container image test
(`tests/e2e/container_test.go`) builds the image and needs a Docker daemon; it is skipped
automatically when Docker is unavailable, or when `PM_SKIP_DOCKER_TESTS=1`.

More about the tests, benchmarks and repository layout is in [development](development.md).

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `smtp listen: ... address already in use` | Another program uses port 2525 or 8080. Change `PM_SMTP_ADDR` / `PM_HTTP_ADDR`, or the published ports in `compose.yaml`. |
| `550 Relay access denied` | The recipient domain is not in `PM_DOMAINS`. Add it (comma-separated) or use `@localhost`. |
| `452 ... Mailbox limit reached` | The instance already holds `PM_MAX_MAILBOXES` mailboxes; delete some or raise the limit. |
| Mail sent but not shown | Check the mailbox name; `Alice+x@localhost` is mailbox `alice`. |
| `configuration error: PM_...` | A setting has an invalid value; the message names the variable. |
