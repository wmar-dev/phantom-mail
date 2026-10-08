# Testing sign-ups that send verification codes

The reason Phantom Mail exists: you are testing an account sign-up, a password reset or a login
that emails a one-time code, and you need to receive that email and read the code, by hand or
from an automated test, without owning a mailbox.

The pattern is always the same:

1. **Choose a fresh mailbox name** for this test run, such as `signup-8f3a91c2`.
2. **Trigger the flow** in the system under test using `signup-8f3a91c2@your.domain` as the email
   address.
3. **Wait** for the email with the [wait endpoint](api.md#wait-for-a-message). It returns the
   moment the message arrives (or `204` on timeout).
4. **Read the message** and extract the code.
5. **Clean up** by emptying the mailbox (optional; old mail expires anyway).

Use a **new mailbox name per test run**. Because a mailbox keeps its messages for 24 hours by
default, reusing a name could hand you the code from an earlier run. (Within one name,
`wait` with `after=<last id>` returns only newer mail if you must reuse it.)

Which domain? Locally it is `localhost` and the system under test must be able to reach SMTP port
`2525` on your machine (see [running locally](local.md)). In the cloud it is the domain you set
in `PM_DOMAINS`, and the system under test is a real third-party site (see
[deployment](deployment.md)).

## Examples by language

Complete, copy-ready programs that wait for the email and print the code. Each uses only its
language's standard library, so there is nothing to install:

| Language | Needs | Run it |
|----------|-------|--------|
| [Go](#with-the-go-example-program) | Go 1.23+ | `go run ./docs/examples/verification-code -mailbox NAME` |
| [Python](#with-the-python-example-program) | Python 3.9+ | `python3 docs/examples/verification-code-python/verification_code.py --mailbox NAME` |
| [Node.js](#with-the-nodejs-example-program) | Node.js 18+ | `node docs/examples/verification-code-node/verification-code.mjs --mailbox NAME` |

All three take the same options (`mailbox`, `api`, `timeout`, `pattern`), read `PM_API_TOKEN`, and
exit with `0` when a code was printed, `1` when no email arrived or it had no code, and `2` for a
usage or connection error. The only difference is the flag prefix: the Go program uses a single
dash (`-mailbox`), the Python and Node.js programs a double dash (`--mailbox`). For any other
language, see [From any language](#from-any-language).

## With curl and jq

This complete example plays both roles: a subshell stands in for the website and mails the code
a second after the script starts waiting; the rest is what your test does. Run it against a local
instance:

```bash verify
BOX="signup-$RANDOM"

# --- stand-in for the system under test: it emails a code to $BOX@localhost ---
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Your verification code\r\n\r\nYour code is 482913\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &

# --- your test: wait for the email, read it, extract the code ---
ID=$(curl -sf "http://localhost:8080/api/v1/mailboxes/$BOX/messages/wait?timeout=30" | jq -r '.messages[0].id')
CODE=$(curl -sf "http://localhost:8080/api/v1/mailboxes/$BOX/messages/$ID" | jq -r '.text' | grep -oE '[0-9]{6}' | head -1)
echo "verification code: $CODE"
test "$CODE" = "482913"

# --- clean up ---
curl -sf -X DELETE "http://localhost:8080/api/v1/mailboxes/$BOX/messages"
wait
```

With an access token (`PM_API_TOKEN`), add `-H "Authorization: Bearer $PM_API_TOKEN"` to each
`curl` that talks to port 8080.

## With the Go example program

[`docs/examples/verification-code`](examples/verification-code/main.go) is a small, standard
library only command that waits for the email and prints the code. Copy it into your test
harness or call it from scripts:

```bash verify
BOX="signup-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Welcome\r\n\r\nUse 904217 to finish signing up.\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
CODE=$(go run ./docs/examples/verification-code -api http://localhost:8080 -mailbox "$BOX" -timeout 20s)
echo "verification code: $CODE"
test "$CODE" = "904217"
wait
```

Flags: `-mailbox` (required), `-api` (default `http://localhost:8080`), `-timeout`, and `-pattern`
(a regular expression for codes that are not six digits, for example `[A-Z0-9]{8}`). It reads
`PM_API_TOKEN` from the environment. Exit status `0` means a code was printed, `1` means no email
arrived or it had no code, `2` means a usage or connection error.

## With the Python example program

[`docs/examples/verification-code-python`](examples/verification-code-python/verification_code.py) does the same as the Go program, with the Python 3.9 or newer standard library only
(nothing to install):

```bash verify
BOX="signup-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Welcome\r\n\r\nUse 904217 to finish signing up.\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
CODE=$(python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:8080 --mailbox "$BOX" --timeout 20s)
echo "verification code: $CODE"
test "$CODE" = "904217"
wait
```

Options: `--mailbox` (required), `--api`, `--timeout`, and `--pattern`, with the same meaning and
defaults as the Go flags. It reads `PM_API_TOKEN` and uses the same exit statuses. It prints only
the code on standard output; messages go to standard error.

Exit statuses and a custom pattern, checked against a live instance:

```bash verify
BOX="empty-$RANDOM"
rc=0; out=$(python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:8080 --mailbox "$BOX" --timeout 1s 2>/dev/null) || rc=$?
test "$rc" = 1 && test -z "$out"
rc=0; python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:8080 2>/dev/null || rc=$?
test "$rc" = 2
rc=0; python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:1 --mailbox "$BOX" --timeout 1s 2>/dev/null || rc=$?
test "$rc" = 2
BOX="pattern-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Code\r\n\r\nCode: AB12CD34\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
CODE=$(python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:8080 --mailbox "$BOX" --timeout 20s --pattern '[A-Z0-9]{8}')
test "$CODE" = "AB12CD34"
wait
```

## With the Node.js example program

[`docs/examples/verification-code-node`](examples/verification-code-node/verification-code.mjs) does the same as the Go program, with the Node.js 18 or newer standard library only
(nothing to install):

```bash verify
BOX="signup-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Welcome\r\n\r\nUse 615203 to finish signing up.\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
CODE=$(node docs/examples/verification-code-node/verification-code.mjs --api http://localhost:8080 --mailbox "$BOX" --timeout 20s)
echo "verification code: $CODE"
test "$CODE" = "615203"
wait
```

Options: `--mailbox` (required), `--api`, `--timeout`, and `--pattern`, with the same meaning and
defaults as the Go flags. It reads `PM_API_TOKEN` and uses the same exit statuses. It prints only
the code on standard output; messages go to standard error.

Exit statuses and a custom pattern, checked against a live instance:

```bash verify
BOX="empty-$RANDOM"
rc=0; out=$(node docs/examples/verification-code-node/verification-code.mjs --api http://localhost:8080 --mailbox "$BOX" --timeout 1s 2>/dev/null) || rc=$?
test "$rc" = 1 && test -z "$out"
rc=0; node docs/examples/verification-code-node/verification-code.mjs --api http://localhost:8080 2>/dev/null || rc=$?
test "$rc" = 2
rc=0; node docs/examples/verification-code-node/verification-code.mjs --api http://localhost:1 --mailbox "$BOX" --timeout 1s 2>/dev/null || rc=$?
test "$rc" = 2
BOX="pattern-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Code\r\n\r\nCode: AB12CD34\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
CODE=$(node docs/examples/verification-code-node/verification-code.mjs --api http://localhost:8080 --mailbox "$BOX" --timeout 20s --pattern '[A-Z0-9]{8}')
test "$CODE" = "AB12CD34"
wait
```

## From any language

Only plain HTTP is involved, so any language works. In pseudocode:

```text
mailbox = "signup-" + randomHex()
start the flow with  mailbox + "@" + MAIL_DOMAIN
response = GET {API}/api/v1/mailboxes/{mailbox}/messages/wait?timeout=30
if response.status == 204: fail("no email")
message  = GET {API}/api/v1/mailboxes/{mailbox}/messages/{response.messages[0].id}
code     = firstMatch(/\b\d{6}\b/, message.text or message.html)
```

Prefer the `text` field for matching: it is the plain-text part and needs no HTML parsing. Many
senders include only HTML; then match against `html`, and be as specific as the email allows,
because markup contains digits too (a colour such as `#123456` matches `\b\d{6}\b`). A pattern
like `code[^0-9]*(\d{6})` is safer than a bare six-digit match.

## Tips

- **Confirmation links instead of codes.** The same flow works for "click to confirm" emails:
  extract the first `https://` link from `text` or `html` with a regular expression and request it
  from your test.
- **Several emails.** Pass `after=<id of the newest message you have seen>` to `wait` to receive
  only newer mail, for example when a flow sends a welcome email and then a code.
- **Plus addresses.** `signup+run42@your.domain` delivers into mailbox `signup`; the original
  address is kept in the message's `to` field, so one mailbox can serve a whole suite while you
  still tell runs apart.
- **Timing.** Delivery to the inbox is typically a few milliseconds after the sender finishes
  transmitting; the time you wait is dominated by how long the system under test takes to send.
  Use a generous `timeout` (30 s) in tests and fail clearly on `204`.
- **Debugging.** Open `http://localhost:8080/#/signup-8f3a91c2` in a browser to watch the mailbox
  live while your test runs.
- **Rate limits.** Default limits are generous; if a large parallel suite hits `429`, raise
  `PM_RATE_*` (see [configuration](configuration.md)).
