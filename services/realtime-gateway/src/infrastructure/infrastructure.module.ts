import { Module } from '@nestjs/common';
import { KafkaConsumerAdapter } from './kafka/kafka-consumer.adapter';
import { RedisFanoutAdapter } from './redis/redis-fanout.adapter';
import { REMOTE_FANOUT_PORT } from '../domain/ports/remote-fanout.port';

@Module({
  providers: [
    KafkaConsumerAdapter,
    RedisFanoutAdapter,
    { provide: REMOTE_FANOUT_PORT, useExisting: RedisFanoutAdapter },
  ],
  exports: [KafkaConsumerAdapter, RedisFanoutAdapter, REMOTE_FANOUT_PORT],
})
export class InfrastructureModule {}
