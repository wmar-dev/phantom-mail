# Web interface guide

The web interface is served by the same process as the API, at the root of the HTTP port
(`http://localhost:8080` by default). It needs no account and no installation, makes no requests
to other sites, and the whole page is about 20 KB.

## Opening a mailbox

1. Type any mailbox name into the box at the top and press **Open**. You can also paste a full
   address such as `Alice+shop@mail.example.com`: the part after `@` and any `+tag` are ignored,
   and the name is lower-cased, so it opens `alice`.
2. Valid names are 1 to 64 characters from letters, digits, `.`, `_` and `-`.
3. Mailboxes are created the first time mail arrives for them. There is nothing to register, and
   an unused mailbox is simply empty.

The address bar keeps the state, so `http://localhost:8080/#/alice` opens `alice` directly and
`#/alice/<message id>` opens one message. You can bookmark or share these links, and the browser
back button works.

## Reading mail

- **Live updates.** New messages appear at the top of the list the moment they arrive, with no
  reload. The indicator next to the title shows **Live** while the stream is connected and
  **Reconnecting...** if the connection drops. After a reconnect the list is refreshed so nothing
  is missed.
- **Verification codes.** Standalone numbers of 4 to 8 digits found in the subject or plain-text
  body are shown above the message as buttons. Click one to copy it.
- **Message / Plain text.** The **Message** tab shows the email as it was designed; **Plain text**
  shows the text part, which is often easier to read and to copy from.
- **Attachments** are listed under the message; click one to download it.
- **Copy address** copies `name@<this host>`. If your instance serves a different mail domain
  than the web host name, use the domain you configured in `PM_DOMAINS`.

## Deleting

- **Delete** (inside a message) removes that message.
- **Delete all** (above the list) empties the mailbox after a confirmation.

Messages also disappear on their own after the retention period (24 hours by default, see
[configuration](configuration.md)), and the oldest messages are dropped when a mailbox holds more
than the per-mailbox limit.

## How email content is kept safe

Email from strangers is untrusted, so the interface never treats it as part of the page:

- Subjects, senders and plain text are inserted as text, never as HTML.
- The HTML body is displayed in a **sandboxed frame** that cannot run scripts, submit forms, open
  windows, or reach the rest of the page. The server also sends a strict Content-Security-Policy
  with it, so **remote images and tracking pixels are not loaded**. Images embedded in the email
  itself (`data:` images) do display.
- Attachments are always served as downloads and never rendered in the page.

## Mailboxes are public

Anyone who knows a mailbox name can read it, exactly like other disposable-inbox services. Use
the service only with non-sensitive test data and unguessable names where it matters (for example
`signup-8f3a91c2`). If you run an instance on the public internet and want to restrict who can
read, set an access token; see [configuration](configuration.md).

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| "Mailbox names use letters, digits, . _ and -" | The name has spaces or other characters, or is longer than 64 characters. |
| Nothing arrives | The sender used a different domain than the one in `PM_DOMAINS`, or mail is not reaching port 25 (see [deployment](deployment.md)). |
| Stuck on "Reconnecting..." behind a reverse proxy | The proxy is buffering the event stream; turn off response buffering for `/api/` (see [deployment](deployment.md)). |
| HTML body is blank | The email has no HTML part; use the **Plain text** tab. |

## Signing in (protected instances)

If the operator set an access token (`PM_API_TOKEN`, see [configuration](configuration.md)), the
page asks for it the first time you open a mailbox. Enter the token once; the browser then keeps
you signed in until you close it.

- The token is sent in the body of a single request and is never put in a URL, so it does not end
  up in browser history, server logs or proxy logs.
- The server replies with a session cookie that is `HttpOnly` (scripts on the page cannot read
  it), `SameSite=Strict` (other sites cannot use it), and `Secure` when the page is served over
  HTTPS. The token itself is never stored in the browser.
- After a wrong token you can try again. After several wrong attempts from the same address the
  server pauses sign-in for a minute ("Too many attempts").
- To sign out, close the browser, or call `DELETE /api/v1/session`
  (see [API reference](api.md#authentication)).
