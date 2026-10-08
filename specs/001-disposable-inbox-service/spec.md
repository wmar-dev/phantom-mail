# Feature Specification: Disposable Inbox Service

**Feature Branch**: `001-disposable-inbox-service`

**Created**: 2026-10-07

**Status**: Draft

**Input**: User description: "Design a maildrop.cc clone, so I can test accounts that require entering in a verification code. I want a web interface as well as an api. I want to be able to run it locally and also serve it on a domain in the cloud."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Receive and read a verification email in the web interface (Priority: P1)

A tester signs up for an account on some other site using an address of their choosing at the service's domain (for example `anything@mail.example.com`). They open the service's web page, type the mailbox name, and see the verification email appear. They open it and read the code.

**Why this priority**: This is the core value: receiving mail for any address with no signup, and reading it. Everything else builds on it.

**Independent Test**: Send an email to a never-before-used address, open the web interface, enter that mailbox name, and confirm the message is listed and its content is readable.

**Acceptance Scenarios**:

1. **Given** no mailbox has ever been used for `alice`, **When** an email is sent to `alice@<service domain>`, **Then** `alice` appears to have that message without any prior registration.
2. **Given** a mailbox with messages, **When** the tester enters the mailbox name in the web interface, **Then** the messages are listed newest first with sender, subject, and received time.
3. **Given** a listed message, **When** the tester opens it, **Then** they see the sender, recipient, subject, and the body (text and HTML rendered safely).
4. **Given** the tester has a mailbox open, **When** a new email arrives, **Then** it appears in the list without a manual page reload.

---

### User Story 2 - Retrieve messages and verification codes through an API (Priority: P1)

An automated test script signs up for an account, then calls the service's API to wait for the verification email and extract the message content, so the whole sign-up flow can be tested without a human.

**Why this priority**: Automated testing of verification flows is the stated purpose; the API is explicitly requested and is equal in importance to the web interface.

**Independent Test**: Send an email to a mailbox, then use the API to list messages, fetch one, and delete it; confirm each response contains the expected data.

**Acceptance Scenarios**:

1. **Given** a mailbox with messages, **When** a client requests the mailbox's messages, **Then** it receives a machine-readable list with identifiers, sender, subject, and received time.
2. **Given** a message identifier, **When** a client requests that message, **Then** it receives the full content including text and HTML bodies.
3. **Given** an empty mailbox, **When** a client asks to wait for a message with a timeout, **Then** the request returns as soon as a message arrives, or reports no message when the timeout elapses.
4. **Given** a message, **When** a client deletes it (or the whole mailbox), **Then** subsequent requests no longer return it.
5. **Given** a malformed mailbox name or unknown message identifier, **When** requested, **Then** the client receives a clear error response.

---

### User Story 3 - Run the whole service locally (Priority: P2)

A developer starts the full service on their own machine with one documented command, with no cloud account or internet. They point the system under test at the local service and can also inject sample emails directly to try things out.

**Why this priority**: Local use is an explicit requirement and lets the service be used in development and automated test pipelines before any cloud deployment exists.

**Independent Test**: On a clean machine with no network access, start the service with the single documented command, deliver a test email to it, and read it via both the web interface and the API.

**Acceptance Scenarios**:

1. **Given** a fresh checkout, **When** the developer runs the documented start command, **Then** the web interface, API, and mail receiver are all available locally.
2. **Given** the local service is running, **When** a test email is delivered to its local mail receiver, **Then** it is readable via web and API exactly as in the hosted deployment.
3. **Given** the local service, **When** the developer runs the documented test command, **Then** the entire automated test suite passes offline.

---

### User Story 4 - Serve the service on a public domain in the cloud (Priority: P2)

The owner deploys the service to a cloud environment, points their own domain's mail and web records at it, and then real emails sent by third-party sites to any address at that domain arrive and are readable.

**Why this priority**: Real third-party sites can only send to a publicly reachable domain, so this is required to test real accounts; it comes after local use works.

**Independent Test**: Deploy to the cloud, configure the domain, sign up on an external site with an address at that domain, and read the verification email through the web interface.

**Acceptance Scenarios**:

