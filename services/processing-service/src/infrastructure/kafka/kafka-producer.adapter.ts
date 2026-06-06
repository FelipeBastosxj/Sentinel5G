import {
  Injectable,
  Logger,
  OnApplicationShutdown,
  OnModuleInit,
} from '@nestjs/common';
import { Kafka, logLevel, Producer } from 'kafkajs';
import { CanonicalEvent } from '@eventstream/contracts';
import { CORRELATION_ID_KAFKA_HEADER } from '@eventstream/utils';
import { EnvService } from '../../config/config.module';
import { EventPublisherPort } from '../../domain/ports/event-publisher.port';
import { AppLoggerService } from '../../common/logger/logger.module';

@Injectable()
export class KafkaProducerAdapter
  implements EventPublisherPort, OnModuleInit, OnApplicationShutdown
{
  private readonly nest = new Logger(KafkaProducerAdapter.name);
  private readonly kafka: Kafka;
  private readonly producer: Producer;
  private connected = false;

  constructor(
    private readonly env: EnvService,
    private readonly logger: AppLoggerService,
  ) {
    this.kafka = new Kafka({
      clientId: `${this.env.kafkaClientId}-producer`,
      brokers: this.env.kafkaBrokers,
      logLevel: logLevel.WARN,
    });
    this.producer = this.kafka.producer({
      idempotent: true,
      allowAutoTopicCreation: true,
      retry: { retries: 8, initialRetryTime: 200 },
    });
  }

  async onModuleInit(): Promise<void> {
    await this.producer.connect();
    this.connected = true;
    this.nest.log('Kafka producer connected');
  }

  async onApplicationShutdown(): Promise<void> {
    if (!this.connected) return;
    await this.producer.disconnect();
    this.connected = false;
    this.nest.log('Kafka producer disconnected');
  }

  isConnected(): boolean {
    return this.connected;
  }

  async publish(topic: string, event: CanonicalEvent): Promise<void> {
    await this.producer.send({
      topic,
      messages: [
        {
          key: event.eventId,
          value: JSON.stringify(event),
          headers: {
            [CORRELATION_ID_KAFKA_HEADER]: event.correlationId,
            'event-type': String(event.eventType),
            channel: String(event.channel),
            source: String(event.source),
          },
        },
      ],
    });
    this.logger.debug('Event published', { topic, eventId: event.eventId });
  }
}
