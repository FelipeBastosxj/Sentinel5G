import { Global, Module, Injectable } from '@nestjs/common';
import { Counter, Histogram, Registry, collectDefaultMetrics } from 'prom-client';
import { Controller, Get, Header } from '@nestjs/common';

@Injectable()
export class MetricsService {
  readonly registry = new Registry();

  readonly webhookReceivedTotal = new Counter({
    name: 'webhook_received_total',
    help: 'Total provider webhook calls received.',
    labelNames: ['provider', 'channel'] as const,
    registers: [this.registry],
  });

  readonly webhookRejectedTotal = new Counter({
    name: 'webhook_rejected_total',
    help: 'Total provider webhook calls rejected (signature/schema).',
    labelNames: ['provider', 'reason'] as const,
    registers: [this.registry],
  });

  readonly webhookForwardedTotal = new Counter({
    name: 'webhook_forwarded_total',
    help: 'Total CanonicalEvents forwarded to the ingestion service.',
    labelNames: ['provider'] as const,
    registers: [this.registry],
  });

  readonly forwardLatencySeconds = new Histogram({
    name: 'webhook_forward_latency_seconds',
    help: 'Latency forwarding events to ingestion-service.',
    labelNames: ['provider'] as const,
    buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5],
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
