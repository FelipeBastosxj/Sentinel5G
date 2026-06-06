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
    return Number(this.config.get<string>('REALTIME_GATEWAY_PORT', '3003'));
  }

  get wsPath(): string {
    return this.config.get<string>('REALTIME_GATEWAY_WS_PATH', '/ws');
  }

  get corsOrigin(): string[] {
    return this.config
      .get<string>('REALTIME_GATEWAY_CORS_ORIGIN', 'http://localhost:4200')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
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
    return this.config.get<string>('KAFKA_CLIENT_ID', 'realtime-gateway');
  }

  get kafkaGroupId(): string {
    // Each gateway instance should join the same group for load-balanced
    // partition assignment, but we allow override via env for fan-out modes.
    return this.config.get<string>(
      'KAFKA_GROUP_ID_GATEWAY',
      'eventstream.realtime-gateway',
    );
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

  // -------- Redis -------------------------------------------------------
  get redisHost(): string {
    return this.config.get<string>('REDIS_HOST', 'localhost');
  }

  get redisPort(): number {
    return Number(this.config.get<string>('REDIS_PORT', '6379'));
  }

  get redisPassword(): string | undefined {
    const v = this.config.get<string>('REDIS_PASSWORD');
    return v && v.length > 0 ? v : undefined;
  }

  get otelServiceName(): string {
    return this.config.get<string>(
      'OTEL_SERVICE_NAME_GATEWAY',
      'realtime-gateway',
    );
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
