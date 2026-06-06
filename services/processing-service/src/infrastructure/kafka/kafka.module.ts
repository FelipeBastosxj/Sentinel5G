import { Module } from '@nestjs/common';
import { KafkaProducerAdapter } from './kafka-producer.adapter';
import { KafkaConsumerAdapter } from './kafka-consumer.adapter';
import { EVENT_PUBLISHER_PORT } from '../../domain/ports/event-publisher.port';

@Module({
  providers: [
    KafkaProducerAdapter,
    KafkaConsumerAdapter,
    { provide: EVENT_PUBLISHER_PORT, useExisting: KafkaProducerAdapter },
  ],
  exports: [
    KafkaProducerAdapter,
    KafkaConsumerAdapter,
    EVENT_PUBLISHER_PORT,
  ],
})
export class KafkaModule {}
