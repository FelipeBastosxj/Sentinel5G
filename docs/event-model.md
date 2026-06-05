# Canonical Event Model

This document defines the core data contract of the EventStream Observability Engine.

All events must be normalized into `CanonicalEvent` before entering the platform.

---

## TypeScript Interface

```typescript
import { CanonicalEvent, EventType } from '@eventstream/contracts';
```

Defined in: `shared/contracts/src/canonical-event.interface.ts`

---

## Structure

```json
{
  "eventId":       "550e8400-e29b-41d4-a716-446655440000",
  "eventType":     "DELIVERY_EVENT",
  "timestamp":     "2026-06-05T12:00:00.000Z",
  "source":        "twilio",
  "correlationId": "req-abc-123",
  "version":       "1.0",
  "metadata": {
    "provider":    "twilio",
    "region":      "us-east-1"
  },
  "payload": {
    "to":          "+15551234567",
    "status":      "delivered",
    "messageSid":  "SM1234567890abcdef"
  }
}
```

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `eventId` | `string` (UUID v4) | ✅ | Unique event identifier, generated at ingestion |
| `eventType` | `EventType \| string` | ✅ | Type of event (see EventType enum) |
| `timestamp` | `string` (ISO 8601) | ✅ | When the event was created |
| `source` | `string` | ✅ | Origin provider or system |
| `correlationId` | `string` | ✅ | Distributed tracing ID, propagated across all services |
| `version` | `string` | ❌ | Schema version, defaults to `"1.0"` |
| `metadata` | `Record<string, unknown>` | ❌ | Routing tags, enrichment data |
| `payload` | `T` (generic) | ❌ | Provider-specific or event-specific data |

---

## EventType Enum

```typescript
export enum EventType {
  DELIVERY_EVENT = 'DELIVERY_EVENT',  // Message delivery confirmation
  STATUS_EVENT   = 'STATUS_EVENT',    // Status update from provider
  ERROR_EVENT    = 'ERROR_EVENT',     // Error or failure event
  METRIC_EVENT   = 'METRIC_EVENT',    // Aggregated platform metric
  ALERT_EVENT    = 'ALERT_EVENT',     // System alert (DLQ, threshold breaches)
}
```

---

## Immutability Rules

1. `eventId` is assigned once at ingestion and never changed.
2. `timestamp` reflects the creation time, never modified.
3. `correlationId` is preserved across the entire event flow.
4. Events are never mutated after being published to Kafka — enrichment creates new events.

---

## Integration Boundary Rule

Provider-specific payloads (`TwilioWebhookPayload`, `InfobipPayload`, etc.) must be transformed into `CanonicalEvent` **before** being published to Kafka.

The core platform (Processing Service, Realtime Gateway, ClickHouse) must never consume provider-specific structures directly.

```text
TwilioPayload  ──► WebhookService.normalize() ──► CanonicalEvent ──► Kafka
                                                       ✅ allowed
TwilioPayload  ──────────────────────────────────────────────────► Kafka
                                                       ❌ forbidden
```

See: `HARDNESS.md §4.3`
