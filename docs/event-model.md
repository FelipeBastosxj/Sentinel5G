# Event model

## WebhookEvent

Every inbound webhook is normalised into this interface before being persisted.

```typescript
interface WebhookEvent {
  id: string;               // UUID v4, server-assigned
  workspaceId: string;      // UUID of the receiving workspace
  provider: TelecomProvider;
  eventType: TelecomEventType;

  receivedAt: Date;         // UTC timestamp at HTTP layer
  processedAt?: Date;
  processingMs?: number;    // wall-clock processing time

  messageSid?: string;      // Twilio MessageSid
  callSid?: string;         // Twilio CallSid
  from?: string;            // originating number (E.164)
  to?: string;              // destination number
  status?: string;          // delivery/call status string

  headers: Record<string, string>;    // full HTTP request headers
  payload: Record<string, unknown>;   // full HTTP request body
}
```

### WebhookEventSummary

Lightweight projection broadcast over WebSocket (omits large headers/payload blobs):

```typescript
interface WebhookEventSummary {
  id: string;
  workspaceId: string;
  provider: TelecomProvider;
  eventType: TelecomEventType;
  receivedAt: Date;
  from?: string;
  to?: string;
  status?: string;
}
```

---

## TelecomEventType values

### Messaging

| Value | Trigger |
|---|---|
| `message.inbound` | Incoming SMS / WhatsApp to your Twilio number |
| `message.status.queued` | Message accepted by Twilio |
| `message.status.sending` | Being sent |
| `message.status.sent` | Sent to carrier |
| `message.status.delivered` | Delivered to recipient |
| `message.status.undelivered` | Carrier confirmed non-delivery |
| `message.status.failed` | Failed before reaching carrier |
| `message.status.read` | WhatsApp read receipt |

### Voice

| Value | Trigger |
|---|---|
| `call.inbound` | Incoming call to your Twilio number |
| `call.outbound` | Outbound call initiated |
| `call.status.initiated` | Outbound call created |
| `call.status.ringing` | Call is ringing |
| `call.status.in-progress` | Call answered |
| `call.status.completed` | Call ended normally |
| `call.status.busy` | Busy signal |
| `call.status.no-answer` | Not answered |
| `call.status.failed` | Failed to connect |

---

## Twilio field mapping

### Inbound SMS (`message.inbound`)

Twilio sends `application/x-www-form-urlencoded`:

| Twilio field | WebhookEvent field | Notes |
|---|---|---|
| `MessageSid` / `SmsSid` / `SmsMessageSid` | `messageSid` | legacy aliases supported |
| `From` | `from` | E.164 format |
| `To` | `to` | Your Twilio number |
| `Body` | _(in `payload`)_ | SMS text |
| `AccountSid` | _(in `payload`)_ | |
| `NumMedia` | _(in `payload`)_ | Number of attachments |

**Detection:** body contains `Body` field or `MessageSid`/`SmsSid` without `MessageStatus`.

### Outbound status callback (`message.status.*`)

| Twilio field | WebhookEvent field |
|---|---|
| `MessageSid` / `SmsSid` | `messageSid` |
| `MessageStatus` / `SmsStatus` | `status` + determines `eventType` |
| `From` | `from` |
| `To` | `to` |
| `ErrorCode` | _(in `payload`)_ |

### Voice call (`call.*`)

| Twilio field | WebhookEvent field |
|---|---|
| `CallSid` | `callSid` |
| `CallStatus` | `status` + determines `eventType` |
| `Direction` | determines `call.inbound` vs `call.outbound` |
| `From` | `from` |
| `To` | `to` |

---

## eventType detection logic (Twilio)

```
if CallSid present:
  direction = Direction field -> 'inbound' | 'outbound'
  status    = CallStatus
  eventType = call.{direction} | call.status.{status}

else if MessageStatus or SmsStatus present:
  eventType = message.status.{status}

else (inbound SMS):
  eventType = message.inbound
```

---

## TelecomProvider values

```typescript
type TelecomProvider = 'twilio' | 'vonage' | 'messagebird' | 'infobip' | 'plivo';
```

Unknown providers use a passthrough normaliser that stores the raw body verbatim with `eventType: 'unknown'`.
