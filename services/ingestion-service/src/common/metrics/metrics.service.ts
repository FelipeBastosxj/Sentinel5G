import { Injectable } from '@nestjs/common';

/**
 * Simple in-process counters for the ingestion-service.
 *
 * Prometheus / prom-client removed from MVP. Metrics are derived from
 * the PostgreSQL `webhook_events` table in the processing-service.
 * A `/metrics` endpoint can be added in Phase 5 if needed.
 */
@Injectable()
export class MetricsService {
  private _received = 0;
  private _forwarded = 0;
  private _errors = 0;

  inc(type: 'received' | 'forwarded' | 'error'): void {
    if (type === 'received') this._received++;
    else if (type === 'forwarded') this._forwarded++;
    else this._errors++;
  }

  snapshot(): Record<string, number> {
    return {
      received: this._received,
      forwarded: this._forwarded,
      errors: this._errors,
    };
  }
}
