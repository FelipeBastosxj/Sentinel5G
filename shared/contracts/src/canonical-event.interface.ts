import { EventType } from './event-type.enum';

/**
 * CanonicalEvent — the core data contract of the entire platform.
 *
 * Every event that enters the platform MUST be normalized into this structure
 * before being published to Kafka. Provider-specific payloads must never
 * pass this boundary in raw form.
 *
 * Rules (see HARDNESS.md §5):
 *   - eventId, eventType, timestamp, source, and correlationId are required.
 *   - Events are immutable after creation.
 *   - Events are versioned via the `version` field.
 *
 * @template T  The shape of the `payload` field. Defaults to a generic record.
 */
export interface CanonicalEvent<T = Record<string, unknown>> {
  /**
   * Unique identifier for this event instance.
   * Must be a v4 UUID generated at ingestion time.
   */
  eventId: string;

  /**
   * The type of event. Must be a value from the EventType enum
   * or a custom string for extensibility with third-party integrations.
   */
  eventType: EventType | string;

  /**
   * ISO 8601 timestamp indicating when the event was created.
   * @example "2026-06-05T12:00:00.000Z"
   */
  timestamp: string;

  /**
   * The origin system or provider that produced this event.
   * @example "twilio" | "infobip" | "sendgrid" | "internal"
   */
  source: string;

  /**
   * Correlation ID for distributed tracing across the entire event flow.
   * Must be propagated through all services and logged with every operation.
   */
  correlationId: string;

  /**
   * Schema version for backward-compatibility tracking.
   * @default "1.0"
   */
  version?: string;

  /**
   * Arbitrary key-value metadata for routing, filtering, or tagging.
   * Must not contain sensitive information.
   */
  metadata?: Record<string, unknown>;

  /**
   * Provider-specific or event-specific data payload.
   * Strongly typed via the generic parameter T.
   */
  payload?: T;
}
