# Observability Guide

Every backend service in the EventStream Observability Engine emits the
same three observability signals — **structured logs**, **metrics**, and
**distributed traces** — sharing a common `correlationId` (HARDNESS §6).

```
            ┌────────────────────── correlationId ──────────────────────┐
            ▼                                                            ▼
HTTP / Webhook  ──►  ingestion  ──►  events.raw  ──►  processing  ──►  events.processed
                       │                                  │                   │
                       └─ logs ────► stdout (JSON)  ◄─────┘ ◄─────────────────┘
                       └─ metrics ──► /metrics (Prometheus)
                       └─ traces ───► OTLP (Jaeger / Tempo / OTel Collector)
```

---

## 1. Structured logs

* Every service uses `createLogger()` from `@eventstream/utils`.
* Output is **JSON-line stdout** — machine-parsable by Loki / Elastic / etc.
* Each log entry contains `timestamp`, `level`, `service`, `correlationId`
  (when in scope), `message`, and any structured bindings.
* `LoggerService` automatically merges the current `correlationId` from
  `CorrelationService` (AsyncLocalStorage), so handlers never have to pass
  it manually.

Example:

```json
{
  "timestamp": "2026-06-05T12:00:01.234Z",
  "level": "info",
  "service": "ingestion-service",
  "correlationId": "req-abc-123",
  "eventId": "550e8400-e29b-41d4-a716-446655440000",
  "channel": "SMS",
  "source": "twilio",
  "message": "event published to events.raw"
}
```

---

## 2. Metrics

Each service exposes `GET /metrics` in Prometheus exposition format.

### Common metrics (all services)

| Metric | Type | Labels |
|--------|------|--------|
| `process_cpu_user_seconds_total` | counter | — |
| `process_resident_memory_bytes` | gauge | — |
| `nodejs_heap_size_total_bytes` | gauge | — |
| `nodejs_eventloop_lag_seconds` | gauge | — |

(provided by `prom-client`'s default collectors)

### Ingestion service

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `ingestion_events_received_total` | counter | `source`, `channel`, `eventType` | Events arriving at `POST /ingest` |
| `ingestion_events_rejected_total` | counter | `reason` | Schema or rate-limit rejections |
| `ingestion_events_published_total` | counter | `topic` | Events successfully published to Kafka |
| `ingestion_publish_latency_seconds` | histogram | `topic` | End-to-end publish latency |

### Processing service

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `processing_events_consumed_total` | counter | `source`, `channel` | Events read from `events.raw` |
| `processing_events_processed_total` | counter | `source`, `channel` | Events successfully published to `events.processed` |
| `processing_events_retried_total` | counter | `attempt` | Retries performed during enrichment |
| `processing_events_dead_lettered_total` | counter | `source` | Events sent to `events.alerts` after exhausting retries |
| `processing_duration_seconds` | histogram | `outcome` | Per-event processing time (success/retry/dlq) |

### Realtime gateway

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `gateway_connected_clients` | gauge | — | Active WebSocket connections |
| `gateway_broadcasts_total` | counter | `stream` | Messages broadcast to clients (`events` / `metrics`) |
| `gateway_redis_fanout_total` | counter | `direction` | Messages flowing through the Redis pub/sub backplane (`publish` / `receive`) |

### Webhook service

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `webhook_received_total` | counter | `provider`, `channel` | Inbound webhook invocations |
| `webhook_rejected_total` | counter | `provider`, `reason` | Rejections (`signature` / `normalization` / `schema`) |
| `webhook_forwarded_total` | counter | `provider` | Events successfully forwarded to `ingestion-service` |
| `webhook_forward_latency_seconds` | histogram | `provider` | Latency of the HTTP forward call |

---

## 3. Distributed traces

Traces are emitted via the **OpenTelemetry SDK** with auto-instrumentations
for HTTP, Express/NestJS, and `kafkajs`. Bootstrap happens **before**
NestJS starts, in `observability/tracing.ts`.

* OTLP HTTP exporter → endpoint configured by
  `OTEL_EXPORTER_OTLP_ENDPOINT` (defaults to `http://localhost:4318`).
* Service name comes from `OTEL_SERVICE_NAME` (defaults to the service
  package name).
* Each span is automatically tagged with `correlation.id` so traces and
  logs are joinable.

Disable tracing by leaving `OTEL_EXPORTER_OTLP_ENDPOINT` unset.

---

## 4. Health checks

Each service exposes `GET /health` returning HTTP 200 + JSON when:

* The service has finished bootstrap.
* Its Kafka producer/consumer reports `isConnected() === true`.
* (Realtime gateway) Redis fanout adapter is connected.

A non-ready service returns HTTP 503 with the failing component listed.

---

## 5. Correlation propagation

`CorrelationService` is an `AsyncLocalStorage`-backed wrapper that flows
through:

1. Inbound HTTP — `CorrelationMiddleware` reads `x-correlation-id` (or
   generates one) and calls `correlation.run(id, next)`.
2. Outbound HTTP (e.g. webhook → ingestion) — the forwarder injects
   the current correlationId into the request headers.
3. Kafka producer — the correlationId is added to message headers.
4. Kafka consumer — the consumer reads the header and re-enters the
   AsyncLocalStorage context via `correlation.run()` before invoking the
   use-case.
5. WebSocket broadcasts — the correlationId is included in the emitted
   payload metadata.

Result: a single `correlationId` is observable end-to-end in logs, metrics
labels, and trace baggage.

---

## 6. Local observability stack

Optional services are defined under the `observability` profile in
`infra/docker-compose.yml`. Start them with:

```powershell
docker compose --profile observability up -d
```

| Component | Port | Purpose |
|-----------|------|---------|
| Prometheus | 9090 | Scrapes `/metrics` from all backend services |
| Loki | 3100 | Aggregates JSON logs |
| Tempo | 3200 | Stores OpenTelemetry traces |
| Grafana | 3000 | Dashboards over Prometheus / Loki / Tempo |
| OTel Collector | 4318 | Receives OTLP, fans out to Tempo / Prometheus / Loki |

> The default profile (no `--profile`) starts only Kafka / Redis /
> ClickHouse / Kafka-UI — keeping the local footprint small.
