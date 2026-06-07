# Integrations

## Currently supported

### Twilio

All Twilio SMS, WhatsApp, and Voice webhooks are supported.

**Provider detection:** Request contains `x-twilio-signature` header.

#### SMS inbound

Twilio sends `application/x-www-form-urlencoded` to your webhook URL when a message arrives.

Configure: **Twilio Console → Phone Numbers → your number → Messaging → A message comes in**

Captured fields: `MessageSid`, `From`, `To`, `Body`, `NumMedia`, `NumSegments`, `AccountSid`

#### SMS delivery status callback

Configure: **Messaging → Message Status Callback URL**

Statuses: `queued`, `sending`, `sent`, `delivered`, `undelivered`, `failed`, `read`

Legacy field aliases supported: `SmsStatus`, `SmsSid`, `SmsMessageSid`

#### Voice call events

Configure: **Voice → A call comes in**

Statuses: `initiated`, `ringing`, `in-progress`, `completed`, `busy`, `no-answer`, `failed`

---

## Adding a new provider

### 1. Add the payload interface to `shared/contracts`

```typescript
// shared/contracts/src/provider-payloads/vonage.payload.ts
export interface VonageInboundSmsPayload {
  msisdn: string;
  to: string;
  messageId: string;
  text: string;
  'message-timestamp': string;
}
```

Export it from `shared/contracts/src/provider-payloads/index.ts` and `shared/contracts/src/index.ts`.

### 2. Create the normaliser in `processing-service`

```typescript
// services/processing-service/src/infrastructure/normalizers/vonage.normalizer.ts
import { Injectable } from '@nestjs/common';
import { WebhookEvent } from '@telecom-webhook/contracts';

@Injectable()
export class VonageNormalizer {
  normalize(id: string, capture: {
    workspaceId: string;
    headers: Record<string, string>;
    body: Record<string, unknown>;
    receivedAt: Date;
  }): WebhookEvent {
    const body = capture.body as Record<string, string>;
    return {
      id,
      workspaceId: capture.workspaceId,
      provider: 'vonage',
      eventType: 'message.inbound',
      receivedAt: capture.receivedAt,
      messageSid: body['messageId'],
      from: body['msisdn'],
      to: body['to'],
      headers: capture.headers,
      payload: capture.body,
    };
  }
}
```

### 3. Register the normaliser in `processing.module.ts`

```typescript
providers: [TwilioNormalizer, VonageNormalizer, ProcessEventUseCase],
```

### 4. Add detection in `ingest-event.use-case.ts`

```typescript
private detectProvider(headers: Record<string, string>): string {
  if (headers['x-twilio-signature'])      return 'twilio';
  if (headers['x-vonage-signature'])      return 'vonage';   // add
  if (headers['messagebird-signature-jwt']) return 'messagebird';
  return 'unknown';
}
```

### 5. Route to the new normaliser in `process-event.use-case.ts`

```typescript
switch (cmd.provider) {
  case 'twilio': return this.twilio.normalize(id, capture);
  case 'vonage': return this.vonage.normalize(id, capture);  // add
  default:       return passthroughNormalize(id, cmd);
}
```

---

## Provider roadmap

| Provider | Status |
|---|---|
| Twilio SMS + Voice | ✅ Supported |
| Vonage SMS | 🔜 Phase 2 |
| Vonage Voice | 🔜 Phase 2 |
| MessageBird SMS | 🔜 Phase 2 |
| Infobip | 🔜 Phase 2 |
| Plivo | 🔜 Phase 2 |
