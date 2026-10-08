# Data Model: Disposable Inbox Service

Derived from the Key Entities and Functional Requirements in [spec.md](spec.md).

## Mailbox

A name at a served domain. Has no stored record of its own: it exists implicitly as the set of
messages stored under its normalized name (FR-001).

| Field | Description |
|-------|-------------|
| `name` | Normalized local part |

**Normalization (FR-002)**: lowercase; strip anything from the first `+` onward; must match
`^[a-z0-9._-]{1,64}$` after normalization, else it is invalid (SMTP: `550`; API: `400`). Mail
to `alice+shop@mail.example.com` is stored in mailbox `alice`, and the original recipient is
preserved in `Message.to`.

**Limits**: at most `PM_MAX_MESSAGES_PER_MAILBOX` (default 100) messages; the oldest are evicted
when exceeded.

## Message

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Unique, time-ordered (sortable); hex of 48-bit ms timestamp + 80 random bits |
| `mailbox` | string | Normalized mailbox name |
| `from` | string | Envelope/header sender |
| `to` | list of string | Original recipients as addressed (includes plus tags) |
| `subject` | string | Decoded; empty string if absent |
| `received_at` | timestamp (UTC) | Set at ingest; basis for retention |
| `size` | integer | Raw bytes |
| `text` | string | Decoded text body ("" if none) |
| `html` | string | Decoded HTML body ("" if none) |
| `attachments` | list of Attachment | Metadata only in list/get; content fetched separately |

**Validation**: raw size ≤ `PM_MAX_MESSAGE_BYTES` (default 10 MiB) else rejected with SMTP
`552`; recipient domain must be in `PM_DOMAINS` else `550` (FR-003); malformed headers or
unknown charsets are stored with best-effort decoding rather than rejected.

**Storage**: raw bytes on disk at `<data>/<mailbox>/<id>.eml` plus an in-memory index entry
holding the list-view fields (`id`, `mailbox`, `from`, `to`, `subject`, `received_at`, `size`,
attachment count, computed on first listing and then cached). `text`, `html`, and attachment content are parsed from the file on demand.

**Lifecycle**: `received` → (`deleted` by user | `expired` by retention | `evicted` by
per-mailbox or total-size cap). No intermediate states; deletion is permanent.

## Attachment

| Field | Type | Notes |
|-------|------|-------|
| `index` | integer | Position within the message; addresses the download |
| `filename` | string | Sanitized; defaults to `attachment-N` |
| `content_type` | string | As declared |
| `size` | integer | Decoded bytes |

Attachments are not stored separately; they are decoded from the raw message when downloaded
(FR-011).

## Domain

A hostname configured in `PM_DOMAINS` (comma-separated, default `localhost`). Not persisted.
Matching is case-insensitive.

## Relationships

```text
Domain (config) 1 ── * Mailbox (implicit) 1 ── * Message 1 ── * Attachment
```

## Index and eviction invariants

- Index is rebuilt at startup by scanning the data directory; files that fail to parse as
  `.eml` are logged and ignored.
- Messages are ordered by `id` (time-ordered), newest first in list responses.
- `received_at + PM_RETENTION <= now` means the message is treated as nonexistent for every
  read, even before the janitor deletes the file (SC-008).
- Total on-disk bytes never exceed `PM_MAX_TOTAL_BYTES` after an ingest completes.
- A mailbox directory exists only while it holds at least one message; it is removed with its last message (delete, expiry, or eviction).
- At most `PM_MAX_MAILBOXES` (default 10,000) mailboxes exist at once. Mail for a new mailbox name beyond that limit is refused with an SMTP temporary failure (`452`); existing mailboxes keep receiving.
