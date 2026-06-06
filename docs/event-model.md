# Canonical Event Model

This document defines the core data contract of the EventStream Observability Engine.

All events must be normalized into `CanonicalEvent` before entering the platform.

---

## TypeScript Interface

```typescript
import { CanonicalEvent, EventType, Channel } from '@eventstream/contracts';
```

Defined in: `shared/contracts/src/canonical-event.interface.ts`

---

## Structure

```json
{
  "eventId":       "550e8400-e29b-41d4-a716-446655440000",
  "eventType":     "DELIVERY_EVENT",
  "channel":       "SMS",
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
| `eventType` | `EventType \| string` | ✅ | Type of event (see `EventType` enum) |
| `channel` | `Channel` | ✅ | Communication channel — `SMS` / `EMAIL` / `WHATSAPP` / `PUSH` / `INTERNAL` |
| `timestamp` | `string` (ISO 8601 UTC) | ✅ | When the event was created |
| `source` | `string` | ✅ | Origin provider or system |
| `correlationId` | `string` | ✅ | Distributed tracing ID, propagated across all services |
| `version` | `string` | ✅ | Schema version, currently `"1.0"` |
| `metadata` | `Record<string, unknown>` | ❌ | Routing tags, enrichment data |
| `payload` | `T` (generic) | ❌ | Provider-specific or event-specific data |

> All fields are declared `readonly` in the TypeScript interface and the
> factory `buildCanonicalEvent()` calls `Object.freeze()` on the result —
> events are immutable by construction (HARDNESS §4).

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

## Channel Enum

```typescript
export enum Channel {
  SMS      = 'SMS',
  EMAIL    = 'EMAIL',
  WHATSAPP = 'WHATSAPP',
  PUSH     = 'PUSH',
  INTERNAL = 'INTERNAL',
}
```

---

## Immutability Rules (HARDNESS §4)

1. `eventId` is assigned once at ingestion and never changed.
2. `timestamp` reflects the creation time, never modified.
3. `correlationId` is preserved across the entire event flow.
4. Events are never mutated after being published to Kafka — enrichment
   creates **new** events via `buildCanonicalEvent()`.
5. Every produced `CanonicalEvent` is `Object.freeze`d.

---

## Integration Boundary Rule (HARDNESS §5)

Provider-specific payloads (`TwilioWebhookPayload`, `InfobipPayload`, etc.)
must be transformed into `CanonicalEvent` **before** being published to Kafka.

The core platform (Processing Service, Realtime Gateway, ClickHouse) must
never consume provider-specific structures directly.

```text
TwilioPayload ──► webhook-service.normalize() ──► HTTP /ingest ──► Kafka
                                                       ✅ allowed
TwilioPayload ───────────────────────────────────────────────► Kafka
                                                       ❌ forbidden
```

See: `HARDNESS.md §4.3` and `docs/integrations.md`.

---

## Validation

Runtime validation is performed via the Zod schema
`canonicalEventSchema` exported from `@eventstream/schemas`. The
`ingestion-service` invokes `validateOrThrow()` before publishing to Kafka,
guaranteeing that no malformed event ever reaches the platform.

```typescript
import { canonicalEventSchema, validateOrThrow } from '@eventstream/schemas';

const event = validateOrThrow(canonicalEventSchema, payload);
// event is now a typed, validated, frozen CanonicalEvent
```

A validation failure throws `SchemaValidationError` (mapped to HTTP 400 by
the global exception filter) and the event is **not** published.
