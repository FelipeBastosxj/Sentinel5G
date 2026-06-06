import { Controller, Get, Header, Global, Module, Injectable } from '@nestjs/common';
import { Counter, Gauge, Registry, collectDefaultMetrics } from 'prom-client';

@Injectable()
export class MetricsService {
  readonly registry = new Registry();

  readonly broadcastsTotal = new Counter({
    name: 'gateway_broadcasts_total',
    help: 'Events broadcast over WebSocket',
    labelNames: ['stream'] as const,
    registers: [this.registry],
  });

  readonly connectedClients = new Gauge({
    name: 'gateway_connected_clients',
    help: 'Currently connected WebSocket clients',
    registers: [this.registry],
  });

  readonly redisFanoutTotal = new Counter({
    name: 'gateway_redis_fanout_total',
    help: 'Messages broadcast via Redis pub/sub for multi-instance fanout',
    labelNames: ['direction'] as const,
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

@Controller('health')
export class HealthController {
  @Get()
  check(): { status: 'ok'; uptimeSeconds: number } {
    return { status: 'ok', uptimeSeconds: Math.floor(process.uptime()) };
  }
}

@Global()
@Module({
  controllers: [MetricsController, HealthController],
  providers: [MetricsService],
  exports: [MetricsService],
})
export class ObservabilityModule {}
