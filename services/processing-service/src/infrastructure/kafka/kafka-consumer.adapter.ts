import {
  Injectable,
  Logger,
  OnApplicationShutdown,
  OnModuleInit,
} from '@nestjs/common';
import {
  Consumer,
  EachMessagePayload,
  Kafka,
  logLevel,
} from 'kafkajs';
import { CanonicalEvent } from '@eventstream/contracts';
import {
  CORRELATION_ID_KAFKA_HEADER,
  generateUuid,
} from '@eventstream/utils';
import { EnvService } from '../../config/config.module';
import { CorrelationService } from '../../common/correlation/correlation.module';
import { AppLoggerService } from '../../common/logger/logger.module';

export type EventConsumerHandler = (
  event: CanonicalEvent,
  raw: EachMessagePayload,
) => Promise<void>;

/**
 * KafkaConsumerAdapter — owns the kafkajs consumer lifecycle.
 *
 * Why an adapter and not a NestJS microservice transport:
 *   - we need explicit control over manual commits, DLQ, and per-message
 *     correlation context (AsyncLocalStorage).
 *   - the microservice transport hides too much for an observability platform.
 *
 * Lifecycle:
 *   - `onModuleInit`           connects + subscribes.
 *   - `onApplicationShutdown`  stops the consumer (graceful exit, HARDNESS §10).
 */
@Injectable()
export class KafkaConsumerAdapter implements OnModuleInit, OnApplicationShutdown {
  private readonly nest = new Logger(KafkaConsumerAdapter.name);
  private readonly kafka: Kafka;
  private readonly consumer: Consumer;
  private running = false;
  private handler: EventConsumerHandler | null = null;

  constructor(
    private readonly env: EnvService,
    private readonly correlation: CorrelationService,
    private readonly logger: AppLoggerService,
  ) {
    this.kafka = new Kafka({
      clientId: `${this.env.kafkaClientId}-consumer`,
      brokers: this.env.kafkaBrokers,
      logLevel: logLevel.WARN,
    });
    this.consumer = this.kafka.consumer({
      groupId: this.env.kafkaGroupId,
      allowAutoTopicCreation: true,
      retry: { retries: 8 },
    });
  }

  registerHandler(handler: EventConsumerHandler): void {
    this.handler = handler;
  }

  isRunning(): boolean {
    return this.running;
  }

  async onModuleInit(): Promise<void> {
    await this.consumer.connect();
    await this.consumer.subscribe({
      topic: this.env.topicEventsRaw,
      fromBeginning: false,
    });

    await this.consumer.run({
      eachMessage: async (payload) => this.dispatch(payload),
    });

    this.running = true;
    this.nest.log(
      `Kafka consumer subscribed (topic=${this.env.topicEventsRaw} group=${this.env.kafkaGroupId})`,
    );
  }

  async onApplicationShutdown(): Promise<void> {
    if (!this.running) return;
    await this.consumer.disconnect();
    this.running = false;
    this.nest.log('Kafka consumer disconnected');
  }

  private async dispatch(payload: EachMessagePayload): Promise<void> {
    if (!this.handler) return;
    if (!payload.message.value) return;

    const correlationId = this.readCorrelation(payload) ?? generateUuid();
    let event: CanonicalEvent;
    try {
      event = JSON.parse(payload.message.value.toString('utf8')) as CanonicalEvent;
    } catch (err) {
      this.logger.error('Failed to parse Kafka message JSON', {
        topic: payload.topic,
        offset: payload.message.offset,
        error: (err as Error).message,
      });
      return; // commit & skip — poison pill
    }

    await this.correlation.run(correlationId, () => this.handler!(event, payload));
  }

  private readCorrelation(payload: EachMessagePayload): string | undefined {
    const headers = payload.message.headers ?? {};
    const raw = headers[CORRELATION_ID_KAFKA_HEADER];
    if (!raw) return undefined;
    if (Buffer.isBuffer(raw)) return raw.toString('utf8');
    if (Array.isArray(raw)) {
      const first = raw[0];
      return Buffer.isBuffer(first) ? first.toString('utf8') : String(first);
    }
    return String(raw);
  }
}
