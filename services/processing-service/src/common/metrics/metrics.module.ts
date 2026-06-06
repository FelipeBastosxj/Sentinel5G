import { Controller, Get, Global, Module, Injectable } from '@nestjs/common';

/**
 * Simple in-process metrics — prom-client removed from MVP.
 * Exposes GET /metrics as a plain JSON snapshot.
 */
@Injectable()
export class MetricsService {
  private _processed = 0;
  private _errors = 0;

  incProcessed(): void { this._processed++; }
  incErrors(): void { this._errors++; }

  snapshot() {
    return {
      processed: this._processed,
      errors: this._errors,
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

@Global()
@Module({
  controllers: [MetricsController],
  providers: [MetricsService],
  exports: [MetricsService],
})
export class MetricsModule {}
