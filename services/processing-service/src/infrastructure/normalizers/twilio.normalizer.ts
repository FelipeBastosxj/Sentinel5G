import { Injectable } from '@nestjs/common';
import {
  TelecomChannel,
  TelecomEventType,
  WebhookEvent,
} from '@telecom-webhook/contracts';

interface RawCapture {
  workspaceId: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
  receivedAt: Date;
}

/**
 * TwilioNormalizer — maps a raw Twilio form-encoded payload to a WebhookEvent.
 *
 * Twilio uses application/x-www-form-urlencoded for all webhooks. The same
 * MessageSid value may arrive in three different field names depending on
 * the callback type (MessageSid, SmsSid, SmsMessageSid). Likewise the
 * delivery status field can be MessageStatus or the legacy SmsStatus.
 *
 * Voice direction is taken from the `Direction` field when present:
 *   inbound | outbound-api | outbound-dial
 *
 * Channel detection rules (in order):
 *   1. CallSid present                           → 'voice'
 *   2. From/To prefixed with 'whatsapp:'         → 'whatsapp'
 *   3. Message-shaped payload (MessageSid, etc.) → 'sms'
 *   4. Otherwise                                 → 'other'
 *
 * Both `from` and `to` are stored stripped of the 'whatsapp:' prefix to keep
 * the dashboard view consistent (the channel field carries that information).
 */
@Injectable()
export class TwilioNormalizer {
  normalize(id: string, capture: RawCapture): WebhookEvent {
    const body    = capture.body;
    const rawFrom = this.firstString(body, 'From', 'Caller');
    const rawTo   = this.firstString(body, 'To',   'Called');
    const channel = this.detectChannel(body, rawFrom, rawTo);

    return {
      id,
      workspaceId: capture.workspaceId,
      provider: 'twilio',
      eventType: this.detectEventType(body),
      channel,
      receivedAt: capture.receivedAt,
      headers: capture.headers,
      payload: body,
      messageSid: this.firstString(body, 'MessageSid', 'SmsSid', 'SmsMessageSid'),
      callSid:    this.firstString(body, 'CallSid'),
      from:       this.stripWhatsappPrefix(rawFrom),
      to:         this.stripWhatsappPrefix(rawTo),
      status:     this.firstString(body, 'MessageStatus', 'SmsStatus', 'CallStatus'),
    };
  }

  // ───────────────────────────────────────────────────────────────────────
  // Channel detection
  // ───────────────────────────────────────────────────────────────────────
  private detectChannel(
    body: Record<string, unknown>,
    rawFrom: string | undefined,
    rawTo: string | undefined,
  ): TelecomChannel {
    if (typeof body['CallSid'] === 'string') return 'voice';
    if (this.hasWhatsappPrefix(rawFrom) || this.hasWhatsappPrefix(rawTo)) {
      return 'whatsapp';
    }
    if (
      body['Body'] !== undefined ||
      typeof body['MessageSid']    === 'string' ||
      typeof body['SmsSid']        === 'string' ||
      typeof body['SmsMessageSid'] === 'string'
    ) {
      return 'sms';
    }
    return 'other';
  }

  private hasWhatsappPrefix(value: string | undefined): boolean {
    return typeof value === 'string' && value.toLowerCase().startsWith('whatsapp:');
  }

  private stripWhatsappPrefix(value: string | undefined): string | undefined {
    if (!value) return value;
    return value.toLowerCase().startsWith('whatsapp:')
      ? value.slice('whatsapp:'.length)
      : value;
  }

  // ───────────────────────────────────────────────────────────────────────
  // Event-type detection
  // ───────────────────────────────────────────────────────────────────────
  private detectEventType(body: Record<string, unknown>): TelecomEventType {
    // ── Voice / Call ──────────────────────────────────────────────────────
    if (typeof body['CallSid'] === 'string') {
      const callStatus = (body['CallStatus'] as string | undefined)?.toLowerCase();
      if (callStatus) return `call.status.${callStatus}` as TelecomEventType;

      const direction = (body['Direction'] as string | undefined)?.toLowerCase();
      if (direction && direction.startsWith('outbound')) return 'call.outbound';
      return 'call.inbound';
    }

    // ── SMS / MMS — outbound status callback ──────────────────────────────
    const msgStatus = (
      (body['MessageStatus'] ?? body['SmsStatus']) as string | undefined
    )?.toLowerCase();
    if (msgStatus) return `message.status.${msgStatus}` as TelecomEventType;

    // ── SMS / MMS — inbound (default for any message-shaped payload) ──────
    if (
      body['Body'] !== undefined ||
      typeof body['MessageSid'] === 'string' ||
      typeof body['SmsSid'] === 'string' ||
      typeof body['SmsMessageSid'] === 'string'
    ) {
      return 'message.inbound';
    }

    return 'message.inbound';
  }

  private firstString(
    body: Record<string, unknown>,
    ...keys: string[]
  ): string | undefined {
    for (const k of keys) {
      const v = body[k];
      if (typeof v === 'string' && v.length > 0) return v;
    }
    return undefined;
  }
}
