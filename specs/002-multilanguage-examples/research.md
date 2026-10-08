# Research: Multi-Language Verification-Code Examples

## Decision 1: Python implementation
- **Decision**: One script using `urllib.request`, `argparse`, `json`, `re`, `os`, `sys`; targets Python 3.9+.
- **Rationale**: Standard library only, meets FR-006. `urllib` handles headers and 204/HTTP errors adequately.
- **Alternatives**: `requests` (rejected: third-party package, violates Principle II).

## Decision 2: Node.js implementation
- **Decision**: One ES-module script (`.mjs`) using global `fetch`, `AbortSignal.timeout`, and `util.parseArgs`; targets Node 18+.
- **Rationale**: No packages, no `package.json`; `.mjs` avoids needing module config. The repo already requires Node 22 for web tests.
- **Alternatives**: `axios`/`node-fetch` (rejected: dependencies); CommonJS with `http` (rejected: more code).

## Decision 3: Flag style
- **Decision**: Keep the Go flag names (`-mailbox`, `-api`, `-timeout`, `-pattern`) in spirit but use each language's idiom: Python/Node accept `--mailbox`, `--api`, `--timeout`, `--pattern`. Timeout is given as `30s` style duration (digits plus `s`, `m`, or `ms`) to match Go's `-timeout 20s`.
- **Rationale**: Double-dash is idiomatic for both parsers; the Go `flag` package accepts either form, so docs can say "same flags" with one note. Matching the duration format keeps the value interchangeable.
- **Alternatives**: single-dash long flags in Python/Node (non-idiomatic and unsupported by `argparse`/`parseArgs` without hacks).

## Decision 4: Exit statuses and error classes
- **Decision**: 0 code printed; 1 no email in time or no code in it; 2 usage, connection, HTTP error (including 401, so the message names the status and body); matches Go. Messages go to stderr; stdout carries only the code.
- **Rationale**: Directly satisfies FR-002/FR-005 and the "wrong token" edge case via the HTTP error text.

## Decision 5: Behavior details copied from Go
- Wait timeout clamped to 1-60 seconds; HTTP client timeout is wait + 10 s.
- Take the first message from the wait response; search subject, then text, then HTML for the pattern; print the first match.
- Default pattern `\b\d{6}\b`. Regex dialect differences (Go RE2 vs Python/JS) are irrelevant for the default and for simple documented patterns.

## Decision 6: Verification
- **Decision**: Add `bash verify` blocks for each language in the docs page, run by the existing docs test. Extend the test's tool-detection list with `python3` and `node` so a missing runtime skips with a message. Add a test asserting each example file exists, is linked from the docs page, and mentions the shared flags, `PM_API_TOKEN`, and exit codes.
- **Rationale**: Reuses the established mechanism (FR-009) instead of a second harness.
- **Alternatives**: separate per-language unit tests (rejected: duplicates the live-instance test and adds frameworks).
- **Environment check**: the `test` Docker stage is `node:22-bookworm`, which has Node and Python 3; CI (`ubuntu-latest`) has Python 3 and sets up Node 22.

## Decision 7: Failure-path coverage
- Add one verified block per language that checks the exit status 1 on an empty mailbox with a short timeout and status 2 with no mailbox flag, using `bash` conditionals (`set -e` safe: `|| rc=$?`).