1. **Given** a deployed instance and a configured domain, **When** an external site sends mail to any address at that domain, **Then** the message is received and readable.
2. **Given** the deployed instance, **When** it is restarted or replaced, **Then** it starts quickly using only environment-provided configuration, and messages still within their retention period remain available.
3. **Given** the deployed instance, **When** an operator checks its health, **Then** a health status is reported.

---

### User Story 5 - Mailboxes clean themselves up (Priority: P3)

Messages are automatically removed after a limited time so the service does not grow without bound and old verification codes do not linger.

**Why this priority**: Important for operating cost and privacy, but the service is usable without it in the short term.

**Independent Test**: Deliver a message, advance past the retention period, and confirm it is no longer retrievable.

**Acceptance Scenarios**:

1. **Given** a message older than the retention period, **When** anyone requests it, **Then** it is no longer available.
2. **Given** a mailbox exceeding the per-mailbox message limit, **When** a new message arrives, **Then** the oldest messages are discarded.

---

### Edge Cases

- Mailbox names differing only by letter case (`Alice` vs `alice`) refer to the same mailbox.
- Plus-addressing (`alice+shop@domain`) delivers to a defined mailbox (`alice`) while preserving the original recipient on the message.
- Mail addressed to a domain this service does not serve is rejected.
- Oversized messages and oversized attachments are rejected or truncated per a stated limit rather than crashing the service.
- Messages with only HTML, only text, no subject, non-English character sets, or malformed headers are still stored and displayed.
- HTML content from untrusted senders must not run scripts or load remote content in the web interface.
- A flood of mail to one mailbox, from one sender, or to many distinct random mailbox names does not take the service down for others; the number of mailboxes is capped, and mail for a new mailbox beyond the cap gets a temporary failure while existing mailboxes keep receiving.
- Multiple clients waiting on the same mailbox all receive the new message.
- Messages with attachments list the attachments, and they can be downloaded.
- When storage is full, the sender is told to retry later (a temporary failure) rather than the message being silently dropped or the service crashing.
- Mail addressed to the same mailbox name at different served domains goes to one shared mailbox (names are not scoped by domain).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST accept inbound email addressed to any mailbox name at its configured domain(s), creating the mailbox implicitly on first use with no registration.
- **FR-002**: System MUST treat mailbox names case-insensitively and MUST define how plus-addressing maps to a mailbox.
- **FR-003**: System MUST reject inbound mail for domains it is not configured to serve.
- **FR-004**: System MUST store each received message with sender, recipients, subject, received time, text body, HTML body, and attachments.
- **FR-005**: Web interface MUST let a user enter any mailbox name and view its messages newest first, with no account required (unless an access token is configured, see FR-027).
- **FR-006**: Web interface MUST display a selected message's headers and body, rendering HTML safely (no script execution, no automatic remote content loading).
- **FR-007**: Web interface MUST show newly arrived messages in an open mailbox without a manual reload.
- **FR-008**: Web interface MUST let users delete individual messages and empty a mailbox.
- **FR-009**: API MUST provide operations to list a mailbox's messages, fetch a single message, delete a message, and empty a mailbox, returning structured data and clear errors.
- **FR-010**: API MUST provide a way to wait for a new message in a mailbox with a caller-specified timeout.
- **FR-011**: API MUST allow attachments to be downloaded.
- **FR-012**: System MUST automatically delete messages after a configurable retention period (default 24 hours) and cap messages per mailbox (default 100 messages).
- **FR-013**: System MUST enforce a configurable maximum message size and per-remote-address and per-mailbox rate limits, plus a configurable cap on the number of mailboxes, to protect availability.
- **FR-014**: System MUST run entirely on a developer machine with one documented command and require no cloud account, credentials, or internet access.
- **FR-015**: System MUST provide a documented way to deliver a test email to a local instance and a single documented command that runs the full automated test suite offline.
- **FR-021**: System MUST be packaged as a Docker container image that runs the complete service (web, API, mail receiver) with a single command, and MUST provide a Docker Compose setup for local use so a developer needs only Docker installed.
- **FR-022**: The container image MUST be configurable solely through environment variables, keep durable data on a mountable volume, run as a non-root user, expose a container health check, and stop cleanly on termination signals.
- **FR-023**: The same container image MUST be usable unchanged for local use and for the cloud deployment, and the automated test suite MUST be runnable inside a container as well as directly.
- **FR-024**: The project MUST include thorough documentation covering: an overview and quick start; local setup with and without Docker; running the tests; the full configuration reference (every setting, its default, and its effect); the complete API reference with request and response examples and error cases; a web interface user guide; a cloud deployment guide, including domain, mail-routing, and encrypted-connection setup, with troubleshooting; and an example of testing a verification-code sign-up flow end to end.
- **FR-025**: Documentation MUST be kept in the repository, versioned with the code, and every command and example in it MUST be verified by an automated check (or the test suite) so it cannot silently go stale. A change to behavior, configuration, or the API MUST include the matching documentation update.
- **FR-026**: The API MUST be described by a machine-readable specification that is served by the running service and kept consistent with actual behavior.
- **FR-027**: When the operator configures an access token, the API and web interface MUST require it. Scripts present the token on each request. The web interface asks for it once and keeps the viewer signed in for the browser session, so live updates, message bodies, and attachment downloads keep working, and the token never appears in a URL or log. Failed attempts MUST be rate limited. The health check and inbound mail are unaffected.
- **FR-016**: System MUST take all deployment-specific settings (served domain, ports, limits, storage location) from environment configuration, with local defaults.
- **FR-017**: System MUST be deployable to a cloud environment and served on a custom domain for both web/API traffic (over an encrypted connection) and inbound mail.
- **FR-018**: System MUST expose a health status and shut down gracefully so deployments and restarts do not lose accepted messages.
- **FR-019**: System MUST write operational logs to standard output in a structured form.
- **FR-020**: System MUST NOT send outbound mail to external recipients (receive-only), so it cannot be used as a spam relay.

