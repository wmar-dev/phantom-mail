# Data Model: Multi-Language Verification-Code Examples

No stored data. The examples consume the existing API and keep nothing between runs.

## Language example
| Field | Description |
|-------|-------------|
| language | Python or Node.js (Go already exists) |
| path | `docs/examples/verification-code-<lang>/<file>` |
| inputs | mailbox (required), api, timeout, pattern, `PM_API_TOKEN` (environment) |
| outputs | code on stdout; diagnostics on stderr; exit status 0, 1 or 2 |

## Example catalog
The "Verification flows" documentation page lists each example with a link, run command, and runtime requirement.

## API data used (existing, see `specs/001-disposable-inbox-service/contracts/openapi.yaml`)
- Wait response: `{ messages: [{ id }] }` or `204`.
- Message: `{ subject, text, html }`.
