import {
  Injectable,
  Logger,
  OnApplicationShutdown,
} from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import { CORRELATION_ID_HEADER } from '@eventstream/utils';
import { EnvService } from '../../config/env.service';
import { CanonicalEventForwarderPort } from '../../domain/ports/canonical-event-forwarder.port';
import { MetricsService } from '../../common/metrics/metrics.module';
import { AppLoggerService } from '../../common/logger/logger.module';

/**
 * Forwards normalized CanonicalEvents to the ingestion-service over HTTP.
 *
 * Uses Node's built-in `fetch` (engines.node >= 22). No third-party HTTP
 * library is needed, which keeps the dependency surface small.
 *
 * Failure mode:
 *   - Non-2xx responses raise an Error so the calling controller answers 502.
 *   - Network errors propagate the same way.
 */
@Injectable()
export class IngestionHttpClient
  implements CanonicalEventForwarderPort, OnApplicationShutdown
{
  private readonly nestLogger = new Logger(IngestionHttpClient.name);
  private readonly abortController = new AbortController();

  constructor(
    private readonly env: EnvService,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {}

  onApplicationShutdown(): void {
    this.abortController.abort('shutdown');
  }

  async forward(event: CanonicalEvent): Promise<void> {
    const url = `${this.env.ingestionUrl.replace(/\/$/, '')}/ingest`;
    const stop = this.metrics.forwardLatencySeconds.startTimer({
      provider: String(event.source),
    });
    try {
      const response = await fetch(url, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          [CORRELATION_ID_HEADER]: event.correlationId,
        },
        body: JSON.stringify(event),
        signal: this.abortController.signal,
      });

      if (!response.ok) {
        const body = await response.text().catch(() => '');
        const err = new Error(
          `Ingestion forward failed (${response.status}): ${body}`,
        );
        this.nestLogger.error(err.message);
        throw err;
      }

      this.metrics.webhookForwardedTotal.inc({
        provider: String(event.source),
      });
      this.logger.debug('Event forwarded to ingestion-service', {
        eventId: event.eventId,
        url,
      });
    } finally {
      stop();
    }
  }
}
