# WebhookEvent Model

This document defines the core data contract of **Open Telecom Webhook Observability**.

All inbound webhooks are normalised into `WebhookEvent` before persistence.

Defined in: `shared/contracts/src/webhook-event.interface.ts`

---

## TypeScript Interface

```typescript
import { WebhookEvent, TelecomProvider, TelecomEventType } from '@telecom-webhook/contracts';
```

---

## Full Structure

```typescript
interface WebhookEvent {
  // Identity
  id:           string;              // UUID v4 — server-assigned at ingestion time
  workspaceId:  string;              // Resolved from the URL token

  // Provider
  provider:     TelecomProvider;     // 'twilio' | 'vonage' | 'messagebird' | 'infobip' | 'plivo'
  eventType:    TelecomEventType;    // e.g. 'message.inbound', 'call.status.completed'

  // Timing
  receivedAt:   Date;                // UTC — stamped at the HTTP layer before any processing

  // Raw HTTP capture
  headers: Record<string, string>;   // All request headers, lower-cased
  payload: Record<string, unknown>;  // Full parsed request body (JSON or form-encoded)

  // Extracted telecom fields — indexed for fast filtering
  messageSid?:  string;              // Twilio MessageSid (or provider equivalent)
  callSid?:     string;              // Twilio CallSid (or provider equivalent)
  from?:        string;              // Originating phone number, E.164
  to?:          string;              // Destination phone number, E.164
  status?:      string;              // Delivery / call status reported by provider

  // Processing metadata
  processedAt?: Date;                // When normalisation completed (null = pending)
  processingMs?: number;             // Wall-clock normalisation time in milliseconds
}
```

---

## JSON Example — Twilio Delivered SMS

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "workspaceId": "a0000000-0000-0000-0000-000000000001",
  "provider": "twilio",
  "eventType": "message.status.delivered",
  "receivedAt": "2026-06-06T14:30:00.000Z",
  "headers": {
    "content-type": "application/x-www-form-urlencoded",
    "x-twilio-signature": "abc123=="
  },
  "payload": {
    "MessageSid": "SM1234567890abcdef",
    "MessageStatus": "delivered",
    "To": "+14155552671",
    "From": "+15017122661",
    "AccountSid": "ACxxxxxxxxxxxxxxxx"
  },
  "messageSid": "SM1234567890abcdef",
  "from": "+15017122661",
  "to": "+14155552671",
  "status": "delivered",
  "processedAt": "2026-06-06T14:30:00.045Z",
  "processingMs": 45
}
```

---

## Event Type Reference

### SMS / WhatsApp

| `eventType` | Trigger |
|---|---|
| `message.inbound` | A message was received by your Twilio number |
| `message.status.queued` | Message accepted by Twilio |
| `message.status.sent` | Message handed to carrier |
| `message.status.delivered` | Carrier confirmed delivery |
| `message.status.undelivered` | Carrier could not deliver |
| `message.status.failed` | Twilio could not send |
| `message.status.read` | WhatsApp read receipt |

### Voice

| `eventType` | Trigger |
|---|---|
| `call.inbound` | Inbound call to your Twilio number |
| `call.outbound` | Outbound call initiated |
| `call.status.initiated` | Call created |
| `call.status.ringing` | Remote phone is ringing |
| `call.status.in-progress` | Call connected |
| `call.status.completed` | Call ended normally |
| `call.status.busy` | Remote was busy |
| `call.status.no-answer` | No answer |
| `call.status.failed` | Call failed |

---

## `WebhookEventSummary`

A lighter projection used in list responses and WebSocket broadcast payloads.
Omits `headers` and `payload` to reduce bandwidth.

```typescript
type WebhookEventSummary = Pick<WebhookEvent,
  | 'id' | 'workspaceId' | 'provider' | 'eventType' | 'receivedAt'
  | 'messageSid' | 'callSid' | 'from' | 'to' | 'status' | 'processingMs'
>;
```

---

## Database Representation

Stored in `webhook_events` table. `headers` and `payload` are `JSONB` columns.
Extracted fields (`message_sid`, `call_sid`, `from_number`, `to_number`, `status`)
have dedicated indexed columns for efficient filtering.

See `infra/postgres/init.sql` for the full schema.
