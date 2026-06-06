# Integrations Guide

External providers integrate with the EventStream Observability Engine
exclusively through the **`webhook-service`**. This document describes how
each supported provider is wired in, the signature scheme used to verify
authenticity, and how to add a new provider.

> **Architectural rule (HARDNESS §5).**  
> The `webhook-service` is the **only** integration boundary. It validates
> provider signatures, normalizes the payload to `CanonicalEvent`, and
> forwards the result to `ingestion-service` over HTTP. Providers never
> talk to Kafka directly.

```
External provider  ──HTTP──►  webhook-service  ──HTTP──►  ingestion-service  ──Kafka──►  events.raw
   (raw payload)       (validate signature)        (validate schema)
                       (normalize → CanonicalEvent)
```

---

## Supported providers

| Provider | Endpoint | Channel | Signature scheme | Header(s) |
|----------|----------|---------|------------------|-----------|
| **Twilio** | `POST /integrations/twilio/webhook` | `sms` | HMAC-SHA1 (Base64) over `fullUrl + sortedFormParams` | `X-Twilio-Signature` |
| **Infobip** | `POST /integrations/infobip/webhook` | `sms` | Static Bearer token | `Authorization: Bearer <token>` |
| **SendGrid** | `POST /integrations/sendgrid/webhook` | `email` | HMAC-SHA256 over `timestamp + rawBody` | `X-Twilio-Email-Event-Webhook-Signature`, `X-Twilio-Email-Event-Webhook-Timestamp` |
| **Custom** | `POST /integrations/custom/webhook` | depends on payload | None (network-level auth assumed) | — |

All four endpoints respond with **HTTP 202 Accepted** on success and
**HTTP 401 Unauthorized** on signature failure. The forwarder propagates
the inbound `x-correlation-id` header (or generates one if absent).

The `webhook-service` listens on **`WEBHOOK_SERVICE_PORT`** (default `3004`).

---

## Twilio

### Signature

Twilio signs every webhook with HMAC-SHA1 over `URL + sortedFormParams`,
encoded as Base64. The validator implementation is in
`services/webhook-service/src/infrastructure/signatures/twilio-signature.validator.ts`.

```text
expected = base64( HMAC_SHA1(authToken, fullUrl + sortedConcat(params)) )
match    = expected === request.header('X-Twilio-Signature')
```

### Configuration

```dotenv
TWILIO_WEBHOOK_SECRET=your-twilio-auth-token
```

In non-production environments, validation is skipped if `TWILIO_WEBHOOK_SECRET`
is not set, with a warning logged.

### Normalization

Twilio form payloads (`MessageStatus=delivered`, `From=+1...`, etc.) are
mapped to one `CanonicalEvent` with:

* `channel = SMS`
* `source  = twilio`
* `eventType = STATUS_EVENT` (or `ERROR_EVENT` if `ErrorCode` is present)

---

## Infobip

### Signature

Infobip uses static Bearer authentication on the inbound webhook. We compare
`Authorization` against the configured token (`INFOBIP_WEBHOOK_SECRET`). The
validator implementation is in
`services/webhook-service/src/infrastructure/signatures/infobip-signature.validator.ts`.

### Configuration

```dotenv
INFOBIP_WEBHOOK_SECRET=your-infobip-webhook-token
```

### Normalization

Infobip batches multiple messages per request (`results: [...]`). Each entry
becomes its own `CanonicalEvent`, sharing the same `correlationId`.

---

## SendGrid

### Signature

SendGrid signs the body with HMAC-SHA256 using a shared key:

```text
toSign   = timestampHeader + rawBody
expected = base64( HMAC_SHA256(secret, toSign) )
```

The validator implementation is in
`services/webhook-service/src/infrastructure/signatures/sendgrid-signature.validator.ts`.

> SendGrid sends a JSON **array** of events; we map each entry to a
> separate `CanonicalEvent` (`channel = EMAIL`).

### Configuration

```dotenv
SENDGRID_WEBHOOK_SECRET=your-sendgrid-shared-secret
```

The raw request body must be available for HMAC verification — the
`webhook-service` registers `express.json({ verify })` to capture
`req.rawBody` before parsing.

---

## Custom

The `custom` endpoint accepts any JSON payload that already conforms to
`CanonicalEvent`. It is intended for internal or test integrations.

> **Auth model.** The custom endpoint uses a `NoopSignatureValidator` —
> authentication is delegated to the platform networking layer (private
> VPC, mTLS, API gateway). Add a real validator before exposing it to
> the public internet (HARDNESS §9).

### Normalization

The payload is validated against `canonicalEventSchema` and forwarded
verbatim. Any missing field that has a default (eventId, timestamp,
version) is auto-filled by `buildCanonicalEvent()`.

---

## Adding a new provider

1. **Define the raw payload type** in
   `shared/contracts/src/provider-payloads/<provider>.payload.ts`.
2. **Add a Zod schema** for the payload in
   `shared/schemas/src/provider-payloads/<provider>.schema.ts`.
3. **Implement the normalizer** in
   `services/webhook-service/src/domain/normalizers/<provider>.normalizer.ts`,
   mapping the raw payload to `CanonicalEvent[]` via
   `buildCanonicalEvent()`.
4. **Implement the signature validator** under
   `services/webhook-service/src/infrastructure/signatures/`.
5. **Wire a thin controller** under
   `services/webhook-service/src/controllers/` that delegates to the
   `ProcessWebhookUseCase`.
6. **Add unit tests** for the normalizer and signature validator
   (mandatory per HARDNESS §11).
7. **Document the provider** in this file (signature scheme, channel,
   environment variables).

> The forwarder, correlation propagation, error handling, observability
> hooks and DI plumbing are reused — you only ship domain logic.

---

## Testing webhooks locally

```powershell
# Twilio (form-encoded)
curl -X POST http://localhost:3004/integrations/twilio/webhook `
  -H "X-Twilio-Signature: <hash>" `
  -d "MessageSid=SM123&AccountSid=AC123&MessageStatus=delivered&From=%2B15551234567&To=%2B15557654321"

# Infobip (Bearer)
curl -X POST http://localhost:3004/integrations/infobip/webhook `
  -H "Authorization: Bearer $env:INFOBIP_WEBHOOK_SECRET" `
  -H "Content-Type: application/json" `
  -d "{\"results\":[{\"messageId\":\"m1\",\"to\":\"+15551234567\",\"status\":{\"id\":5,\"groupId\":3,\"groupName\":\"DELIVERED\",\"name\":\"DELIVERED_TO_HANDSET\"}}]}"

# SendGrid (JSON array)
curl -X POST http://localhost:3004/integrations/sendgrid/webhook `
  -H "Content-Type: application/json" `
  -H "X-Twilio-Email-Event-Webhook-Signature: <sig>" `
  -H "X-Twilio-Email-Event-Webhook-Timestamp: 1700000000" `
  -d "[{\"event\":\"delivered\",\"email\":\"a@b.com\",\"sg_event_id\":\"abc\",\"timestamp\":1700000000}]"

# Custom (any CanonicalEvent-shaped payload)
curl -X POST http://localhost:3004/integrations/custom/webhook `
  -H "Content-Type: application/json" `
  -d "{\"channel\":\"internal\",\"eventType\":\"DELIVERY_EVENT\",\"source\":\"my-app\",\"correlationId\":\"corr-1\"}"
```

The accepted event will appear in real time on the Angular dashboard
(`Events` page) and on the Kafka topic `events.raw`.
