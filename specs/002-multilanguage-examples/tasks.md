---

description: "Task list for multi-language verification-code examples"
---

# Tasks: Multi-Language Verification-Code Examples

**Input**: Design documents from `/specs/002-multilanguage-examples/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli.md, quickstart.md

**Tests**: Mandatory (Constitution Principle I). Tests here are `bash verify` documentation blocks run by `tests/docs/docs_test.go` plus Go test additions, written before the scripts so they fail first.

**Organization**: Grouped by user story. The reference behavior is the Go example at `docs/examples/verification-code/main.go`; the shared contract is `specs/002-multilanguage-examples/contracts/cli.md`.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

- [X] T001 Verify (no edits unless missing): `python3 --version` is 3.9+, `node --version` is 18+, and the `test` stage in `Dockerfile` and `.github/workflows/ci.yml` provide both; add `python3` to the CI job only if absent

---

## Phase 2: Foundational (blocks all user stories)

**Purpose**: Make the docs test able to run and skip the new languages.

- [X] T002 In `tests/docs/docs_test.go`, add `python3` and `node` to the tool list in `TestVerifiedExamplesRun` so a missing runtime produces `t.Skipf` naming the runtime (spec US3 scenario 2, FR-009)

**Checkpoint**: Existing docs tests still pass (`make docs-check`).

---

## Phase 3: User Story 1 - Copy a working example in my own language (P1) MVP

**Goal**: Python and Node scripts that print the code from a live mailbox.

**Independent Test**: With an instance running, deliver an email with a code and run each script; it prints the code and exits 0.

### Tests first (must fail before T006/T007)

- [X] T003 [US1] In `docs/testing-verification-flows.md`, add a section "With the Python example program" containing a `bash verify` block (mirror the Go block: background `curl` SMTP delivery of code `904217`, then `CODE=$(python3 docs/examples/verification-code-python/verification_code.py --api http://localhost:8080 --mailbox "$BOX" --timeout 20s)`, `test "$CODE" = "904217"`, `wait`)
- [X] T004 [US1] In the same file, add the equivalent "With the Node.js example program" section with a `bash verify` block using `node docs/examples/verification-code-node/verification-code.mjs`
- [X] T005 [US1] Add `bash verify` blocks for both languages in `docs/testing-verification-flows.md`, each using a fresh `$BOX`: (a) an empty mailbox with `--timeout 1s` exits 1 with empty stdout; (b) a missing `--mailbox` exits 2; (c) `--api http://localhost:1` exits 2; (d) an email containing `Code: AB12CD34` with `--pattern '[A-Z0-9]{8}'` prints `AB12CD34` (capture statuses with `rc=0; ... || rc=$?; test "$rc" = 1`)
- [X] T006 [P] [US1] Implement `docs/examples/verification-code-python/verification_code.py` per `contracts/cli.md`: `argparse` options `--mailbox` (required), `--api`, `--timeout` (`ms`/`s`/`m` suffix, default `30s`, clamped 1-60 s), `--pattern`; read `PM_API_TOKEN`; use only `urllib`, `json`, `re`, `os`, `sys`; wait endpoint then message fetch; search subject, text, html; print only the code; exit 0/1/2 with Go's messages (`no email arrived...`, `no code matching...`); header comment with purpose and run command
- [X] T007 [P] [US1] Implement `docs/examples/verification-code-node/verification-code.mjs` with the same behavior using global `fetch`, `AbortSignal.timeout`, and `util.parseArgs`; no `package.json`; header comment with purpose and run command
- [X] T008 [US1] In `tests/docs/docs_test.go`, extend `startInstance` to accept extra environment entries and add `TestExamplesSendAccessToken`: start an instance with `PM_API_TOKEN=secret`; for Python and Node, a run with the token exits 0 and a run without it exits 2 with an `unauthorized` message on stderr (skip if the runtime is missing)
- [X] T009 [US1] Run `go test ./tests/docs/...`; confirm T003-T005 and T008 pass for both languages

**Checkpoint**: Both scripts work end to end; US1 is shippable.

---

## Phase 4: User Story 2 - Find the right example quickly (P2)

**Goal**: Discoverable, uniform documentation.

**Independent Test**: The docs page lists each language with a working link; parity test passes.

- [X] T010 [US2] Add a parity test `TestLanguageExamplesAreConsistent` to `tests/docs/docs_test.go`: for each example file (Go, Python, Node) assert it exists, contains `PM_API_TOKEN` and the strings `mailbox`, `timeout`, `pattern` and `api`, and is linked from `docs/testing-verification-flows.md`; also scan the Python file's `import` lines against standard-library names and the Node file's imports for non-`node:` packages, failing on any other; write it first and see it fail
- [X] T011 [US2] In `docs/testing-verification-flows.md`, add a "Examples by language" table near the top (Go, Python, Node.js: runtime required, run command, link), explain the single-dash (Go) versus double-dash (Python, Node) flag difference, repeat the exit-status meanings, and keep the "From any language" pseudocode section as the fallback
- [X] T012 [P] [US2] In `docs/index.md`, update the row "Automate 'sign up, read the code, continue' in tests" to mention Go, Python and Node.js examples
- [X] T013 [P] [US2] In `README.md`, add a one-line mention of the multi-language examples with a link to `docs/testing-verification-flows.md` (FR-010)
- [X] T014 [US2] Run `go test ./tests/docs/...` (links, anchors, parity) and fix any failure

**Checkpoint**: US1 and US2 pass.

---

## Phase 5: User Story 3 - Trust that examples stay correct (P3)

**Goal**: Examples are guarded by the automated checks and break loudly.

**Independent Test**: Temporarily change a JSON field name the scripts read; `make docs-check` fails for both.

- [X] T015 [US3] Mutation check: temporarily rename `"messages"` to `"msgs"` in each script, run `make docs-check`, confirm it fails for Python and Node, then revert (do not commit the change). This is a one-off validation, not a committed test
- [X] T016 [US3] Skip check: run `PATH=/usr/bin:/bin $(which go) test ./tests/docs/...` (absolute `go`, so Node is not found) and confirm the Node blocks report SKIP naming the runtime rather than passing. This is a one-off validation, not a committed test
- [X] T017 [US3] In `docs/development.md`, note under the docs-check description that the language examples need `python3` and `node` and are skipped when absent, and that new examples must follow `specs/002-multilanguage-examples/contracts/cli.md`

---

## Phase 6: Polish

- [X] T018 [P] Run the quickstart in `specs/002-multilanguage-examples/quickstart.md` by hand against `make run`
- [X] T019 Run the full suite: `make test docs-check` (and `docker compose run --rm test` if Docker is available) and confirm green with no new third-party dependencies (`TestNoThirdPartyDependencies`)
- [X] T020 Review each script for style consistency with the Go example (comments, messages, exit codes) and verify stdout contains only the code

---

## Dependencies & Order

- Phase 1 → Phase 2 → US1 → US2 → US3 → Polish. US2 depends on US1 files existing for link checks; US3 depends on US1 blocks.
- Within US1: T003-T005 and T008 (tests) before T006/T007 (implementation).
- Parallel: T006 with T007; T012 with T013.

## Parallel Example (US1)

```text
T006 python script   |  T007 node script      (different files)
```

## Implementation Strategy

- **MVP**: Phases 1-3 (US1): working Python and Node examples with verified docs blocks.
- Then US2 for discoverability, US3 for guard validation, then Polish.
- Commit after each phase; no service code changes.
