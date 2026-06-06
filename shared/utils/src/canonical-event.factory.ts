import {
  CanonicalEvent,
  CANONICAL_EVENT_VERSION,
  Channel,
  EventType,
} from '@eventstream/contracts';
import { generateUuid } from './uuid.util';
import { nowIsoUtc } from './time.util';

/**
 * Input shape for {@link buildCanonicalEvent}. Required identity fields are
 * mandatory; eventId / timestamp / version are auto-filled when omitted.
 */
export interface BuildCanonicalEventInput<T = Record<string, unknown>> {
  readonly eventType: EventType | string;
  readonly channel: Channel;
  readonly source: string;
  readonly correlationId: string;
  readonly eventId?: string;
  readonly timestamp?: string;
  readonly version?: string;
  readonly metadata?: Record<string, unknown>;
  readonly payload?: T;
}

/**
 * Factory that builds a fully-formed CanonicalEvent.
 *
 * Centralizing construction here guarantees that every event published to
 * Kafka carries the mandatory fields defined by HARDNESS §4 and obeys the
 * immutability contract (object frozen before being returned).
 */
export function buildCanonicalEvent<T = Record<string, unknown>>(
  input: BuildCanonicalEventInput<T>,
): CanonicalEvent<T> {
  const event: CanonicalEvent<T> = {
    eventId: input.eventId ?? generateUuid(),
    eventType: input.eventType,
    channel: input.channel,
    timestamp: input.timestamp ?? nowIsoUtc(),
    source: input.source,
    correlationId: input.correlationId,
    version: input.version ?? CANONICAL_EVENT_VERSION,
    metadata: input.metadata,
    payload: input.payload,
  };
  // Enforces immutability at the value level. Kafka serialization is unaffected.
  return Object.freeze(event);
}
