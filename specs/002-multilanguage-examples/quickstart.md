# Quickstart: validating the language examples

Prerequisites: Go 1.23+, `curl`, Python 3.9+, Node.js 18+. Start an instance (`make run`, ports 8080 and 2525).

```bash
BOX="signup-$RANDOM"
( sleep 1
  printf 'From: Shop <no-reply@shop.example>\r\nTo: %s@localhost\r\nSubject: Code\r\n\r\nYour code is 482913\r\n' "$BOX" \
    | curl -s --url smtp://localhost:2525 --mail-from no-reply@shop.example --mail-rcpt "$BOX@localhost" -T - ) &
python3 docs/examples/verification-code-python/verification_code.py --mailbox "$BOX" --timeout 20s   # prints 482913
wait
```

Repeat with `node docs/examples/verification-code-node/verification-code.mjs --mailbox "$BOX"`.

Expected: stdout is exactly the code and the exit status is 0. With an empty mailbox and `--timeout 1s` the status is 1; without `--mailbox` it is 2 (see the [CLI contract](contracts/cli.md)).

Automated: `make docs-check` runs all of this.
