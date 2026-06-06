# Getting Started

This guide walks you through running **Open Telecom Webhook Observability** locally
from scratch, including receiving your first Twilio webhook.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Docker | >= 27.x | Required |
| Docker Compose | >= 2.x | Required |
| Node.js | >= 22.x | Dev only |
| npm | >= 10.x | Dev only |

---

## Step 1 — Clone and configure

```bash
git clone https://github.com/FelipeBastosxj/open-telecom-webhook-observability.git
cd open-telecom-webhook-observability

cp .env.example .env
```

The default `.env.example` values work out-of-the-box for local development.
Only set `TWILIO_AUTH_TOKEN` if you want signature validation enabled.

---

## Step 2 — Start the infrastructure

```bash
npm run infra:up
```

This starts **PostgreSQL** (port 5432) and **Redis** (port 6379).

Verify:

```bash
docker ps
# telecom-webhook-postgres   Up
# telecom-webhook-redis      Up
```

The PostgreSQL schema is applied automatically via `infra/postgres/init.sql`
on first start. A default local-dev workspace is seeded with:

- **Workspace ID:** `a0000000-0000-0000-0000-000000000001`
- **Endpoint Token:** `dev-token-local-001`

---

## Step 3 — Start the services

```bash
npm run services:up
```

This builds Docker images and starts all three backend services.

Wait for the health checks to pass (~30 s), then verify:

```bash
curl http://localhost:3001/health   # ingestion-service
curl http://localhost:3003/health   # processing-service
curl http://localhost:3004/health   # realtime-gateway
```

All should return `{"status":"ok"}`.

---

## Step 4 — Start the frontend

```bash
npm run frontend
```

Open [http://localhost:4200](http://localhost:4200).

---

## Step 5 — Send your first webhook

### Option A — Simulate a Twilio webhook locally

```bash
# Incoming SMS
curl -X POST "http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123&From=%2B15017122661&To=%2B14155552671&Body=Hello+World&NumMedia=0"
```

```bash
# Message delivered callback
curl -X POST "http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123&MessageStatus=delivered&To=%2B14155552671&From=%2B15017122661"
```

Check the dashboard — events should appear in the live list within seconds.

### Option B — Connect your real Twilio account

1. In your Twilio Console, set the **Status Callback URL** (or *A Message Comes In*) to:
   ```
   https://<your-public-host>:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001
   ```
2. Use [ngrok](https://ngrok.com) or similar to expose your local port 3001 publicly:
   ```bash
   ngrok http 3001
   ```
3. Set `TWILIO_AUTH_TOKEN` in `.env` and restart the services to enable signature validation.

---

## Running in Development Mode (no Docker for services)

If you prefer to run the services directly with Node.js (hot reload):

```bash
# Terminal 1 — infrastructure only
npm run infra:up

# Terminal 2
npm run services:ingestion   # :3001

# Terminal 3
npm run services:processing  # :3003

# Terminal 4
npm run services:gateway     # :3004

# Terminal 5
npm run frontend             # :4200
```

---

## Stopping Everything

```bash
npm run services:down   # Stop app services
npm run infra:down      # Stop PostgreSQL + Redis
```

Full reset (removes all data):

```bash
npm run reset
```
