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
import { CORRELATION_ID_KAFKA_HEADER } from '@eventstream/utils';
import { EnvService } from '../../config/config.module';
import {
  AppLoggerService,
  CorrelationService,
} from '../../common/common-infra.module';
import { EventStream } from '../../domain/ports/event-broadcaster.port';

export type KafkaEventHandler = (
  stream: EventStream,
  event: CanonicalEvent,
  raw: EachMessagePayload,
) => Promise<void>;

@Injectable()
export class KafkaConsumerAdapter implements OnModuleInit, OnApplicationShutdown {
  private readonly nest = new Logger(KafkaConsumerAdapter.name);
  private readonly kafka: Kafka;
  private readonly consumer: Consumer;
  private running = false;
  private handler: KafkaEventHandler | null = null;

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
    });
  }

  registerHandler(handler: KafkaEventHandler): void {
    this.handler = handler;
  }

  isRunning(): boolean {
    return this.running;
  }

  async onModuleInit(): Promise<void> {
    await this.consumer.connect();
    await this.consumer.subscribe({
      topic: this.env.topicEventsProcessed,
      fromBeginning: false,
    });
    await this.consumer.subscribe({
      topic: this.env.topicEventsMetrics,
      fromBeginning: false,
    });

    await this.consumer.run({
      eachMessage: (payload) => this.dispatch(payload),
    });

    this.running = true;
    this.nest.log(
      `Kafka consumer subscribed (topics=${this.env.topicEventsProcessed},${this.env.topicEventsMetrics})`,
    );
  }

  async onApplicationShutdown(): Promise<void> {
    if (!this.running) return;
    await this.consumer.disconnect();
    this.running = false;
    this.nest.log('Kafka consumer disconnected');
  }

  private async dispatch(payload: EachMessagePayload): Promise<void> {
    if (!this.handler || !payload.message.value) return;
    let event: CanonicalEvent;
    try {
      event = JSON.parse(payload.message.value.toString('utf8')) as CanonicalEvent;
    } catch {
      return;
    }

    const stream: EventStream =
      payload.topic === this.env.topicEventsMetrics ? 'metrics' : 'events';
    const correlationId = this.readCorrelation(payload) ?? event.correlationId;
    await this.correlation.run(correlationId, () =>
      this.handler!(stream, event, payload),
    );
  }

  private readCorrelation(payload: EachMessagePayload): string | undefined {
    const headers = payload.message.headers ?? {};
    const raw = headers[CORRELATION_ID_KAFKA_HEADER];
    if (!raw) return undefined;
    if (Buffer.isBuffer(raw)) return raw.toString('utf8');
    return String(raw);
  }
}
