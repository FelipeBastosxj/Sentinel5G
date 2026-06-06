import { Injectable, OnModuleInit } from '@nestjs/common';
import { KafkaConsumerAdapter } from '../infrastructure/kafka/kafka-consumer.adapter';
import { RedisFanoutAdapter } from '../infrastructure/redis/redis-fanout.adapter';
import { BroadcastEventUseCase } from '../application/use-cases/broadcast-event.use-case';

/**
 * RealtimeWiring — connects:
 *   - KafkaConsumerAdapter → BroadcastEventUseCase.broadcastFromLocal()
 *   - RedisFanoutAdapter   → BroadcastEventUseCase.broadcastFromRemote()
 *
 * Lives at the boundary between infra adapters and the application use case
 * so the controllers/gateway stay framework-only.
 */
@Injectable()
export class RealtimeWiring implements OnModuleInit {
  constructor(
    private readonly consumer: KafkaConsumerAdapter,
    private readonly redis: RedisFanoutAdapter,
    private readonly useCase: BroadcastEventUseCase,
  ) {}

  async onModuleInit(): Promise<void> {
    this.consumer.registerHandler(async (stream, event) =>
      this.useCase.broadcastFromLocal(stream, event),
    );
    await this.redis.subscribe((stream, event) =>
      this.useCase.broadcastFromRemote(stream, event),
    );
  }
}
