import { Controller, Get, Header, Global, Module, Injectable } from '@nestjs/common';
import { Counter, Histogram, Registry, collectDefaultMetrics } from 'prom-client';

@Injectable()
export class MetricsService {
  readonly registry = new Registry();

  readonly eventsConsumedTotal = new Counter({
    name: 'processing_events_consumed_total',
    help: 'Events consumed from events.raw',
    labelNames: ['source', 'channel'] as const,
    registers: [this.registry],
  });

  readonly eventsProcessedTotal = new Counter({
    name: 'processing_events_processed_total',
    help: 'Events successfully published to events.processed',
    labelNames: ['source', 'channel'] as const,
    registers: [this.registry],
  });

  readonly eventsRetriedTotal = new Counter({
    name: 'processing_events_retried_total',
    help: 'Retries triggered while processing an event',
    labelNames: ['attempt'] as const,
    registers: [this.registry],
  });

  readonly eventsDeadLetteredTotal = new Counter({
    name: 'processing_events_dead_lettered_total',
    help: 'Events sent to events.alerts after exhausting retries',
    labelNames: ['source'] as const,
    registers: [this.registry],
  });

  readonly processingDurationSeconds = new Histogram({
    name: 'processing_duration_seconds',
    help: 'End-to-end duration to process an event',
    labelNames: ['outcome'] as const,
    buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10],
    registers: [this.registry],
  });

  constructor() {
    collectDefaultMetrics({ register: this.registry });
  }
}

@Controller('metrics')
export class MetricsController {
  constructor(private readonly metrics: MetricsService) {}

  @Get()
  @Header('Cache-Control', 'no-cache')
  scrape(): Promise<string> {
    return this.metrics.registry.metrics();
  }
}

@Global()
@Module({
  controllers: [MetricsController],
  providers: [MetricsService],
  exports: [MetricsService],
})
export class MetricsModule {}
