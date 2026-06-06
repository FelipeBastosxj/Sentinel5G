import { Injectable } from '@nestjs/common';
import { Counter, Histogram, Registry, collectDefaultMetrics } from 'prom-client';

/**
 * Prometheus registry + canonical counters/histograms for the ingestion-service.
 *
 * HARDNESS §8: every service must expose metrics. Centralizing instrument
 * creation prevents accidental duplicates that would crash prom-client.
 */
@Injectable()
export class MetricsService {
  readonly registry = new Registry();

  readonly eventsReceivedTotal = new Counter({
    name: 'ingestion_events_received_total',
    help: 'Total CanonicalEvents received by the ingestion endpoint.',
    labelNames: ['source', 'channel', 'eventType'] as const,
    registers: [this.registry],
  });

  readonly eventsRejectedTotal = new Counter({
    name: 'ingestion_events_rejected_total',
    help: 'Total CanonicalEvents rejected by schema validation.',
    labelNames: ['reason'] as const,
    registers: [this.registry],
  });

  readonly eventsPublishedTotal = new Counter({
    name: 'ingestion_events_published_total',
    help: 'Total CanonicalEvents successfully published to events.raw.',
    labelNames: ['topic'] as const,
    registers: [this.registry],
  });

  readonly publishLatencySeconds = new Histogram({
    name: 'ingestion_publish_latency_seconds',
    help: 'Latency of the Kafka publish call in seconds.',
    labelNames: ['topic'] as const,
    buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5],
    registers: [this.registry],
  });

  constructor() {
    collectDefaultMetrics({ register: this.registry });
  }
}
