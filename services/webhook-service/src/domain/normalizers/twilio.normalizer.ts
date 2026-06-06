import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
  TwilioWebhookPayload,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  NormalizationContext,
  ProviderNormalizerPort,
} from '../ports/provider-normalizer.port';

@Injectable()
export class TwilioNormalizer implements ProviderNormalizerPort {
  readonly providerName = EventSource.TWILIO;

  normalize(
    payload: unknown,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    const p = payload as TwilioWebhookPayload;
    return [
      buildCanonicalEvent({
        eventType: TwilioNormalizer.mapEventType(p.MessageStatus),
        channel: Channel.SMS,
        source: EventSource.TWILIO,
        correlationId: context.correlationId,
        timestamp: new Date().toISOString(),
        metadata: {
          provider: EventSource.TWILIO,
          accountSid: p.AccountSid,
          messageSid: p.MessageSid,
          numSegments: p.NumSegments,
        },
        payload: {
          messageId: p.MessageSid,
          from: p.From,
          to: p.To,
          status: p.MessageStatus,
          body: p.Body,
          errorCode: p.ErrorCode,
          errorMessage: p.ErrorMessage,
        },
      }),
    ];
  }

  private static mapEventType(status?: string): EventType {
    switch (status) {
      case 'delivered':
        return EventType.DELIVERY_EVENT;
      case 'failed':
      case 'undelivered':
        return EventType.ERROR_EVENT;
      default:
        return EventType.STATUS_EVENT;
    }
  }
}
