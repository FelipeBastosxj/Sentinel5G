# Observability

The platform exposes lightweight built-in observability signals. No external monitoring infrastructure required.

---

## Health checks

Every backend service exposes `GET /health`:

```bash
curl http://localhost:3001/health   # ingestion-service
curl http://localhost:3003/health   # processing-service
curl http://localhost:3004/health   # realtime-gateway
```

Response: `{ "status": "ok", "uptimeSeconds": 142 }`

Docker Compose uses these endpoints to determine when each container is healthy before starting dependent services.

---

## In-process metrics

### ingestion-service — `GET /metrics`

```json
{ "received": 47, "forwarded": 46, "errors": 1 }
```

### realtime-gateway — `GET /metrics`

```json
{
  "broadcasts": 46,
  "connectedClients": 2,
  "redisFanout": { "in": 0, "out": 46 },
  "uptimeSeconds": 203
}
```

---

## Structured logs

All services emit JSON lines to stdout.

```json
{
  "level": "info",
  "service": "ingestion-service",
  "message": "Webhook captured and forwarded",
  "workspaceId": "7ec21aaf-f7b9-4626-b2e7-af68a4a87e75",
  "provider": "twilio",
  "correlationId": "req-abc123",
  "timestamp": "2026-06-07T12:34:56.789Z"
}
```

### Log levels

Set via `LOG_LEVEL` env var (`debug` | `info` | `warn` | `error`).

### Tail logs

```bash
npm run logs                              # all services
docker logs -f telecom-webhook-ingestion  # single service
```

---

## Correlation IDs

Every inbound request to the ingestion-service is assigned a `correlationId` (from the `X-Correlation-ID` header, or auto-generated). It is:

- Injected into every log line via `AsyncLocalStorage`
- Forwarded as `x-correlation-id` header to the processing-service

Use `correlationId` to trace a single webhook event across both services.

---

## PostgreSQL as metrics source

```sql
-- Event count by provider (last 24 hours)
SELECT provider, COUNT(*) AS total
FROM webhook_events
WHERE received_at > NOW() - INTERVAL '24 hours'
GROUP BY provider;

-- Twilio delivery success rate
SELECT
  COUNT(*) FILTER (WHERE event_type = 'message.status.delivered') AS delivered,
  COUNT(*) FILTER (WHERE event_type LIKE 'message.status.%')      AS total
FROM webhook_events
WHERE provider = 'twilio';

-- Average processing time
SELECT ROUND(AVG(processing_ms)) AS avg_ms FROM webhook_events;
```

Connect with any PostgreSQL client:

```
Host:     localhost
Port:     5432
Database: telecom_webhooks
User:     webhook_user
Password: webhook_pass   (or value from .env)
```
