import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { PgListenerAdapter } from '../infrastructure/pg-listener/pg-listener.adapter';
import { RedisFanoutAdapter } from '../infrastructure/redis/redis-fanout.adapter';
import { BroadcastEventUseCase } from '../application/use-cases/broadcast-event.use-case';

/**
 * RealtimeWiring — connects:
 *   - PgListenerAdapter  -> BroadcastEventUseCase.broadcastFromLocal()
 *   - RedisFanoutAdapter -> BroadcastEventUseCase.broadcastFromRemote()
 */
@Injectable()
export class RealtimeWiring implements OnModuleInit {
  private readonly logger = new Logger(RealtimeWiring.name);

  constructor(
    private readonly pgListener: PgListenerAdapter,
    private readonly redis: RedisFanoutAdapter,
    private readonly useCase: BroadcastEventUseCase,
  ) {}

  async onModuleInit(): Promise<void> {
    this.pgListener.registerHandler((raw) => {
      try {
        const event = JSON.parse(raw);
        void this.useCase.broadcastFromLocal(event.workspaceId, event);
      } catch (err) {
        this.logger.warn('Malformed pg_notify payload', (err as Error).message);
      }
    });

    await this.redis.subscribe((workspaceId, event) =>
      this.useCase.broadcastFromRemote(workspaceId, event),
    );
  }
}
