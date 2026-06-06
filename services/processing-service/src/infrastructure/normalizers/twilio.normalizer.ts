import { Injectable } from '@nestjs/common';
import { WebhookEvent, TelecomEventType } from '@telecom-webhook/contracts';

interface RawCapture {
  workspaceId: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
  receivedAt: Date;
}

/**
 * TwilioNormalizer — maps a raw Twilio form-encoded payload to a WebhookEvent.
 *
 * Twilio uses form-encoded POST bodies for all webhooks. Key fields:
 *   MessageSid, MessageStatus, From, To, Body, CallSid, CallStatus, etc.
 */
@Injectable()
export class TwilioNormalizer {
  normalize(id: string, capture: RawCapture): WebhookEvent {
    const body = capture.body;

    return {
      id,
      workspaceId: capture.workspaceId,
      provider: 'twilio',
      eventType: this.detectEventType(body),
      receivedAt: capture.receivedAt,
      headers: capture.headers,
      payload: body,
      messageSid: body['MessageSid'] as string | undefined,
      callSid: body['CallSid'] as string | undefined,
      from: body['From'] as string | undefined,
      to: body['To'] as string | undefined,
      status: (body['MessageStatus'] ?? body['CallStatus']) as string | undefined,
    };
  }

  private detectEventType(body: Record<string, unknown>): TelecomEventType {
    if (body['CallSid']) {
      const callStatus = (body['CallStatus'] as string | undefined)?.toLowerCase();
      if (callStatus) return `call.status.${callStatus}` as TelecomEventType;
      return 'call.inbound';
    }

    const msgStatus = (body['MessageStatus'] as string | undefined)?.toLowerCase();
    if (msgStatus) return `message.status.${msgStatus}` as TelecomEventType;
    if (body['MessageSid']) return 'message.inbound';

    return 'message.inbound';
  }
}
