import { Controller, Get, Global, Module, Injectable } from '@nestjs/common';

/**
 * Simple in-process metrics — no prom-client dependency.
 * Exposes GET /metrics as a plain JSON snapshot.
 */
@Injectable()
export class MetricsService {
  private _broadcasts = 0;
  private _connectedClients = 0;
  private _redisFanoutIn = 0;
  private _redisFanoutOut = 0;

  incBroadcasts(): void { this._broadcasts++; }
  incConnectedClients(): void { this._connectedClients++; }
  decConnectedClients(): void { if (this._connectedClients > 0) this._connectedClients--; }
  incRedisFanout(dir: 'in' | 'out'): void {
    if (dir === 'in') this._redisFanoutIn++;
    else this._redisFanoutOut++;
  }

  snapshot() {
    return {
      broadcasts: this._broadcasts,
      connectedClients: this._connectedClients,
      redisFanout: { in: this._redisFanoutIn, out: this._redisFanoutOut },
      uptimeSeconds: Math.floor(process.uptime()),
    };
  }
}

@Controller('metrics')
export class MetricsController {
  constructor(private readonly metrics: MetricsService) {}
  @Get()
  scrape() {
    return this.metrics.snapshot();
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
  providers: [MetricsService],
  controllers: [MetricsController, HealthController],
  exports: [MetricsService],
})
export class ObservabilityModule {}
