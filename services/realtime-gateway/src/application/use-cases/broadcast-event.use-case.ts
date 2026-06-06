import { Inject, Injectable } from '@nestjs/common';
import {
  EVENT_BROADCASTER_PORT,
  EventBroadcasterPort,
  WebhookEventSummary,
} from '../../domain/ports/event-broadcaster.port';
import {
  REMOTE_FANOUT_PORT,
  RemoteFanoutPort,
} from '../../domain/ports/remote-fanout.port';
import { MetricsService } from '../../common/observability.module';
import { AppLoggerService } from '../../common/common-infra.module';

/**
 * BroadcastEventUseCase — delivers a WebhookEventSummary to all connected
 * WebSocket clients for the given workspace.
 *
 * Two execution modes:
 *   - broadcastFromLocal  — event arrived from pg_notify on this instance;
 *     we broadcast locally and fan out to peers via Redis.
 *   - broadcastFromRemote — event arrived from a Redis peer; broadcast locally
 *     only (no re-publish to avoid echo loops).
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

  async broadcastFromLocal(
    workspaceId: string,
    event: WebhookEventSummary,
  ): Promise<void> {
    this.broadcaster.broadcast(workspaceId, event);
    try {
      await this.fanout.publish(workspaceId, event);
    } catch (err) {
      this.logger.warn('Redis fanout publish failed (broadcast continues)', {
        error: (err as Error).message,
      });
    }
  }

  broadcastFromRemote(workspaceId: string, event: WebhookEventSummary): void {
    this.broadcaster.broadcast(workspaceId, event);
  }
}
