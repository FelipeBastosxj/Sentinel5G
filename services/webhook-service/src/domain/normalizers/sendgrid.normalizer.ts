import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
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
export class SendGridNormalizer implements ProviderNormalizerPort {
  readonly providerName = EventSource.SENDGRID;

  normalize(
    payload: unknown,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    const events = payload as SendGridWebhookPayload;
    return events.map((ev) =>
      buildCanonicalEvent({
        eventType: SendGridNormalizer.mapEventType(ev.event),
        channel: Channel.EMAIL,
        source: EventSource.SENDGRID,
        correlationId: context.correlationId,
        timestamp: new Date(ev.timestamp * 1000).toISOString(),
        metadata: {
          provider: EventSource.SENDGRID,
          sgMessageId: ev.sg_message_id,
          sgEventId: ev.sg_event_id,
        },
        payload: {
          email: ev.email,
          event: ev.event,
          ip: ev.ip,
          useragent: ev.useragent,
          url: ev.url,
          reason: ev.reason,
          status: ev.status,
        },
      }),
    );
  }

  private static mapEventType(event: string): EventType {
    switch (event) {
      case 'delivered':
        return EventType.DELIVERY_EVENT;
      case 'bounce':
      case 'dropped':
        return EventType.ERROR_EVENT;
      default:
        return EventType.STATUS_EVENT;
    }
  }
}
