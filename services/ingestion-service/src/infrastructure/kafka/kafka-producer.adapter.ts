import {
  Injectable,
  Logger,
  OnApplicationShutdown,
  OnModuleInit,
} from '@nestjs/common';
import { Kafka, logLevel, Producer } from 'kafkajs';
import { CanonicalEvent } from '@eventstream/contracts';
import { CORRELATION_ID_KAFKA_HEADER } from '@eventstream/utils';
import { EnvService } from '../../config/env.service';
import { EventPublisherPort } from '../../domain/ports/event-publisher.port';
import { MetricsService } from '../../common/metrics/metrics.service';
import { AppLoggerService } from '../../common/logger/logger.module';

/**
 * Adapter that fulfills {@link EventPublisherPort} using kafkajs.
 *
 * Lifecycle is bound to NestJS:
 *   - `onModuleInit`         connects the producer.
 *   - `onApplicationShutdown` flushes & disconnects gracefully.
 *
 * Producer settings:
 *   - acks=-1 (all in-sync replicas) for durability.
 *   - idempotent retries to prevent duplicates.
 */
@Injectable()
export class KafkaProducerAdapter
  implements EventPublisherPort, OnModuleInit, OnApplicationShutdown
{
  private readonly internalLogger = new Logger(KafkaProducerAdapter.name);
  private readonly kafka: Kafka;
  private readonly producer: Producer;
  private connected = false;

  constructor(
    private readonly env: EnvService,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {
    this.kafka = new Kafka({
      clientId: this.env.kafkaClientId,
      brokers: this.env.kafkaBrokers,
      logLevel: logLevel.WARN,
    });
    this.producer = this.kafka.producer({
      allowAutoTopicCreation: true,
      idempotent: true,
      retry: { retries: 8, initialRetryTime: 200 },
    });
  }

  async onModuleInit(): Promise<void> {
    try {
      await this.producer.connect();
      this.connected = true;
      this.internalLogger.log(
        `Kafka producer connected (brokers=${this.env.kafkaBrokers.join(',')})`,
      );
    } catch (err) {
      this.internalLogger.error('Kafka producer failed to connect', err as Error);
      throw err;
    }
  }

  async onApplicationShutdown(): Promise<void> {
    if (!this.connected) return;
    await this.producer.disconnect();
    this.connected = false;
    this.internalLogger.log('Kafka producer disconnected');
  }

  isConnected(): boolean {
    return this.connected;
  }

  async publish(topic: string, event: CanonicalEvent): Promise<void> {
    const stop = this.metrics.publishLatencySeconds.startTimer({ topic });
    try {
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
      this.metrics.eventsPublishedTotal.inc({ topic });
      this.logger.debug('Event published', {
        topic,
        eventId: event.eventId,
        eventType: event.eventType,
      });
    } finally {
      stop();
    }
  }
}
