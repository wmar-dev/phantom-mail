# Quickstart & Validation Guide: Disposable Inbox Service

Runnable scenarios that prove the feature works end to end. Contract details live in
[contracts/openapi.yaml](contracts/openapi.yaml); entities in [data-model.md](data-model.md).
These commands are the basis of the automated docs-verification test required by the
constitution (Principle V).

## Prerequisites

- Docker with Compose (the only requirement), **or** Go 1.23+ for native runs.
- No cloud account, credentials, or internet access once images are built.

## 1. Start locally

```bash
docker compose up --build
```

Expect: web UI at <http://localhost:8080>, API at `http://localhost:8080/api/v1`, SMTP on
`localhost:2525`, `GET /healthz` returns `{"status":"ok",...}`. Default served domain:
`localhost`.

Native alternative: `make run` (builds and runs `./cmd/phantom-mail` with the same defaults).

## 2. Deliver a test email (User Story 3)

```bash verify
curl --url smtp://localhost:2525 --mail-from sender@example.com \
  --mail-rcpt alice@localhost \
  -T - <<'EOF'
From: Sender <sender@example.com>
To: alice@localhost
Subject: Your verification code

Your code is 482913
EOF
```

## 3. Read it through the API (User Story 2)

```bash verify
curl -s http://localhost:8080/api/v1/mailboxes/alice/messages
curl -s "http://localhost:8080/api/v1/mailboxes/alice/messages/wait?timeout=5"
```

Expect: the list contains one message with subject `Your verification code`; fetching it by id
returns the text body containing `482913`.

`wait` returns immediately if the mailbox already has messages, otherwise it holds the request
open until one arrives. Use a unique mailbox name per test run (for example `test-$RANDOM`) so
stale mail is never picked up.

Typical test-script pattern (wait, then extract the code):

```bash verify
curl -s "http://localhost:8080/api/v1/mailboxes/alice/messages/wait?timeout=30" \
  | jq -r '.messages[0].id' \
  | xargs -I{} curl -s http://localhost:8080/api/v1/mailboxes/alice/messages/{} \
  | jq -r '.text' | grep -oE '[0-9]{6}'
```

## 4. Read it in the web interface (User Story 1)

Open <http://localhost:8080>, type `alice`, open the message. Send another email (step 2) and
confirm it appears without reloading.

## 5. Delete and cleanup (User Stories 2 and 5)

```bash verify
curl -s -X DELETE http://localhost:8080/api/v1/mailboxes/alice/messages   # 204
```

Retention and caps are exercised by automated tests using a fake clock; to try manually, start
with `PM_RETENTION=1m` and confirm messages disappear after a minute.

## 6. Run the full test suite offline (Principle I and IV)

```bash
docker compose run --rm test        # only Docker required
make test                           # native Go
make bench                          # performance benchmarks
```

Expect: all unit, integration, e2e, and docs-verification tests pass with the network
disabled; benchmarks report results within the goals in [plan.md](plan.md).

## 7. Validate footprint (performance focus)

```bash
docker images phantom-mail --format '{{.Size}}'     # expect < 15 MB
docker stats --no-stream phantom-mail               # expect idle memory < 20 MB
```

## 8. Cloud deployment smoke test (User Story 4)

Follow `docs/deployment.md`: run the same image on a host with a public IP and port 25 open, set
`PM_DOMAINS=mail.example.com`, create an `MX` record for `mail.example.com` and an `A` record
for the MX host, front port 8080 with HTTPS. Then sign up on an external site using
`anything@mail.example.com` and confirm the verification email shows in the web interface.
