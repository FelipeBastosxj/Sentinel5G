# Getting started

## Prerequisites

| Requirement | Notes |
|---|---|
| Docker Desktop (Windows/macOS) or Docker Engine + Compose plugin (Linux) | Docker 24+ recommended |
| Git | Any recent version |

> Node.js is **not** required on your host machine — everything runs inside Docker.

---

## 1. Clone the repository

```bash
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine
```

---

## 2. Configure environment

```bash
cp .env.example .env
```

Defaults work out of the box. You only need to change values if you want different ports or credentials.

Key variables:

| Variable | Default | Description |
|---|---|---|
| `POSTGRES_PASSWORD` | `webhook_pass` | PostgreSQL password |
| `INGESTION_PORT` | `3001` | Public webhook receiver port |
| `PROCESSING_PORT` | `3003` | Processing API port |
| `GATEWAY_PORT` | `3004` | WebSocket gateway port |
| `FRONTEND_PORT` | `4200` | Angular dashboard port |

---

## 3. Start all services

```bash
npm run up
```

This runs `docker compose --profile services up -d --build`.

First run: Docker builds images — allow **2–3 minutes**.

### Verify containers are healthy

```bash
docker ps
```

All six containers must show `(healthy)`:

```
telecom-webhook-frontend    Up (healthy)
telecom-webhook-ingestion   Up (healthy)
telecom-webhook-processing  Up (healthy)
telecom-webhook-gateway     Up (healthy)
telecom-webhook-postgres    Up (healthy)
telecom-webhook-redis       Up (healthy)
```

---

## 4. Open the dashboard

Navigate to **http://localhost:4200**.

On first visit the app:
1. Generates a random UUID as your `endpointToken`, stored in `localStorage`
2. Calls `POST /workspace/auto` on the processing-service to provision a workspace
3. Shows your webhook URL: `http://localhost:3001/{workspaceId}/{token}`

---

## 5. Expose your webhook to the internet

Twilio requires a public HTTPS URL. Use `cloudflared` (free, no account required):

**Windows — run from WSL:**

```bash
bash expose.sh
```

**macOS / Linux:**

```bash
bash expose.sh
```

The script downloads the cloudflared binary if not present, then runs:

```
cloudflared tunnel --url http://localhost:3001
```

It prints a URL like `https://example.trycloudflare.com`. Keep the terminal open.

> The tunnel URL changes on every restart. Update Twilio when you restart.

---

## 6. Configure Twilio

1. Go to [console.twilio.com](https://console.twilio.com)
2. **Phone Numbers → Manage → Active Numbers → your number**
3. **Messaging → A message comes in:**
   - URL: `https://<tunnel-url>/<workspaceId>/<token>`
   - Method: `HTTP POST`
4. Optionally set the same URL as **Status Callback URL** for delivery events

> Copy `<workspaceId>` and `<token>` from the webhook URL shown in the dashboard header.

---

## 7. Test it

Send an SMS to your Twilio phone number. The event appears in the dashboard at **http://localhost:4200** within ~1 second.

Or test with curl:

```bash
curl -X POST "http://localhost:3001/<workspaceId>/<token>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -H "x-twilio-signature: test" \
  -d "MessageSid=SM123&From=%2B15551234567&To=%2B15559876543&Body=Hello&AccountSid=AC000"
```

---

## Useful commands

```bash
npm run logs                              # tail all service logs
docker logs -f telecom-webhook-ingestion  # single service
npm run down                              # stop containers
npm run down -- -v                        # full reset (clears DB)
npm run restart                           # rebuild + restart
```

---

## Troubleshooting

### Container stuck in `(starting)` or `(unhealthy)`

```bash
docker logs telecom-webhook-processing
```

Common causes: `DATABASE_URL` misconfigured, or PostgreSQL not ready yet — wait 10 s.

### `POST /workspace/auto` returns 500

```bash
docker logs telecom-webhook-processing
docker logs telecom-webhook-postgres
```

### Dashboard shows `localhost:3001` in the webhook URL

Expected — the Angular build hardcodes `localhost:3001`. Replace it manually with your cloudflare tunnel URL when configuring Twilio.

### cloudflared shows 502 Bad Gateway (Windows)

Run `expose.sh` from **WSL**, not from PowerShell or CMD. Docker containers are reachable from WSL but not from the Windows network stack directly.
