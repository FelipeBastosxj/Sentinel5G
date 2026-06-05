# Architecture

This document describes the architecture of the **EventStream Observability Engine**.

---

## System Overview

```text
External Systems (Twilio, Infobip, SendGrid, Custom)
        │
        ▼  POST /integrations/{provider}/webhook
┌─────────────────────┐
│   Webhook Service   │  Validates signatures, normalizes payloads
└─────────────────────┘
        │ CanonicalEvent
        ▼
┌─────────────────────┐
│  Ingestion Service  │  Rate limiting, schema validation
└─────────────────────┘
        │ Kafka: events.raw
        ▼
┌─────────────────────┐
│ Processing Service  │  Event enrichment, retry logic, DLQ
└─────────────────────┘
        │ Kafka: events.processed
        ├─────────────────────────────────────┐
        ▼                                     ▼
┌─────────────────────┐           ┌─────────────────────┐
│     ClickHouse      │           │  Realtime Gateway   │
│  (Historical Data)  │           │  (WebSocket Server) │
└─────────────────────┘           └─────────────────────┘
        │                                     │
        ▼                                     ▼
  REST API Queries                  Angular Dashboard
```

---

## Services

### Webhook Service (`services/webhook-service`)

Entry point for all external provider integrations.

Responsibilities:
- Receive provider-specific payloads via HTTP POST
- Validate webhook signatures (HMAC, token-based)
- Transform provider payloads into `CanonicalEvent`
- Forward to Ingestion Service

Endpoints:
- `POST /integrations/twilio/webhook`
- `POST /integrations/infobip/webhook`
- `POST /integrations/sendgrid/webhook`
- `POST /integrations/custom/webhook`

---

### Ingestion Service (`services/ingestion-service`)

Internal event ingestion pipeline.

Responsibilities:
- Apply rate limiting to all ingestion endpoints
- Validate `CanonicalEvent` schema
- Publish to Kafka topic `events.raw`
- Assign correlation IDs if not present

---

### Processing Service (`services/processing-service`)

Kafka consumer that drives the core event processing pipeline.

Responsibilities:
- Consume from `events.raw`
- Enrich events with metadata
- Handle retries with exponential backoff
- Route failed events to Dead Letter Queue (`events.alerts`)
- Publish processed events to `events.processed`
- Publish metrics to `events.metrics`

---

### Realtime Gateway (`services/realtime-gateway`)

WebSocket server that bridges Kafka to the frontend dashboard.

Responsibilities:
- Consume from `events.processed` and `events.metrics`
- Push events to connected WebSocket clients in real time
- Support multi-instance deployment via Redis pub/sub

---

## Kafka Topics

| Topic | Producer | Consumer | Purpose |
|-------|----------|----------|---------|
| `events.raw` | Ingestion Service | Processing Service | Raw normalized events |
| `events.processed` | Processing Service | Realtime Gateway, ClickHouse Writer | Processed events |
| `events.metrics` | Processing Service | Realtime Gateway | Aggregated metrics |
| `events.alerts` | Processing Service | Alerting (future) | Dead Letter Queue |

---

## Infrastructure

| Service | Port | Purpose |
|---------|------|---------|
| Kafka | 9092 | Event streaming (external) |
| Kafka | 29092 | Event streaming (internal docker) |
| Kafka UI | 8080 | Web management console |
| Zookeeper | 2181 | Kafka coordination |
| Redis | 6379 | Cache and pub/sub |
| ClickHouse | 8123 | Analytical queries (HTTP) |
| ClickHouse | 9000 | Analytical queries (native) |

---

## Shared Packages

| Package | Purpose |
|---------|---------|
| `@eventstream/contracts` | TypeScript interfaces — `CanonicalEvent`, `EventType` |
| `@eventstream/schemas` | Validation schemas |
| `@eventstream/utils` | UUID generation, date helpers, correlation ID utilities |

---

## Clean Architecture Layers (per service)

```text
Controller       ← HTTP / WebSocket / Kafka handlers — no business logic
    │
Application      ← Use cases, orchestration
    │
Domain           ← Entities, interfaces, business rules — no framework deps
    │
Infrastructure   ← Kafka, Redis, ClickHouse, HTTP clients
```
