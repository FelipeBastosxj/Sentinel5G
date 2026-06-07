# Architecture

## Overview

Three NestJS microservices communicate via direct HTTP and PostgreSQL notify. No message broker — events flow synchronously through ingestion → processing, then fan out to WebSocket clients via `pg_notify` + Redis pub/sub.

```
Browser (Angular :4200)
    |  WebSocket (Socket.IO)                |  REST
    +---------------------------------------+---------------------------+
                                                                        |
Twilio -POST-> ingestion-service:3001 -POST /internal/process-> processing-service:3003
               (rate-limit, capture)                                    |
                                                                  normalise
                                                                  INSERT
                                                                  pg_notify
                                                                        |
                                                               PostgreSQL:5432
                                                                        |
                                                          LISTEN webhook_events
                                                                        |
                                                        realtime-gateway:3004
                                                        Socket.IO rooms · Redis fanout
```

---

## ingestion-service (port 3001)

**Single responsibility:** Accept all inbound webhook HTTP requests and forward them raw to the processing-service. Has no database access.

### Endpoint

```
@All() /:workspaceId/:endpointToken
```

Accepts any HTTP method. Returns `202 Accepted` immediately.

### Provider detection

| Header | Detected provider |
|---|---|
| `x-twilio-signature` | `twilio` |
| `x-vonage-signature` | `vonage` |
| `messagebird-signature-jwt` | `messagebird` |
| _(none)_ | `unknown` |

### Modules

| Module | What it does |
|---|---|
| `IngestionModule` | `IngestController` → `IngestEventUseCase` → `ProcessingForwarderAdapter` |
| `CorrelationModule` | Attaches `X-Correlation-ID` to every request via `AsyncLocalStorage` |
| `LoggerModule` | Structured JSON logger with correlation ID injection |
| `MetricsModule` | In-process counters at `GET /metrics` |
| `HealthModule` | `GET /health` |

---

## processing-service (port 3003)

**Single responsibility:** Normalise raw captures, persist to PostgreSQL, notify the realtime-gateway, and serve the REST API.

### HTTP endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/internal/process` | Internal — raw capture from ingestion |
| `POST` | `/workspace/auto` | Idempotent workspace provisioning |
| `GET` | `/events/recent` | Last N events (all workspaces) |
| `GET` | `/events/workspace/:id` | Events for a workspace |
| `GET` | `/health` | Health check |

### Normalisation pipeline

1. Receive `ProcessCommand` (`provider`, `headers`, `body`, `receivedAt`)
2. Route to `TwilioNormalizer` (or identity passthrough for unknown providers)
3. Extract `messageSid`, `callSid`, `from`, `to`, `status`, `eventType`
4. `INSERT INTO webhook_events`
5. `SELECT pg_notify('webhook_events', json)` — triggers realtime-gateway

### Database

A single `pg.Client` provided as `DATABASE_CLIENT` token via `DatabaseModule`. Connects once on module init, disconnects on shutdown.

---

## realtime-gateway (port 3004)

**Single responsibility:** Fan out events from PostgreSQL to connected WebSocket clients.

### Flow

```
PgListenerAdapter
  LISTEN webhook_events (dedicated pg.Client)
      |
      v BroadcastEventUseCase.broadcastFromLocal()
          |
          +---> EventsGateway (Socket.IO)
          |      broadcast to room named by workspaceId
          |
          +---> RedisFanoutAdapter.publish()
                 channel: telecom-webhook.gateway

RedisFanoutAdapter (subscriber)
  Receives events from peer gateway instances
      |
      v BroadcastEventUseCase.broadcastFromRemote()
          +---> EventsGateway (broadcast to local Socket.IO clients)
```

### Socket.IO protocol

```
Client → server:   subscribe  { workspaceId: string }
Server → client:   webhook_event  WebhookEventSummary
```

### Modules

| Module | What it does |
|---|---|
| `RealtimeModule` | `PgListenerAdapter` + `RedisFanoutAdapter` + `EventsGateway` |
| `CommonInfraModule` | Correlation service + structured logger |
| `ObservabilityModule` | `GET /metrics` + `GET /health` |

---

## Shared packages

### `@telecom-webhook/contracts`

| Export | Description |
|---|---|
| `WebhookEvent` | Full normalised event (persisted to DB) |
| `WebhookEventSummary` | Lightweight projection broadcast over WebSocket |
| `TelecomProvider` | `'twilio' \| 'vonage' \| 'messagebird' \| 'infobip' \| 'plivo'` |
| `TelecomEventType` | All `message.*` and `call.*` type strings |
| `TwilioWebhookPayload` | Raw Twilio form body shape |
| `Workspace` | Workspace entity |

### `@telecom-webhook/utils`

| Export | Description |
|---|---|
| `generateUuid()` / `isUuid()` | UUID v4 |
| `extractOrCreateCorrelationId()` | Reads header or generates new ID |
| `createLogger()` / `StructuredLogger` | JSON line logger (stdout) |
| `nowIsoUtc()` / `isIsoTimestamp()` | UTC timestamp helpers |
| `exponentialBackoffMs()` / `sleep()` | Retry backoff |

---

## PostgreSQL schema

```sql
workspaces (
  id             UUID PK,
  name           TEXT,
  endpoint_token TEXT UNIQUE,
  created_at     TIMESTAMPTZ,
  updated_at     TIMESTAMPTZ
)

webhook_events (
  id              UUID PK,
  workspace_id    UUID FK -> workspaces,
  provider        TEXT,
  event_type      TEXT,
  message_sid     TEXT,
  call_sid        TEXT,
  from_number     TEXT,
  to_number       TEXT,
  status          TEXT,
  request_headers JSONB,
  request_payload JSONB,
  received_at     TIMESTAMPTZ,
  processed_at    TIMESTAMPTZ,
  processing_ms   INTEGER
)
```

Indexes: `workspace_id`, `(workspace_id, received_at)`, `message_sid`, `call_sid`, GIN on `request_payload`.

---

## Environment variables

### ingestion-service

| Variable | Default | Description |
|---|---|---|
| `INGESTION_PORT` | `3001` | HTTP port |
| `DATABASE_URL` | _(required)_ | PostgreSQL connection string |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `PROCESSING_BASE_URL` | `http://localhost:3003` | processing-service URL |
| `RATE_LIMIT_TTL_SECONDS` | `60` | Throttler window |
| `RATE_LIMIT_MAX` | `500` | Max requests per window |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

### processing-service

| Variable | Default | Description |
|---|---|---|
| `PROCESSING_PORT` | `3003` | HTTP port |
| `DATABASE_URL` | _(required)_ | PostgreSQL connection string |
| `LOG_LEVEL` | `info` | Log level |

### realtime-gateway

| Variable | Default | Description |
|---|---|---|
| `GATEWAY_PORT` | `3004` | HTTP/WebSocket port |
| `DATABASE_URL` | _(required)_ | PostgreSQL connection string |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REALTIME_GATEWAY_CORS_ORIGIN` | `http://localhost:4200` | Allowed CORS origin |
| `LOG_LEVEL` | `info` | Log level |
