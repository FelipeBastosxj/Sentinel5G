# Telecom Webhook Inspector

A self-hosted, open-source platform for receiving, inspecting, and debugging webhooks from Twilio and other telecom providers in real time.

Think **webhook.site**, but purpose-built for SMS and voice workflows — with a live event dashboard, per-workspace URLs, and full payload history stored in PostgreSQL.

---

## What it does

- Generates a **unique webhook URL** per browser session (`/{workspaceId}/{token}`)
- Accepts `POST` from Twilio (SMS inbound, delivery status callbacks, voice events)
- **Normalises** raw payloads into a structured `WebhookEvent` model
- **Persists** every event to PostgreSQL with full HTTP headers + raw body
- **Pushes live updates** to the Angular dashboard via WebSocket (Socket.IO)
- Real-time fanout across multiple gateway instances via Redis pub/sub

---

## Architecture

```
Browser (Angular :4200)
    |  WebSocket (Socket.IO)
    +-----------------------------------------+
                                              |
Twilio -POST-> ingestion-service:3001 -HTTP-> processing-service:3003
               rate-limit · capture            |
                                         normalise + INSERT
                                              |
                                       PostgreSQL:5432
                                              |
                                         pg_notify
                                              |
                                  realtime-gateway:3004
                                  Socket.IO · Redis fanout
```

| Container | Port | Role |
|---|---|---|
| `frontend` | 4200 | Angular dashboard (nginx) |
| `ingestion-service` | 3001 | Webhook receiver — all traffic enters here |
| `processing-service` | 3003 | Normalisation, persistence, workspace API |
| `realtime-gateway` | 3004 | WebSocket server + Redis pub/sub |
| `postgres` | 5432 | Primary event store |
| `redis` | 6379 | Rate limiting + multi-instance broadcast |

---

## Quick start

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (Windows/macOS) or Docker Engine + Compose plugin (Linux)
- Ports **3001, 3003, 3004, 4200, 5432, 6379** available locally

### 1. Clone and start

```bash
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine

cp .env.example .env        # defaults work out of the box

npm run up                  # builds Docker images and starts all services
```

> First run builds Docker images — allow **2–3 minutes**.

### 2. Open the dashboard

Navigate to **http://localhost:4200**.

The app provisions a workspace on first visit and shows your webhook URL in the header banner.

### 3. Expose to the internet (Twilio testing)

Run from a **WSL** or Linux terminal:

```bash
bash expose.sh
```

Downloads [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/) and creates a free HTTPS tunnel to port 3001. Copy the printed URL.

> The tunnel URL changes every restart. Update Twilio accordingly.

### 4. Configure Twilio

In [console.twilio.com](https://console.twilio.com) → **Phone Numbers → Active Numbers → your number → Messaging**:

| Field | Value |
|---|---|
| A message comes in | `https://<tunnel-url>/<workspaceId>/<token>` |
| HTTP method | `HTTP POST` |
| Message Status Callback | same URL |

Send an SMS to your Twilio number — event appears in the dashboard in ~1 second.

---

## npm scripts

| Script | Description |
|---|---|
| `npm run up` | Build images and start all services in background |
| `npm run down` | Stop and remove containers |
| `npm run logs` | Tail logs for all services |
| `npm run restart` | Full rebuild and restart |

---

## Health checks

```bash
curl http://localhost:3001/health   # { "status": "ok", "uptimeSeconds": N }
curl http://localhost:3003/health
curl http://localhost:3004/health
```

---

## Supported Twilio events

| Event | `eventType` |
|---|---|
| Inbound SMS / WhatsApp | `message.inbound` |
| Delivery status (sent, delivered, failed, …) | `message.status.<status>` |
| Inbound call | `call.inbound` |
| Outbound call | `call.outbound` |
| Call status | `call.status.<status>` |

---

## Project layout

```
services/
  ingestion-service/   # HTTP receiver
  processing-service/  # Normalisation, persistence, workspace API
  realtime-gateway/    # Socket.IO + Redis fanout

shared/
  contracts/           # TypeScript interfaces
  utils/               # Logger, UUID, correlation ID, backoff

frontend/
  angular-dashboard/   # Angular 17 standalone SPA

infra/
  docker-compose.yml
  postgres/init.sql
  nginx/angular.conf
```

---

## Docs

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Service design, data flow, module breakdown |
| [Event model](docs/event-model.md) | `WebhookEvent` interface + Twilio field mapping |
| [Getting started](docs/getting-started.md) | Detailed setup + Twilio configuration |
| [Integrations](docs/integrations.md) | Adding a new provider normaliser |
| [Contributing](CONTRIBUTING.md) | Development workflow and standards |
| [Roadmap](ROADMAP.md) | Planned features by phase |

---

## License

MIT — see [LICENSE](LICENSE).
