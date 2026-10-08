# Implementation Plan: Multi-Language Verification-Code Examples

**Branch**: `002-multilanguage-examples` | **Date**: 2026-10-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-multilanguage-examples/spec.md`

## Summary

Add Python and Node.js equivalents of the existing Go example `docs/examples/verification-code`, each a single standard-library-only script that follows the shared [CLI contract](contracts/cli.md). Document them in `docs/testing-verification-flows.md`, and extend the docs test suite so each one is executed against a live instance by the existing `bash verify` mechanism, with a visible skip when the runtime is missing.

## Technical Context

**Language/Version**: Python 3.9+ (script uses `urllib`, `argparse`, `re`, `json`); Node.js 18+ (uses built-in `fetch`, `util.parseArgs`; the project already uses Node 22 for web tests). Existing Go 1.23 test harness.

**Primary Dependencies**: None added. Python and Node standard libraries only (Constitution II).

**Storage**: N/A

**Testing**: Existing `tests/docs/docs_test.go` runs every `bash verify` block against a freshly started instance; new blocks invoke the Python and Node scripts. Small additions to that test (tool list for skip logic, example-parity check). Run via `make docs-check`.

**Target Platform**: Developer machines and CI (Linux/macOS); the `test` Docker stage already has Node and Debian's Python 3.

**Project Type**: Documentation examples (no change to the service)

**Performance Goals**: N/A (a code is printed within the wait period)

**Constraints**: One file per example, no packages to install, identical flags, environment variable and exit statuses as the Go example.

**Scale/Scope**: Two new files, one documentation page section, one test file edit, README/index mentions.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] I. Well Tested: every example is executed end to end by the docs test (US1, US3); a parity test checks all examples document the same flags and exit codes (US2); failure paths (timeout, no code, bad usage) get verified blocks too.
- [x] II. Minimal Dependencies: zero new dependencies; standard libraries only.
- [x] III. Cloud Friendly: not affected; examples read config from flags and `PM_API_TOKEN`.
- [x] IV. Local Testing Friendly: runs with `make docs-check`, no cloud. Missing runtimes skip visibly.
- [x] V. Thoroughly Documented: the documentation is the deliverable; every added command is verified automatically and linked from index and README.

Post-design re-check: still passes; no violations.

## Project Structure

### Documentation (this feature)

```text
specs/002-multilanguage-examples/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── cli.md           # shared command-line contract for every example
└── tasks.md             # created by /speckit-tasks
```

### Source Code (repository root)

```text
docs/
├── testing-verification-flows.md        # add Python and Node sections, language list
├── index.md                             # mention multi-language examples
└── examples/
    ├── verification-code/main.go        # existing reference
    ├── verification-code-python/verification_code.py
    └── verification-code-node/verification-code.mjs
README.md                                # mention multi-language examples
tests/docs/docs_test.go                  # python3/node in skip list; parity test
```

**Structure Decision**: One directory per language beside the Go example, keeping the existing `docs/examples/` convention; no build files since each is a single script.

## Complexity Tracking

No violations to justify.
