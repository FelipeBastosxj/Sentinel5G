import {
  Injectable,
  Logger,
  OnApplicationShutdown,
  OnModuleInit,
} from '@nestjs/common';
import Redis from 'ioredis';
import { EnvService } from '../../config/config.module';
import { AppLoggerService } from '../../common/common-infra.module';
import { MetricsService } from '../../common/observability.module';
import {
  RemoteFanoutHandler,
  RemoteFanoutPort,
} from '../../domain/ports/remote-fanout.port';
import { WebhookEventSummary } from '../../domain/ports/event-broadcaster.port';

const REDIS_CHANNEL = 'telecom-webhook.gateway';

interface FanoutMessage {
  readonly workspaceId: string;
  readonly event: WebhookEventSummary;
  /** Origin instance identifier — prevents echo loops. */
  readonly origin: string;
}

/**
 * RedisFanoutAdapter — multi-instance broadcast bus over Redis pub/sub.
 *
 * Each gateway instance:
 *   - publishes locally received events on REDIS_CHANNEL.
 *   - subscribes and forwards messages from peers to its WebSocket clients,
 *     skipping its own messages (origin check).
 */
@Injectable()
export class RedisFanoutAdapter
  implements RemoteFanoutPort, OnModuleInit, OnApplicationShutdown
{
  private readonly nest = new Logger(RedisFanoutAdapter.name);
  private readonly originId = `gw-${Math.random().toString(36).slice(2, 10)}`;
  private publisher!: Redis;
  private subscriber!: Redis;
  private handler: RemoteFanoutHandler | null = null;

  constructor(
    private readonly env: EnvService,
    private readonly logger: AppLoggerService,
    private readonly metrics: MetricsService,
  ) {}

  async onModuleInit(): Promise<void> {
    const opts = {
      host: this.env.redisHost,
      port: this.env.redisPort,
      password: this.env.redisPassword,
      lazyConnect: false,
    };
    this.publisher = new Redis(opts);
    this.subscriber = new Redis(opts);

    this.subscriber.on('message', (_channel, raw) => {
      try {
        const msg = JSON.parse(raw) as FanoutMessage;
        if (msg.origin === this.originId) return;
        this.metrics.incRedisFanout('in');
        this.handler?.(msg.workspaceId, msg.event);
      } catch (err) {
        this.logger.warn('Failed to parse redis fanout message', {
          error: (err as Error).message,
        });
      }
    });
    await this.subscriber.subscribe(REDIS_CHANNEL);
    this.nest.log(`Redis fanout subscribed on "${REDIS_CHANNEL}"`);
  }

  async onApplicationShutdown(): Promise<void> {
    await Promise.allSettled([
      this.publisher?.quit(),
      this.subscriber?.quit(),
    ]);
  }

  async publish(workspaceId: string, event: WebhookEventSummary): Promise<void> {
    const msg: FanoutMessage = { workspaceId, event, origin: this.originId };
    await this.publisher.publish(REDIS_CHANNEL, JSON.stringify(msg));
    this.metrics.incRedisFanout('out');
  }

  subscribe(handler: RemoteFanoutHandler): Promise<void> {
    this.handler = handler;
    return Promise.resolve();
  }
}
