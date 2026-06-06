import { Injectable } from '@nestjs/common';
import { CanonicalEvent, EventType } from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import { EventEnricherPort } from '../ports/event-enricher.port';

/**
 * BaseEnricher — the platform's default enrichment pipeline.
 *
 * Currently:
 *   - normalizes phone numbers in `payload.to` / `payload.from` to E.164.
 *   - timestamps the moment processing happened (metadata.processedAt).
 *
 * Pure and immutable: returns a new event without touching the original.
 */
@Injectable()
export class BaseEnricher implements EventEnricherPort {
  enrich(event: CanonicalEvent): CanonicalEvent {
    const payload = (event.payload ?? {}) as Record<string, unknown>;
    const enrichedPayload = {
      ...payload,
      ...(typeof payload.to === 'string'
        ? { to: BaseEnricher.toE164(payload.to as string) }
        : {}),
      ...(typeof payload.from === 'string'
        ? { from: BaseEnricher.toE164(payload.from as string) }
        : {}),
    };

    return buildCanonicalEvent({
      eventType: event.eventType as EventType | string,
      channel: event.channel,
      source: event.source,
      correlationId: event.correlationId,
      eventId: event.eventId,
      timestamp: event.timestamp,
      version: event.version,
      metadata: {
        ...(event.metadata ?? {}),
        processedAt: new Date().toISOString(),
      },
      payload: enrichedPayload,
    });
  }

  /** Naive E.164 normalizer: strips spaces, dashes, parentheses. */
  private static toE164(phone: string): string {
    const cleaned = phone.replace(/[\s\-()]/g, '');
    return cleaned.startsWith('+') ? cleaned : `+${cleaned}`;
  }
}
