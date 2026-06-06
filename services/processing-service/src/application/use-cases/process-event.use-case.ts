import { Inject, Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  EventType,
  KafkaTopic,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  EVENT_ENRICHER_PORT,
  EventEnricherPort,
} from '../../domain/ports/event-enricher.port';
import {
  EVENT_PUBLISHER_PORT,
  EventPublisherPort,
} from '../../domain/ports/event-publisher.port';
import {
  EVENT_STORE_PORT,
  EventStorePort,
} from '../../domain/ports/event-store.port';
import { RetryPolicy } from '../../domain/retry/retry.policy';
import { EnvService } from '../../config/config.module';
import { MetricsService } from '../../common/metrics/metrics.module';
import { AppLoggerService } from '../../common/logger/logger.module';

/**
 * Use case: process a single CanonicalEvent.
 *
 * Pipeline (HARDNESS §6, §10):
 *   1. enrich the event.
 *   2. publish to `events.processed` with retry.
 *   3. emit a metric event to `events.metrics`.
 *   4. on exhausted retries, publish to `events.alerts` (DLQ).
 */
@Injectable()
export class ProcessEventUseCase {
  constructor(
    @Inject(EVENT_ENRICHER_PORT) private readonly enricher: EventEnricherPort,
    @Inject(EVENT_PUBLISHER_PORT) private readonly publisher: EventPublisherPort,
    @Inject(EVENT_STORE_PORT)    private readonly store: EventStorePort,
    private readonly retry: RetryPolicy,
    private readonly env: EnvService,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {}

  async execute(raw: CanonicalEvent): Promise<void> {
    const stop = this.metrics.processingDurationSeconds.startTimer();
    this.metrics.eventsConsumedTotal.inc({
      source: String(raw.source),
      channel: String(raw.channel),
    });

    const enriched = this.enricher.enrich(raw);

    try {
      await this.retry.run(
        () => this.publisher.publish(KafkaTopic.EVENTS_PROCESSED, enriched),
        { attempts: this.env.retryAttempts, baseDelayMs: this.env.retryBaseDelayMs },
        (attempt, err) => {
          this.metrics.eventsRetriedTotal.inc({ attempt: String(attempt) });
          this.logger.warn('Retrying publish to events.processed', {
            attempt,
            error: err.message,
            eventId: enriched.eventId,
          });
        },
      );
      this.metrics.eventsProcessedTotal.inc({
        source: String(enriched.source),
        channel: String(enriched.channel),
      });
      stop({ outcome: 'success' });

      // Persist to ClickHouse for historical queries (non-blocking, non-fatal)
      this.store.save(enriched).catch((err) => {
        this.logger.warn('ClickHouse save failed (non-fatal)', {
          error: (err as Error).message,
          eventId: enriched.eventId,
        });
      });

      // Emit a derived metric event for downstream observability.
      const metricEvent = buildCanonicalEvent({
        eventType: EventType.METRIC_EVENT,
        channel: enriched.channel,
        source: 'processing-service',
        correlationId: enriched.correlationId,
        metadata: { sourceEventId: enriched.eventId },
        payload: { eventType: enriched.eventType, processedAt: new Date().toISOString() },
      });
      await this.publisher
        .publish(KafkaTopic.EVENTS_METRICS, metricEvent)
        .catch((err) => {
          this.logger.warn('Failed to emit metric event (non-fatal)', {
            error: (err as Error).message,
          });
        });
    } catch (err) {
      stop({ outcome: 'failure' });
      this.metrics.eventsDeadLetteredTotal.inc({ source: String(raw.source) });
      this.logger.error('Event dead-lettered after retries', {
        eventId: enriched.eventId,
        error: (err as Error).message,
      });
      await this.sendToDLQ(enriched, err as Error);
    }
  }

  private async sendToDLQ(event: CanonicalEvent, error: Error): Promise<void> {
    const alert = buildCanonicalEvent({
      eventType: EventType.ALERT_EVENT,
      channel: event.channel,
      source: 'processing-service',
      correlationId: event.correlationId,
      metadata: {
        ...(event.metadata ?? {}),
        deadLetter: true,
        originalEventId: event.eventId,
        errorName: error.name,
        errorMessage: error.message,
      },
      payload: event.payload as Record<string, unknown> | undefined,
    });
    try {
      await this.publisher.publish(KafkaTopic.EVENTS_ALERTS, alert);
    } catch (dlqErr) {
      this.logger.error('CRITICAL: failed to publish DLQ event', {
        eventId: event.eventId,
        error: (dlqErr as Error).message,
      });
    }
  }
}
