# Provider Integrations Guide

This document describes how each supported telecom provider sends webhooks to the platform,
how signatures are validated, and how to add a new provider.

---

## How it Works

Every provider delivers webhooks to the **ingestion endpoint**:

```
POST /:workspaceId/:endpointToken
```

The `ingestion-service` accepts and captures the request; the `processing-service` detects
the provider, validates the signature, and normalises the body into a `WebhookEvent`.

```
Provider  ──POST──►  ingestion-service  ──HTTP──►  processing-service  ──INSERT──►  PostgreSQL
                     (capture)                      (normalise + validate)
```

---

## Provider Reference

### Twilio ✅ (Supported — Phase 1)

**Events:** Incoming SMS/WhatsApp, message status callbacks, voice calls, voice status updates.

**Signature scheme:** HMAC-SHA1 over `fullUrl + sortedFormParams`, Base64-encoded.

```
X-Twilio-Signature: <base64(HMAC_SHA1(authToken, url + sortedParams))>
```

**Configuration:**

```dotenv
TWILIO_AUTH_TOKEN=your_twilio_auth_token
```

In development (`NODE_ENV !== production`), signature validation is skipped
with a warning if `TWILIO_AUTH_TOKEN` is not set.

**Twilio Dashboard Setup:**

Point any of these to your ingestion endpoint:
- SMS/WhatsApp → *A Message Comes In* callback
- Voice → *A Call Comes In* callback
- Messaging → *Status Callback URL*

```
http://<your-host>:3001/<workspaceId>/<endpointToken>
```

---

### Vonage 🔜 (Phase 2)

Not yet implemented.

Normaliser target: `services/processing-service/src/normalizers/vonage.normalizer.ts`

Signature scheme: HMAC-SHA256, header `X-Vonage-Signature`.

---

### MessageBird 🔜 (Phase 2)

Not yet implemented.

Signature scheme: HMAC-SHA256, header `MessageBird-Signature-JWT`.

---

### Infobip 🔜 (Phase 2)

Not yet implemented.

Signature scheme: Bearer token, header `Authorization`.

---

### Plivo 🔜 (Phase 2)

Not yet implemented.

Signature scheme: HMAC-SHA256, header `X-Plivo-Signature-V2`.

---

## Adding a New Provider

1. **Add a normaliser** in `services/processing-service/src/normalizers/<provider>.normalizer.ts`:

```typescript
import { WebhookEvent } from '@telecom-webhook/contracts';

export function normalizeTwilio(
  workspaceId: string,
  headers: Record<string, string>,
  body: Record<string, unknown>,
): WebhookEvent {
  return {
    id: generateUUID(),
    workspaceId,
    provider: 'twilio',
    eventType: detectEventType(body),
    receivedAt: new Date(),
    headers,
    payload: body,
    messageSid: body.MessageSid as string | undefined,
    from: body.From as string | undefined,
    to: body.To as string | undefined,
    status: (body.MessageStatus ?? body.CallStatus) as string | undefined,
  };
}
```

2. **Register the normaliser** in the provider registry:
   `services/processing-service/src/normalizers/provider-registry.ts`

3. **Add signature validation** in:
   `services/processing-service/src/validators/<provider>-signature.validator.ts`

4. **Add to `TelecomProvider` union** in `shared/contracts/src/webhook-event.interface.ts`

5. **Write unit tests** with a sample real payload from the provider's documentation.

6. **Update this document** and the provider table in `README.md`.
