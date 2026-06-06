import { Controller, Get, Header } from '@nestjs/common';
import { MetricsService } from './metrics.service';

@Controller('metrics')
export class MetricsController {
  constructor(private readonly metrics: MetricsService) {}

  /**
   * Prometheus scrape endpoint. Returns the OpenMetrics text exposition
   * format with the registry's content type.
   */
  @Get()
  @Header('Cache-Control', 'no-cache')
  async scrape(): Promise<string> {
    return this.metrics.registry.metrics();
  }
}
