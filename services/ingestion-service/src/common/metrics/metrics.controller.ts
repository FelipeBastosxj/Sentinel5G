import { Controller, Get } from '@nestjs/common';
import { MetricsService } from './metrics.service';

@Controller('metrics')
export class MetricsController {
  constructor(private readonly metrics: MetricsService) {}

  /** Returns simple in-process counters as JSON. */
  @Get()
  snapshot(): Record<string, number> {
    return this.metrics.snapshot();
  }
}
