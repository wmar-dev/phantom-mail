# Feature Specification: Multi-Language Verification-Code Examples

**Feature Branch**: `002-multilanguage-examples`

**Created**: 2026-10-08

**Status**: Draft

**Input**: User description: "Add more examples in different programming languages in addition to go."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Copy a working example in my own language (Priority: P1)

A developer writing automated sign-up tests in a language other than Go opens the documentation on testing verification-code flows and finds a ready-to-use example in their language that waits for the email, reads it, and prints the verification code. They copy it into their test harness and it works without modification beyond pointing it at their service address and mailbox name.

**Why this priority**: Today only Go users get a complete example; everyone else must translate pseudocode. Covering the most common test-automation languages delivers the bulk of the value.

**Independent Test**: Run the example for one added language against a running instance while a message containing a code is delivered to the chosen mailbox; it prints the expected code and exits successfully.

**Acceptance Scenarios**:

1. **Given** a running instance and a mailbox that receives an email containing a six-digit code, **When** a developer runs the example for any supported language, **Then** it prints that code and exits with a success status.
2. **Given** a running instance and an empty mailbox, **When** the example's wait period elapses with no email, **Then** it exits with a failure status distinct from usage errors and shows a clear message.
3. **Given** an instance that requires an access token, **When** the token is supplied through the same environment setting used by the existing example, **Then** the example authenticates and succeeds.

---

### User Story 2 - Find the right example quickly (Priority: P2)

A developer reading the documentation sees a list of available languages with a link to each example, plus the pseudocode fallback for any language not covered. Each example is presented consistently so switching languages feels familiar.

**Why this priority**: Examples only help if they are discoverable and uniform; this is secondary to having them.

**Independent Test**: From the verification-flows page, follow the link for each language and confirm it leads to a runnable example with the same inputs and outputs as the others.

**Acceptance Scenarios**:

1. **Given** the verification-flows documentation, **When** a reader looks for their language, **Then** they find a clearly labelled section with run instructions and the link to the example.
2. **Given** any two examples, **When** compared, **Then** they accept the same inputs (mailbox, service address, timeout, code pattern), read the same token setting, and use the same exit-status meanings as the Go example.

---

### User Story 3 - Trust that examples stay correct (Priority: P3)

A maintainer changes the service's behavior or API. The automated documentation checks run every example against a live instance, so any example that no longer works is caught before release.

**Why this priority**: The project already requires documented examples to be verified automatically; the new examples must meet the same bar so they do not go stale.

**Independent Test**: Break the API response shape deliberately and confirm the automated checks fail for every language example.

**Acceptance Scenarios**:

1. **Given** the full automated check suite, **When** it runs, **Then** each language example is executed against a live instance and must print the expected code.
2. **Given** a language whose runtime is not installed on a contributor's machine, **When** the checks run, **Then** that language's check is reported as skipped (not passed) with a message naming the missing runtime.

---

### Edge Cases

- An email arrives with no matching code: the example exits with the "no code" status and says so.
- The service is unreachable or the address is malformed: the example exits with the usage/connection error status.
- The access token is wrong: the example reports an authorization failure rather than a generic timeout.
- A code that is not six digits (for example 8 alphanumeric characters): the pattern option handles it.
- The mailbox already contains older messages: the examples return the first message the service reports, so the documentation tells readers to use a fresh mailbox name per run.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The project MUST provide a complete verification-code example for each of these languages in addition to Go: Python and JavaScript (Node.js).
- **FR-002**: Each example MUST wait for a new email in a given mailbox, extract the verification code, and print only the code to standard output.
- **FR-003**: Each example MUST accept the same inputs as the Go example: mailbox name (required), service address (defaulting to the local instance), wait timeout, and a custom code pattern. Option names and values are the same across languages; only the flag prefix follows each language's convention (single dash in Go, double dash in Python and Node.js), and durations use a number plus a unit suffix such as `30s`.
- **FR-004**: Each example MUST read the optional access token from the same environment setting as the Go example and send it when present.
- **FR-005**: Each example MUST use the same exit statuses as the Go example: success when a code is printed, failure when no email arrives or it contains no code, and a distinct status for usage or connection errors.
- **FR-006**: Each example MUST rely only on its language's standard facilities, with no third-party packages to install.
- **FR-007**: Each example MUST be self-contained in its own location in the examples area, with a short comment header explaining what it does and how to run it.
- **FR-008**: The verification-flows documentation MUST list every available language, with run instructions and a link per example, and MUST retain the generic fallback for other languages.
- **FR-009**: Every documented command for running the examples MUST be verified by the project's automated documentation checks against a live instance, and a missing language runtime MUST cause a visible skip rather than a silent pass.
- **FR-010**: The documentation index and README MUST mention that examples exist in multiple languages.

### Key Entities

- **Language example**: A self-contained program for one language that retrieves a verification code from a mailbox; attributes are language, location, inputs, and exit statuses.
- **Example catalog**: The list in the documentation mapping each language to its example and run instructions.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Three languages (Go, Python, Node.js) have a working end-to-end example.
- **SC-002**: A developer in a covered language can go from opening the documentation to a printed code in under 5 minutes.
- **SC-003**: 100% of language examples are exercised by the automated documentation checks, and each passes against a live instance.
- **SC-004**: All examples return identical results (same code, same exit status) for the same mailbox scenario.
- **SC-005**: No example requires installing any additional package beyond the language's own runtime.

## Assumptions

- The added languages, Python and Node.js, were chosen by the user; more can be added later in the same pattern.
- The existing Go example defines the reference behavior (inputs, token setting, exit statuses) that the others mirror.
- Contributors may not have every runtime installed locally; the continuous checks environment is expected to have them.
- Changing the service or its API is out of scope; this feature only adds examples and documentation.
