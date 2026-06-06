# Open Telecom Webhook Observability

> Open source webhook inspector and observability platform for telecom providers.  
> Debug Twilio webhooks — and more — in real time.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

---

## What is this?

A free, self-hostable alternative to Webhook.site — purpose-built for **telecom webhooks** (SMS, WhatsApp, Voice).

**Receive → Inspect → Analyse → Debug.**

| Capability | Details |
|---|---|
| 🔌 Webhook Receiver | Unique URL per workspace — no configuration needed |
| 🔍 Real-time Inspector | Live event stream via WebSocket |
| 📋 Full Payload View | Raw headers + body, formatted and searchable |
| 📈 Event Timeline | Group SMS/call events by MessageSid / CallSid |
| 📊 Metrics Dashboard | Delivery rates, failure rates, volume per provider |
| 🔎 Search & Filter | By type, status, number, SID, time range, payload text |

---

## Quick Start

> **Only Docker is required.**

```bash
# 1. Clone
git clone https://github.com/FelipeBastosxj/open-telecom-webhook-observability.git
cd open-telecom-webhook-observability

# 2. Environment
cp .env.example .env

# 3. Start PostgreSQL + Redis
npm run infra:up

# 4. Build and start all services
npm run services:up

# 5. Open the dashboard
open http://localhost:4200
```

---

## Service URLs

| Service | URL | Description |
|---|---|---|
| Angular Dashboard | http://localhost:4200 | Webhook Inspector UI |
| Ingestion Service | http://localhost:3001 | `POST /:workspaceId/:token` |
| Processing Service | http://localhost:3003 | Internal event processor |
| Realtime Gateway | http://localhost:3004 | WebSocket `/ws` |

---

## Send a Twilio Webhook

Point your Twilio Status Callback URL to:
```
http://your-host:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001
```

Or simulate one locally:

```bash
# Incoming SMS
curl -X POST "http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123456&From=%2B15017122661&To=%2B14155552671&Body=Hello+World&NumMedia=0"

# Message Status Callback
curl -X POST "http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123456&MessageStatus=delivered&To=%2B14155552671&From=%2B15017122661"

# Voice Status Update
curl -X POST "http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "CallSid=CA123456&CallStatus=completed&To=%2B14155552671&From=%2B15017122661&Duration=42"
```

---

## Stop Everything

```bash
npm run infra:down
```

Full reset (wipes database volumes):

```bash
npm run reset
```

---

## Prerequisites

| Tool | Version | Required |
|---|---|---|
| Docker | >= 27.x | ✅ |
| Docker Compose | >= 2.x | ✅ |
| Node.js | >= 22.x | ⚙️ Dev only |
| npm | >= 10.x | ⚙️ Dev only |

---

## Architecture

```
Telecom Providers (Twilio, Vonage, ...)
        │
        ▼  POST /:workspaceId/:token
  ingestion-service     ← validates token, captures headers + payload
        │
        ▼  HTTP (direct)
  processing-service    ← normalises to WebhookEvent, writes to PostgreSQL
        │                  emits NOTIFY via pg_notify
        ▼
    PostgreSQL           ← source of truth for all events
        │
        ▼  LISTEN (pg_notify)
  realtime-gateway      ← broadcasts WebSocket events to connected clients
        │
        ▼  WebSocket
  Angular Dashboard     ← live event stream, timeline, metrics
```

**No Kafka. No ClickHouse. No Prometheus stack.** Just PostgreSQL, Redis, and Node.js services.

---

## Available Commands

```bash
# Infrastructure
npm run infra:up          # Start PostgreSQL + Redis
npm run infra:down        # Stop infrastructure
npm run infra:reset       # Stop and remove all volumes
npm run infra:logs        # Tail infrastructure logs

# Services (Docker)
npm run services:up       # Build + start all services
npm run services:down     # Stop all services
npm run services:build    # Rebuild service images
npm run services:logs     # Tail service logs

# Services (local dev)
npm run services:ingestion   # Start ingestion-service  (:3001)
npm run services:processing  # Start processing-service (:3003)
npm run services:gateway     # Start realtime-gateway   (:3004)

# Frontend
npm run frontend          # Start Angular dashboard (:4200)
npm run frontend:build    # Build Angular for production

# Build / Lint / Test
npm run build             # Build all packages
npm run lint              # Lint all packages
npm run test              # Run all tests

# Utilities
npm run env               # Copy .env.example → .env
npm run reset             # Full reset (stop + remove volumes)
npm run help              # List all available commands
```

---

## WebhookEvent Model

```typescript
interface WebhookEvent {
  id:           string;              // UUID v4 — server-assigned
  workspaceId:  string;              // Which workspace received it
  provider:     'twilio' | string;   // Telecom provider
  eventType:    string;              // e.g. 'message.inbound', 'call.status.completed'
  receivedAt:   Date;                // UTC timestamp at HTTP layer

  headers: Record<string, string>;   // All HTTP request headers
  payload: Record<string, unknown>;  // Full parsed request body

  // Extracted telecom fields (indexed for filtering)
  messageSid?:  string;
  callSid?:     string;
  from?:        string;              // E.164 origin number
  to?:          string;              // E.164 destination number
  status?:      string;              // Delivery / call status
}
```

---

## Provider Support

| Provider | SMS | Voice | Status Callbacks |
|---|---|---|---|
| Twilio | ✅ | ✅ | ✅ |
| Vonage | 🔜 Phase 2 | 🔜 Phase 2 | 🔜 Phase 2 |
| MessageBird | 🔜 Phase 2 | - | 🔜 Phase 2 |
| Infobip | 🔜 Phase 2 | - | 🔜 Phase 2 |
| Plivo | 🔜 Phase 2 | 🔜 Phase 2 | 🔜 Phase 2 |

---

## Documentation

| Document | Description |
|---|---|
| [docs/getting-started.md](./docs/getting-started.md) | Detailed setup guide |
| [docs/architecture.md](./docs/architecture.md) | Architecture decisions |
| [docs/event-model.md](./docs/event-model.md) | WebhookEvent model reference |
| [docs/integrations.md](./docs/integrations.md) | Provider integration guides |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | How to contribute |
| [ROADMAP.md](./ROADMAP.md) | Phased delivery plan |

---

## Contributing

Contributions are very welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md) for workflow, standards, and the definition of done.

Good first issues are labelled [`good first issue`](https://github.com/FelipeBastosxj/open-telecom-webhook-observability/issues?q=is%3Aopen+label%3A%22good+first+issue%22).

---

## License

[MIT](LICENSE)

