import { Injectable } from '@nestjs/common';
import { CanonicalEvent, Channel, EventSource, EventType } from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import { NormalizationContext, ProviderNormalizerPort } from '../ports/provider-normalizer.port';

export interface CustomWebhookPayload {
  messageId: string;
  status: string;
  to?: string;
  from?: string;
  timestamp?: string;
  [key: string]: unknown;
}

@Injectable()
export class CustomNormalizer implements ProviderNormalizerPort {
  normalize(payload: unknown, context: NormalizationContext): CanonicalEvent[] {
    const p = payload as CustomWebhookPayload;
    return [
      buildCanonicalEvent({
        eventType: EventType.STATUS_EVENT,
        channel: Channel.SMS,
        source: EventSource.CUSTOM,
        correlationId: context.correlationId,
        timestamp: p.timestamp ?? new Date().toISOString(),
        metadata: { provider: 'custom' },
        payload: { messageId: p.messageId, status: p.status, to: p.to, from: p.from },
      }),
    ];
  }
}