### Key Entities

- **Mailbox**: A name at a served domain; exists implicitly once mail arrives or someone views it. Contains messages.
- **Message**: One received email: sender, recipients, subject, received time, text body, HTML body, attachments, unique identifier.
- **Attachment**: A file belonging to a message with name, type, and size.
- **Domain**: A hostname this instance accepts mail for.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A message sent to a new address becomes visible in the web interface and API within 5 seconds of delivery in 95% of cases.
- **SC-002**: A user can go from opening the web interface to reading a verification code in under 30 seconds.
- **SC-003**: An automated script can complete a full sign-up-and-verify test flow (send, wait, read code) using only API calls, with no manual steps.
- **SC-004**: A new developer can go from fresh checkout to a running local instance receiving a test email in under 5 minutes by following the documentation.
- **SC-005**: The full automated test suite runs and passes on a machine with no network access.
- **SC-006**: An owner can go from a deployed instance to receiving a real external email on their own domain by following the documentation, with no code changes.
- **SC-007**: The service handles 50 simultaneous active mailboxes receiving mail with no message loss, while mailbox list requests stay under 50 ms at the 95th percentile.
- **SC-009**: With only Docker installed, a developer can start the full service with one command and receive a test email within 5 minutes, and the identical image runs in the cloud deployment with only configuration changes.
- **SC-010**: A first-time user can complete local setup, an API call, and a cloud deployment using only the documentation, without asking anyone for help; every documented setting and API operation appears in the reference, and 100% of documented commands and examples pass automated verification.
- **SC-008**: 100% of messages past the retention period are unavailable within 10 minutes of expiry.

## Assumptions

- Like maildrop.cc, mailboxes are public: anyone who knows a mailbox name can read it. The service is for testing with non-sensitive data only, and no user accounts or authentication are required for the web interface or API reads. The deployment owner may optionally require an access token for the web interface and API (FR-027).
- The owner controls a domain and can set its mail-routing and web records; registering the domain is out of scope.
- Receive-only: sending email, replying, and forwarding are out of scope.
- Single operator instance; multi-tenant hosting and billing are out of scope.
- Default limits (24-hour retention, 100 messages per mailbox, 10 MB per message) are configurable.
- Spam filtering and virus scanning beyond size and rate limits are out of scope for the first version.
- Docker is the packaging standard: the cloud deployment targets any generic container host rather than one specific vendor; the concrete choice is made at planning time.
- Inbound mail requires the host to allow public traffic on the mail port; some cloud providers restrict this, which is a deployment consideration for planning.
