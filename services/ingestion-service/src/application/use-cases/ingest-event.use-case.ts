import { Inject, Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  KafkaTopic,
} from '@eventstream/contracts';
import {
  canonicalEventSchema,
  validateOrThrow,
} from '@eventstream/schemas';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  EVENT_PUBLISHER_PORT,
  EventPublisherPort,
} from '../../domain/ports/event-publisher.port';
import { MetricsService } from '../../common/metrics/metrics.service';
import { AppLoggerService } from '../../common/logger/logger.module';
import { CorrelationService } from '../../common/correlation/correlation.service';

export interface IngestEventCommand {
  /** Canonical event submitted by an upstream caller. */
  readonly payload: unknown;
}

/**
 * Use case: validate an inbound CanonicalEvent and publish it to `events.raw`.
 *
 * Flow (HARDNESS §4, §5, §6):
 *   1. Validate against the canonical schema. Throws `SchemaValidationError`
 *      on failure (caught by the global exception filter).
 *   2. Backfill missing optional fields (eventId, timestamp, version) using
 *      {@link buildCanonicalEvent}.
 *   3. Publish to Kafka via the outbound port.
 *
 * The use case has no awareness of HTTP, Kafka client implementation, or
 * Prometheus internals — it depends only on ports and shared utilities.
 */
@Injectable()
export class IngestEventUseCase {
  constructor(
    @Inject(EVENT_PUBLISHER_PORT) private readonly publisher: EventPublisherPort,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
    private readonly correlation: CorrelationService,
  ) {}

  async execute(command: IngestEventCommand): Promise<CanonicalEvent> {
    let validated: CanonicalEvent;
    try {
      const parsed = validateOrThrow(canonicalEventSchema, command.payload);
      validated = parsed as CanonicalEvent;
    } catch (err) {
      this.metrics.eventsRejectedTotal.inc({ reason: 'schema' });
      throw err;
    }

    const correlationId =
      validated.correlationId ??
      this.correlation.getCorrelationId() ??
      'unknown';

    const event = buildCanonicalEvent({
      eventType: validated.eventType,
      channel: validated.channel,
      source: validated.source,
      correlationId,
      eventId: validated.eventId,
      timestamp: validated.timestamp,
      version: validated.version,
      metadata: validated.metadata,
      payload: validated.payload as Record<string, unknown> | undefined,
    });

    this.metrics.eventsReceivedTotal.inc({
      source: event.source,
      channel: event.channel,
      eventType: String(event.eventType),
    });

    await this.publisher.publish(KafkaTopic.EVENTS_RAW, event);

    this.logger.info('CanonicalEvent ingested', {
      eventId: event.eventId,
      eventType: event.eventType,
      channel: event.channel,
      source: event.source,
    });

    return event;
  }
}
