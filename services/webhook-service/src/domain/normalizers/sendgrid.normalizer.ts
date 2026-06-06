import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
  SendGridEvent,
  SendGridEventType,
  SendGridWebhookPayload,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  NormalizationContext,
  ProviderNormalizerPort,
} from '../ports/provider-normalizer.port';

/**
 * Maps a SendGrid event-webhook batch to CanonicalEvents (one per item).
 * Channel is `email` (HARDNESS §4 — channel is mandatory).
 */
@Injectable()
export class SendGridNormalizer
  implements ProviderNormalizerPort<SendGridWebhookPayload>
{
  readonly providerName = EventSource.SENDGRID;

  normalize(
    payload: SendGridWebhookPayload,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    return payload.map((evt) => this.toEvent(evt, context));
  }

  private toEvent(
    evt: SendGridEvent,
    ctx: NormalizationContext,
  ): CanonicalEvent {
    return buildCanonicalEvent({
      eventType: SendGridNormalizer.mapEventType(evt.event),
      channel: Channel.EMAIL,
      source: EventSource.SENDGRID,
      correlationId: ctx.correlationId,
      timestamp: new Date(evt.timestamp * 1000).toISOString(),
      metadata: {
        provider: EventSource.SENDGRID,
        category: evt.category,
        smtpId: evt.smtp_id,
      },
      payload: {
        email: evt.email,
        event: evt.event,
        sgEventId: evt.sg_event_id,
        sgMessageId: evt.sg_message_id,
        reason: evt.reason,
        status: evt.status,
        response: evt.response,
        url: evt.url,
        ip: evt.ip,
        useragent: evt.useragent,
      },
    });
  }

  private static mapEventType(type: SendGridEventType): EventType {
    switch (type) {
      case 'delivered':
        return EventType.DELIVERY_EVENT;
      case 'bounce':
      case 'dropped':
      case 'spamreport':
        return EventType.ERROR_EVENT;
      default:
        return EventType.STATUS_EVENT;
    }
  }
}
