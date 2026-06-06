import { Module } from '@nestjs/common';
import { KafkaProducerAdapter } from './kafka-producer.adapter';
import { EVENT_PUBLISHER_PORT } from '../../domain/ports/event-publisher.port';

@Module({
  providers: [
    KafkaProducerAdapter,
    {
      provide: EVENT_PUBLISHER_PORT,
      useExisting: KafkaProducerAdapter,
    },
  ],
  exports: [KafkaProducerAdapter, EVENT_PUBLISHER_PORT],
})
export class KafkaModule {}
