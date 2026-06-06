# Architecture

This document describes the architecture of **Open Telecom Webhook Observability**.

---

## Design Philosophy

**Simple by default. Scalable by choice.**

The MVP uses the simplest stack that can deliver the full feature set:

- **PostgreSQL** as the single source of truth
- **Redis** for rate limiting and real-time pub/sub fanout
- **WebSockets** for live event delivery to the browser
- No Kafka, no ClickHouse, no distributed tracing stack in the base setup

Complexity (Kafka, multi-region ingestion, etc.) is deferred to Phase 5 when actual scale demands it.

---

## System Overview

```
Telecom Providers (Twilio, Vonage, ...)
        │
        ▼  POST /:workspaceId/:endpointToken
┌──────────────────────┐
│  ingestion-service   │  Token validation · header + body capture · rate limiting
└──────────────────────┘
        │  HTTP POST (sync, internal)
        ▼
┌──────────────────────┐
│ processing-service   │  Provider normalisation → WebhookEvent · PostgreSQL write
└──────────────────────┘  pg_notify('webhook_events', event_id)
        │
        ▼
┌──────────────────────┐
│     PostgreSQL       │  Source of truth: workspaces, webhook_events, event_timelines
└──────────────────────┘
        │  LISTEN 'webhook_events'
        ▼
┌──────────────────────┐
│  realtime-gateway    │  WebSocket server · broadcasts new events to subscribed clients
└──────────────────────┘
        │  WebSocket
        ▼
┌──────────────────────┐
│  Angular Dashboard   │  Live inspector · timeline · metrics · search
└──────────────────────┘
```

---

## Services

### `ingestion-service` (port 3001)

Entry point for all inbound webhooks.

**Responsibilities:**
- Accept `POST /:workspaceId/:endpointToken`
- Validate the workspace token against PostgreSQL
- Capture all HTTP headers and the raw body verbatim
- Apply rate limiting (Redis token bucket)
- Forward the raw request to `processing-service`

**Does NOT:**
- Parse provider-specific fields
- Write to the database directly

---

### `processing-service` (port 3003)

Normalisation and persistence layer.

**Responsibilities:**
- Receive raw captures from `ingestion-service`
- Detect the provider from request headers / body shape
- Normalise to `WebhookEvent` (see `shared/contracts`)
- Write to `webhook_events` table
- Emit `pg_notify('webhook_events', json)` for real-time fanout
- Expose `GET /events`, `GET /events/:id`, `GET /timelines/:threadId`, `GET /metrics`

---

### `realtime-gateway` (port 3004)

Real-time event delivery to browser clients.

**Responsibilities:**
- Maintain `LISTEN webhook_events` connection to PostgreSQL
- On `NOTIFY`, broadcast the `WebhookEventSummary` to all WebSocket clients subscribed to that workspace
- Redis pub/sub for horizontal gateway scaling (multiple instances)

---

### Angular Dashboard (port 4200)

Frontend inspection UI.

**Features:**
- Live event list (WebSocket subscription)
- Event detail panel: raw headers, formatted payload, processing time
- Timeline view: all events grouped by `MessageSid` or `CallSid`
- Metrics dashboard: counts, delivery rate, failure rate, volume per provider
- Search & filter: by type, status, from/to numbers, SID, time range, payload text

---

## Database Schema

See `infra/postgres/init.sql` for the full schema.

Key tables:

| Table | Purpose |
|---|---|
| `workspaces` | Tenant configuration, endpoint tokens |
| `webhook_events` | All received events, raw headers + payload (JSONB) |

Key view:

| View | Purpose |
|---|---|
| `event_timelines` | Groups events by `MessageSid` / `CallSid` thread |

---

## Webhook URL Structure

```
https://<host>/<workspaceId>/<endpointToken>
```

Example (local dev):
```
http://localhost:3001/a0000000-0000-0000-0000-000000000001/dev-token-local-001
```

---

## Sequence: Twilio Status Callback

```
Twilio
  │  POST /:workspaceId/:token  (MessageStatus=delivered)
  ▼
ingestion-service
  │  validates token
  │  captures headers + body
  │  POST /internal/process  →  processing-service
  ▼
processing-service
  │  detects provider: twilio
  │  normalises: eventType=message.status.delivered, messageSid=SM…, status=delivered
  │  INSERT INTO webhook_events
  │  pg_notify('webhook_events', { id, workspaceId, eventType, … })
  ▼
realtime-gateway (LISTEN)
  │  receives notify
  │  broadcasts to WebSocket clients subscribed to workspaceId
  ▼
Angular Dashboard
  │  appends event to live list
  │  updates metrics counters
```
