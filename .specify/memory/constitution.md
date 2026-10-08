<!--
Sync Impact Report
Version change: 1.1.0 → 1.1.1 (PATCH: clarification of Principle III)
Modified principles: III. Cloud Friendly (durable state may use a mounted volume)
Added sections: none
Removed sections: none
Templates:
  ✅ .specify/templates/plan-template.md (added documentation gate)
  ✅ .specify/templates/tasks-template.md (added documentation tasks guidance)
  ✅ .specify/templates/spec-template.md (no change needed)
Follow-up TODOs: none
Prior: 1.1.0 added Principle V (Thoroughly Documented)
-->
# Phantom Mail Constitution

## Core Principles

### I. Well Tested (NON-NEGOTIABLE)
Every user story and behavior change MUST ship with automated tests. Tests MUST be
written before or alongside the implementation, and MUST fail before the change and
pass after it. Each user story MUST have at least one test that exercises it end to
end at the highest practical level, plus unit tests for non-trivial logic. Bug fixes
MUST include a regression test. The full suite MUST pass before merge.

Rationale: A mail-handling system's failures are silent and costly; tests are the
only durable proof that behavior is correct.

### II. Minimal Dependencies
Every third-party dependency MUST be justified in the plan (what it replaces, why the
standard library or a few lines of code are insufficient). Prefer the standard library.
New dependencies MUST be actively maintained, permissively licensed, and pinned
(lockfile committed). Transitive weight MUST be considered. Dependencies that are only
used in one small place SHOULD be removed or replaced.

Rationale: Fewer dependencies mean a smaller attack surface, simpler upgrades, and
faster builds.

### III. Cloud Friendly
The system MUST run as stateless, containerizable processes. Configuration MUST come
from environment variables (twelve-factor); secrets MUST never be committed. Durable
state MUST live behind an interface and MUST be kept on a mounted volume or an
external service (database, object store, queue), never on the container's ephemeral
filesystem. Logs MUST go to stdout/stderr as structured output.
Processes MUST start quickly, shut down gracefully on SIGTERM, and expose a health
check. Code MUST NOT be coupled to a single cloud vendor without an abstraction
boundary.

Rationale: The service must deploy and scale on any cloud without rework.

### IV. Local Testing Friendly
The full test suite and a working development environment MUST run on a developer
machine with a single documented command and no cloud account, credentials, or network
access. External services MUST be accessed through interfaces with in-memory or
containerized local substitutes (fakes, emulators). Tests MUST be deterministic,
isolated, and MUST NOT depend on shared remote state. CI MUST run the same commands
developers run locally.

Rationale: Fast, offline feedback keeps quality high and onboarding cheap.

### V. Thoroughly Documented
Documentation is a deliverable, not an afterthought. The repository MUST contain a
quick start, local and container setup instructions, a complete configuration
reference, API reference with examples, and deployment guides. Documentation MUST live
in the repository and be versioned with the code. Every command and example in it MUST
be verified automatically (by the test suite or a docs check). Any change to behavior,
configuration, or an API MUST update the matching documentation in the same change.

Rationale: Software that cannot be set up or understood from its docs is not finished,
and unverified docs silently rot.

## Engineering Constraints

- Ports-and-adapters: all external I/O (mail transport, storage, clock, randomness)
  sits behind an interface so Principles I, III and IV can be met together.
- Configuration has local-friendly defaults; production values are injected via the
  environment.
- Added complexity beyond these constraints MUST be recorded in the plan's Complexity
  Tracking section.

## Development Workflow

- Every plan MUST include a Constitution Check verifying Principles I-V before
  research and again after design.
- Task lists MUST include test tasks for each user story, documentation tasks, and a
  documented local run command.
- Pull requests MUST show passing tests, updated documentation, and list any new
  dependencies with justification.
- Reviewers MUST block changes that violate a principle without a recorded exception.

## Governance

This constitution supersedes other practices. Amendments require a documented change
to this file, a rationale, and a review of dependent templates for consistency. Versions
follow semantic versioning: MAJOR for removed or redefined principles, MINOR for added
or materially expanded principles, PATCH for clarifications. All plans and reviews MUST
verify compliance; justified exceptions MUST be recorded in the plan's Complexity
Tracking. Use CLAUDE.md and the current plan for runtime development guidance.

**Version**: 1.1.1 | **Ratified**: 2026-10-07 | **Last Amended**: 2026-10-07
