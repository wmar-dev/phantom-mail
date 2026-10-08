# Phantom Mail

A disposable inbox you run yourself, for testing sign-ups that email a verification code.

Send mail to **any address** at your domain (`anything@your.domain`) and read it straight away in
a web page or over a small JSON API. No accounts, no setup per address: a mailbox exists the moment
mail arrives for it. It only receives mail; it can never send any.

It is a single small program (a 3 MB container image, about 8 MB of memory when idle) with no
database and no dependencies beyond the Go standard library. You can run it on your laptop
and, with the same image, on a domain in the cloud.

> Mailboxes are public: anyone who knows a name can read it. Use it for test data only. An optional
> access token restricts the web interface and API.

## Quick start

```bash
docker compose up --build        # or: make run   (needs Go 1.23+)
```

Then send a test email and open <http://localhost:8080/#/alice>:

```bash verify
printf 'From: me@example.com\r\nTo: alice@localhost\r\nSubject: hello\r\n\r\nYour code is 482913\r\n' \
  | curl --url smtp://localhost:2525 --mail-from me@example.com --mail-rcpt alice@localhost -T -
```

That is the whole loop. The rest of the documentation covers each piece:

| I want to... | Read |
|--------------|------|
| Run it on my machine, with or without Docker; run the tests | [Running locally](local.md) |
| Put it on a domain on the internet and receive real email | [Deploying to the cloud](deployment.md) |
| Know every setting, default and limit | [Configuration reference](configuration.md) |
| Fetch mail or wait for a code from a script | [API reference](api.md) |
| Use the web page | [Web interface guide](web-ui.md) |
| Automate "sign up, read the code, continue" in tests | [Testing sign-ups with verification codes](testing-verification-flows.md) |
| Change the code, run benchmarks, add a setting or endpoint | [Development](development.md) |

## How it works in one paragraph

An SMTP receiver accepts mail for the domains in `PM_DOMAINS` and refuses everything else, so it
can never relay. Each message is written to a file and indexed in memory; a background sweep
removes mail after the retention period (24 hours by default). The HTTP server serves the API,
live updates over Server-Sent Events, and the web page. Everything is bounded: message size,
messages per mailbox, mailboxes, total bytes, and request rates are all capped and configurable.

## Guarantees and non-goals

- **Receive-only.** No outbound mail, no replies, no forwarding. A test fails the build if any
  code path could send mail.
- **Durable acceptance.** A message is written to disk and synced before the sender is told it was
  accepted, so a crash or restart does not lose accepted mail.
- **Single instance.** One process owns the data directory. There is no clustering.
- **No spam or virus filtering** beyond size and rate limits, and **no TLS for SMTP** in this
  version (HTTPS is provided by a proxy in front of the web port).
