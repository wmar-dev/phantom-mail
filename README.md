# Phantom Mail

A self-hosted, receive-only disposable inbox for testing sign-ups that email you a verification
code. Send mail to any address at your domain, then read it in a web page or over a JSON API. No
accounts and no per-address setup.

- Web interface with live updates and one-click copy of verification codes
- JSON API with a "wait for the email" call built for automated tests
- One 3 MB container image, standard library only, about 8 MB of memory when idle
- Runs on your laptop (`docker compose up --build`) and, unchanged, on a domain in the cloud

```bash
docker compose up --build          # or: make run  (Go 1.23+)
# send a test message, then open http://localhost:8080/#/alice
```

Documentation starts at **[docs/index.md](docs/index.md)**:
[running locally](docs/local.md) ·
[cloud deployment](docs/deployment.md) ·
[configuration](docs/configuration.md) ·
[API](docs/api.md) ·
[testing sign-ups](docs/testing-verification-flows.md) (examples in Go, Python and Node.js) ·
[development](docs/development.md)

Mailboxes are public by design; use it for test data only.
