import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
  isChannel,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  NormalizationContext,
  ProviderNormalizerPort,
} from '../ports/provider-normalizer.port';

/**
 * Shape accepted by the generic /integrations/custom/webhook endpoint.
 * Callers provide as much canonical structure as they have; channel is
 * required to keep the platform's invariants intact (HARDNESS §4).
 */
export interface CustomWebhookPayload {
  readonly eventType: EventType | string;
  readonly channel: Channel | string;
  readonly source?: string;
  readonly metadata?: Record<string, unknown>;
  readonly payload?: Record<string, unknown>;
  readonly timestamp?: string;
}

@Injectable()
export class CustomNormalizer
  implements ProviderNormalizerPort<CustomWebhookPayload>
{
  readonly providerName = EventSource.CUSTOM;

  normalize(
    payload: CustomWebhookPayload,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    if (!isChannel(payload.channel)) {
      throw new Error(
        `Custom webhook payload has invalid channel: ${String(payload.channel)}`,
      );
    }

    const event = buildCanonicalEvent({
      eventType: payload.eventType,
      channel: payload.channel,
      source: payload.source ?? EventSource.CUSTOM,
      correlationId: context.correlationId,
      timestamp: payload.timestamp,
      metadata: payload.metadata,
      payload: payload.payload,
    });

    return [event];
  }
}
