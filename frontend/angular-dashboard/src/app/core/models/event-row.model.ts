import { CanonicalEvent } from '@eventstream/contracts';

/** UI-friendly view-model used across event tables and detail panels. */
export interface EventRow {
  readonly eventId: string;
  readonly eventType: string;
  readonly channel: string;
  readonly source: string;
  readonly timestamp: string;
  readonly correlationId: string;
  readonly raw: CanonicalEvent;
}

export function toEventRow(event: CanonicalEvent): EventRow {
  return {
    eventId: event.eventId,
    eventType: String(event.eventType),
    channel: String(event.channel),
    source: String(event.source),
    timestamp: event.timestamp,
    correlationId: event.correlationId,
    raw: event,
  };
}
