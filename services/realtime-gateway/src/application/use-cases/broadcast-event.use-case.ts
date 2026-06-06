import { Inject, Injectable } from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import {
  EVENT_BROADCASTER_PORT,
  EventBroadcasterPort,
  EventStream,
} from '../../domain/ports/event-broadcaster.port';
import {
  REMOTE_FANOUT_PORT,
  RemoteFanoutPort,
} from '../../domain/ports/remote-fanout.port';
import { MetricsService } from '../../common/observability.module';
import { AppLoggerService } from '../../common/common-infra.module';

/**
 * Use case: take a CanonicalEvent that came in (locally from Kafka, or remotely
 * via Redis pub/sub) and broadcast it to every connected WebSocket client.
 *
 * Two execution modes:
 *   - {@link broadcastFromLocal}  — event came from this instance's Kafka
 *     consumer; we both fan it out to peers and broadcast locally.
 *   - {@link broadcastFromRemote} — event came from a peer; broadcast locally
 *     only to avoid echo loops.
 */
@Injectable()
export class BroadcastEventUseCase {
  constructor(
    @Inject(EVENT_BROADCASTER_PORT)
    private readonly broadcaster: EventBroadcasterPort,
    @Inject(REMOTE_FANOUT_PORT) private readonly fanout: RemoteFanoutPort,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {}

  async broadcastFromLocal(stream: EventStream, event: CanonicalEvent): Promise<void> {
    this.broadcaster.broadcast(stream, event);
    this.metrics.broadcastsTotal.inc({ stream });
    try {
      await this.fanout.publish(stream, event);
    } catch (err) {
      this.logger.warn('Redis fanout publish failed (broadcast continues)', {
        error: (err as Error).message,
      });
    }
  }

  broadcastFromRemote(stream: EventStream, event: CanonicalEvent): void {
    this.broadcaster.broadcast(stream, event);
    this.metrics.broadcastsTotal.inc({ stream });
  }
}
