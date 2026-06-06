import { Global, Module, Injectable } from '@nestjs/common';
import { ConfigModule, ConfigService } from '@nestjs/config';

@Injectable()
export class EnvService {
  constructor(private readonly config: ConfigService) {}

  get nodeEnv(): string {
    return this.config.get<string>('NODE_ENV', 'development');
  }

  get logLevel(): 'debug' | 'info' | 'warn' | 'error' {
    return (this.config.get<string>('LOG_LEVEL', 'info') ?? 'info') as
      | 'debug'
      | 'info'
      | 'warn'
      | 'error';
  }

  get httpPort(): number {
    return Number(this.config.get<string>('PROCESSING_PORT', '3003'));
  }

  // -------- Kafka -------------------------------------------------------
  get kafkaBrokers(): string[] {
    return this.config
      .get<string>('KAFKA_BROKERS', 'localhost:9092')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  }

  get kafkaClientId(): string {
    return this.config.get<string>('KAFKA_CLIENT_ID', 'processing-service');
  }

  get kafkaGroupId(): string {
    return this.config.get<string>(
      'KAFKA_GROUP_ID',
      'eventstream.processing-service',
    );
  }

  get topicEventsRaw(): string {
    return this.config.get<string>('KAFKA_TOPIC_EVENTS_RAW', 'events.raw');
  }

  get topicEventsProcessed(): string {
    return this.config.get<string>(
      'KAFKA_TOPIC_EVENTS_PROCESSED',
      'events.processed',
    );
  }

  get topicEventsMetrics(): string {
    return this.config.get<string>(
      'KAFKA_TOPIC_EVENTS_METRICS',
      'events.metrics',
    );
  }

  get topicEventsAlerts(): string {
    return this.config.get<string>(
      'KAFKA_TOPIC_EVENTS_ALERTS',
      'events.alerts',
    );
  }

  // -------- Retry -------------------------------------------------------
  get retryAttempts(): number {
    return Number(this.config.get<string>('PROCESSING_SERVICE_RETRY_ATTEMPTS', '3'));
  }

  get retryBaseDelayMs(): number {
    return Number(this.config.get<string>('PROCESSING_SERVICE_RETRY_DELAY_MS', '1000'));
  }

  get otelServiceName(): string {
    return this.config.get<string>(
      'OTEL_SERVICE_NAME_PROCESSING',
      'processing-service',
    );
  }

  // -------- ClickHouse --------------------------------------------------
  get clickhouseHost(): string {
    return this.config.get<string>('CLICKHOUSE_HOST', 'localhost');
  }

  get clickhousePort(): number {
    return Number(this.config.get<string>('CLICKHOUSE_PORT', '8123'));
  }

  get clickhouseDatabase(): string {
    return this.config.get<string>('CLICKHOUSE_DATABASE', 'eventstream');
  }

  get clickhouseUser(): string {
    return this.config.get<string>('CLICKHOUSE_USER', 'default');
  }

  get clickhousePassword(): string {
    return this.config.get<string>('CLICKHOUSE_PASSWORD', '');
  }
}

@Global()
@Module({
  imports: [
    ConfigModule.forRoot({
      isGlobal: true,
      cache: true,
      envFilePath: ['.env', '../../.env'],
    }),
  ],
  providers: [EnvService],
  exports: [EnvService],
})
export class AppConfigModule {}
