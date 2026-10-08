# Contract: verification-code example command line

Every language example implements this contract identically.

## Invocation
- Go (reference): `go run ./docs/examples/verification-code -mailbox NAME`
- Python: `python3 docs/examples/verification-code-python/verification_code.py --mailbox NAME`
- Node.js: `node docs/examples/verification-code-node/verification-code.mjs --mailbox NAME`

## Options
| Option | Default | Meaning |
|--------|---------|---------|
| mailbox | required | Mailbox name to watch |
| api | `http://localhost:8080` | Base URL of the instance |
| timeout | `30s` | How long to wait; accepts `ms`, `s`, `m` suffixes; clamped to 1-60 s |
| pattern | `\b\d{6}\b` | Regular expression for the code |

(Go uses single-dash flags; Python and Node use double-dash.)

## Environment
- `PM_API_TOKEN`: when set, sent as `Authorization: Bearer <token>`.

## Behavior
1. `GET {api}/api/v1/mailboxes/{mailbox}/messages/wait?timeout=N`; `204` or empty list means no email.
2. `GET .../messages/{id}` for the first message.
3. Search subject, then text, then html; print the first match and a newline.

## Output and exit status
| Status | Meaning | stdout | stderr |
|--------|---------|--------|--------|
| 0 | Code found | the code only | empty |
| 1 | No email in time, or no code matched | empty | message starting `no email` or `no code` |
| 2 | Usage error, bad pattern, connection or HTTP error | empty | message |
