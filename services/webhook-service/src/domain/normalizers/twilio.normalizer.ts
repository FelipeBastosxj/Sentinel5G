import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
  TwilioMessageStatus,
  TwilioWebhookPayload,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  NormalizationContext,
  ProviderNormalizerPort,
} from '../ports/provider-normalizer.port';

/**
 * Maps a Twilio status-callback payload to a {@link CanonicalEvent}.
 *
 * Twilio sends one webhook per message status transition, so this normalizer
 * always returns a single event (HARDNESS §4 — events are immutable; new
 * status = new event).
 */
@Injectable()
export class TwilioNormalizer
  implements ProviderNormalizerPort<TwilioWebhookPayload>
{
  readonly providerName = EventSource.TWILIO;

  normalize(
    payload: TwilioWebhookPayload,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    const event = buildCanonicalEvent({
      eventType: TwilioNormalizer.mapEventType(payload.MessageStatus),
      channel: Channel.SMS,
      source: EventSource.TWILIO,
      correlationId: context.correlationId,
      metadata: {
        provider: EventSource.TWILIO,
        accountSid: payload.AccountSid,
        apiVersion: payload.ApiVersion,
      },
      payload: {
        messageSid: payload.MessageSid,
        from: payload.From,
        to: payload.To,
        body: payload.Body,
        status: payload.MessageStatus,
        errorCode: payload.ErrorCode,
        errorMessage: payload.ErrorMessage,
        numMedia: payload.NumMedia,
        numSegments: payload.NumSegments,
      },
    });

    return [event];
  }

  /** Translate the provider status string to a domain EventType. */
  private static mapEventType(status: TwilioMessageStatus): EventType {
    switch (status) {
      case 'delivered':
      case 'sent':
      case 'received':
      case 'read':
        return EventType.DELIVERY_EVENT;
      case 'failed':
      case 'undelivered':
      case 'canceled':
        return EventType.ERROR_EVENT;
      default:
        return EventType.STATUS_EVENT;
    }
  }
}
