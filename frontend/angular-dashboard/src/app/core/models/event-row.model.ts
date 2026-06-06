import { WebhookEventSummary } from '@telecom-webhook/contracts';

/** UI-friendly view-model used across event tables and detail panels. */
export interface EventRow {
  readonly id: string;
  readonly provider: string;
  readonly eventType: string;
  readonly from: string;
  readonly to: string;
  readonly status: string;
  readonly receivedAt: string;
  readonly raw: WebhookEventSummary;
}

export function toEventRow(event: WebhookEventSummary): EventRow {
  return {
    id:         event.id,
    provider:   event.provider,
    eventType:  event.eventType,
    from:       event.from    ?? '—',
    to:         event.to      ?? '—',
    status:     event.status  ?? '—',
    receivedAt: event.receivedAt instanceof Date
      ? event.receivedAt.toISOString()
      : String(event.receivedAt),
    raw: event,
  };
}

