# Deploying to the cloud

This guide puts Phantom Mail on a public domain so that real websites can send it mail. The same
container image you run [locally](local.md) is used; only configuration changes.

- [How it fits together](#how-it-fits-together)
- [What you need](#what-you-need)
- [Step by step](#step-by-step)
- [HTTPS](#https)
- [Configuration for production](#configuration-for-production)
- [Operating it](#operating-it)
- [Verify the deployment (checklist)](#verify-the-deployment-checklist)
- [Troubleshooting](#troubleshooting)
- [Security notes](#security-notes)

## How it fits together

```text
 sender's mail server ──► your host, TCP 25  ──► container port 2525 ─┐
                                                                       ├─ phantom-mail ──► /data volume
 browser / scripts ──► HTTPS 443 ──► reverse proxy ──► container 8080 ─┘
```

One process serves two ports:

| Port (container) | Protocol | Publish as | Purpose |
|------------------|----------|------------|---------|
| `2525` | SMTP | host port **25**, to the internet | Inbound mail |
| `8080` | HTTP | **not** directly; put an HTTPS proxy in front | Web interface and API |

The container runs as a non-root user, so it cannot bind port 25 itself. It listens on 2525 and
you map host port 25 to it (`-p 25:2525`).

## What you need

1. **A host that can accept inbound TCP connections on port 25 from the internet** and run a
   container: a small virtual machine with a public IP address is the simplest. Check this before
   anything else:
   - Many platforms that only route HTTP(S), typically serverless container services and PaaS
     offerings, cannot expose a raw TCP port 25. Pick one that can.
   - Some providers and home ISPs block port 25. Blocking *outbound* 25 is common and harmless
     here, because this service never sends mail. *Inbound* 25 must work. If a provider needs a
     request to unblock it, do that first.
2. **A domain you control** where you can create DNS records. You can use a subdomain such as
   `inbox.example.com` so your normal email for `example.com` is unaffected. Do not point the
   MX record of a domain whose real email you use at this service.
3. **Docker** on the host, and the image. Build it from this repository and push it to a registry
   you use, or build it on the host:

   ```bash
   docker build --target runtime -t phantom-mail:local .
   ```

## Step by step

### 1. Create DNS records

Using `inbox.example.com` as the mail domain and `203.0.113.10` as the host's IP address:

| Name | Type | Value | Why |
|------|------|-------|-----|
| `inbox.example.com` | `MX` | `10 inbox.example.com.` | Tells senders where to deliver mail for `anything@inbox.example.com` |
| `inbox.example.com` | `A` | `203.0.113.10` | The MX host must resolve to an address (an `MX` must point at a name that has an `A`/`AAAA` record, never at a bare IP or a `CNAME`) |

The same name can serve the web interface. If you prefer a separate web host name
(for example `mail.example.com`), add an `A` record for it as well. Add `AAAA` records too if the
host has IPv6 and its firewall allows it.

Check from another machine once DNS has propagated:

```bash
dig +short MX inbox.example.com     # 10 inbox.example.com.
dig +short A inbox.example.com      # 203.0.113.10
```

### 2. Open the firewall

Allow inbound TCP **25** (mail) and **80/443** (web, for the HTTPS proxy) in the host firewall and
in the provider's network security group.

### 3. Start the service

The quickest working layout is the example Compose file in this repository, which runs Phantom
Mail plus Caddy for HTTPS ([docs/examples/compose.prod.yaml](examples/compose.prod.yaml) and
[docs/examples/Caddyfile](examples/Caddyfile)). Replace `inbox.example.com` in both, then:

```bash
export PM_API_TOKEN="$(openssl rand -hex 24)"     # optional but recommended, see Security notes
docker compose -f docs/examples/compose.prod.yaml up -d
```

Or run the container yourself behind a proxy you already have:

```bash
docker run -d --name phantom-mail --restart unless-stopped --stop-timeout 30 \
  -p 25:2525 \
  -p 127.0.0.1:8080:8080 \
  -v phantom-mail-data:/data \
  -e PM_DOMAINS=inbox.example.com \
  -e PM_API_TOKEN="$PM_API_TOKEN" \
  -e PM_TRUSTED_PROXIES=127.0.0.1 \
  phantom-mail:local
```

`PM_DOMAINS` must be the domain used in the addresses. Mail for any other domain is refused.

### 4. Check it

Follow the [verification checklist](#verify-the-deployment-checklist) below.

## HTTPS

The application speaks plain HTTP; terminate HTTPS in front of it with a reverse proxy or your
platform's load balancer. This keeps certificate renewal out of the application.

Two requirements apply to whatever proxy you use:

1. **Do not buffer responses.** Live updates use a long-lived event stream
   (`/api/v1/mailboxes/{mailbox}/events`); a proxy that buffers it makes the page look stuck on
   "Reconnecting...". In Caddy this is `flush_interval -1`; in nginx use `proxy_buffering off;`
   (and `proxy_read_timeout 1h;`) for `/api/`.
2. **Tell the app which proxies to trust.** Set `PM_TRUSTED_PROXIES` to the address (or CIDR
   range) the proxy connects from, so the app uses the real client address for rate limiting and
   marks the sign-in cookie `Secure`. Without it, every client looks like the proxy and shares
   one rate-limit budget. Never trust a range that untrusted clients can reach.

### Mail is not encrypted in transit

The SMTP receiver does not offer STARTTLS in this version. Almost all senders fall back to plain
SMTP, which is acceptable for the throwaway test mail this service is for. A sender that insists
on TLS (rare) will fail to deliver.

## Configuration for production

The settings that usually matter are below; the full list is in
[configuration](configuration.md).

| Setting | Suggested | Notes |
|---------|-----------|-------|
| `PM_DOMAINS` | your mail domain(s) | required in practice |
| `PM_API_TOKEN` | random string | restricts the web interface and API; mail is unaffected |
| `PM_TRUSTED_PROXIES` | your proxy's address/CIDR | see HTTPS above |
| `PM_RETENTION` | `24h` (default) or less | shorter means less data held |
| `PM_MAX_TOTAL_BYTES` | fit to the disk | oldest mail is dropped when full |
| `PM_RATE_*` | defaults | tighten if the instance is abused |

Data lives in the `/data` volume (`PM_DATA_DIR`). Use a persistent volume so mail survives
container replacement. Run **one instance only**: the store is a directory, not a shared database.

## Operating it

- **Logs.** One JSON object per line on standard output; read them with `docker logs`. Accepted
  mail logs sender and size but never message content or secrets.
- **Health.** `GET /healthz` returns `{"status":"ok",...}`; the image also defines a container
  `HEALTHCHECK` (`phantom-mail healthcheck`), so `docker ps` shows `healthy`. Use `/healthz` for
  load balancer and orchestrator probes.
- **Updating.** Pull or build the new image and recreate the container. On `SIGTERM` the service
  stops accepting new connections, lets any message being received finish, closes live streams
  and exits (allow 30 seconds: `--stop-timeout 30`, `stop_grace_period: 30s`, or
  `terminationGracePeriodSeconds: 30` in Kubernetes). Stored mail is on the volume and is
  available again immediately.
- **Kubernetes.** Run a single replica with the `Recreate` strategy, a PersistentVolumeClaim at
  `/data`, a `LoadBalancer`/`NodePort` Service exposing TCP 25 → 2525, an Ingress for HTTP 8080,
  and an HTTP readiness/liveness probe on `/healthz`.
- **Disk.** Bounded by retention, the per-mailbox cap, `PM_MAX_MAILBOXES` and `PM_MAX_TOTAL_BYTES`;
  when the volume itself fills up, senders receive a temporary failure (`452`) and retry later.

## Verify the deployment (checklist)

Do these in order; they take a few minutes and prove the whole path, including a real external
sender (this is the manual part of the "receive a real email on my own domain" success criterion).

- [ ] `dig +short MX inbox.example.com` shows your host.
- [ ] From a machine *outside* your network: `nc -vz inbox.example.com 25` connects, and
      `printf 'EHLO test\r\nQUIT\r\n' | nc inbox.example.com 25` prints
      `220 inbox.example.com ESMTP phantom-mail ready`.
- [ ] `curl -s https://inbox.example.com/healthz` returns `"status":"ok"`.
- [ ] Open `https://inbox.example.com/#/smoke-test` in a browser (sign in with the token if you set
      one). The page shows **Live**.
- [ ] Send a real email from an ordinary mail account (Gmail, Outlook, ...) to
      `smoke-test@inbox.example.com`. It appears in the page within a few seconds.
- [ ] Sign up on an external site using `anything@inbox.example.com` and read the verification
      code in the web interface or with the [API](api.md).
- [ ] Restart the container (`docker restart phantom-mail`); the messages are still there.

Record how long the whole procedure took the first time; it should be well under an hour,
mostly waiting for DNS.

## Troubleshooting

| Symptom | What to check |
|---------|---------------|
| No mail ever arrives | `dig MX` shows the right host; `nc -vz host 25` works from outside; the firewall and provider security group allow inbound 25; the container publishes `25:2525`. |
| Sender reports `550 Relay access denied` | The recipient domain is not in `PM_DOMAINS` (compare exactly, case-insensitively). Logs show `recipient rejected ... relay_denied`. |
| Sender reports a temporary error (`4xx`) | `452`: disk full or mailbox limit reached; `451`/`421`: rate limited. Senders retry automatically; see the logs for the reason. |
| Some senders never deliver | The sender requires TLS for SMTP; this version has no STARTTLS. |
| Page loads but shows "Reconnecting..." | The proxy is buffering the event stream; see [HTTPS](#https). |
| Everyone gets `429` quickly | Behind a proxy without `PM_TRUSTED_PROXIES`, all clients share one address. |
| Session cookie lacks the `Secure` flag behind an HTTPS proxy | `PM_TRUSTED_PROXIES` does not include the proxy's address, so its `X-Forwarded-Proto: https` header is ignored. |
| `permission denied` writing `/data` | The volume must be writable by user `65532` (Docker named volumes are; with bind mounts run `chown 65532:65532 dir`). |
| Container restarts at startup | `docker logs` prints `configuration error: PM_...` naming the invalid setting. |
| Wrong "received" times | The host clock is wrong; enable NTP. |

## Security notes

- **Mailboxes are public by design.** Anyone who knows or guesses a name can read its mail. Use
  the service only for non-sensitive test data. Setting `PM_API_TOKEN` restricts reading and
  deleting to people who have the token, but mail is still accepted for any name.
- **It cannot be used to send mail.** It has no code that sends email, refuses recipients at
  other domains, offers no `AUTH`, and never relays or bounces; a test enforces this.
- **Treat stored mail as untrusted.** HTML is shown in a sandboxed frame with a restrictive policy,
  and attachments are download-only.
- **Limits protect availability.** Message size, per-mailbox and total storage caps, a mailbox
  count cap, and per-client rate limits bound what a flood can do. They are configurable.
  The SMTP receiver also caps concurrent sessions (1,000), commands per session (1,000),
  recipients per message (100), command line length (4 KiB) and idle time (60 s per command), and
  the parser ignores MIME parts beyond the first 1,000 in a message.
- **You are holding other people's mail.** If your instance is public, keep the retention short and
  consider your local rules about handling third-party email.
