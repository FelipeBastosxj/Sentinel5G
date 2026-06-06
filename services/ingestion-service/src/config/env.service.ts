import { Injectable } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

/**
 * Typed accessor over `@nestjs/config`.
 *
 * Centralizing env reads here lets every other module receive a strongly-typed
 * dependency instead of grabbing raw strings from `process.env` (HARDNESS §6).
 */
@Injectable()
export class EnvService {
  constructor(private readonly config: ConfigService) {}

  // -------- General -----------------------------------------------------
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

  get correlationIdHeader(): string {
    return this.config.get<string>('CORRELATION_ID_HEADER', 'x-correlation-id');
  }

  // -------- HTTP --------------------------------------------------------
  get httpPort(): number {
    return Number(this.config.get<string>('INGESTION_SERVICE_PORT', '3001'));
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
    return this.config.get<string>('KAFKA_CLIENT_ID', 'ingestion-service');
  }

  get kafkaTopicEventsRaw(): string {
    return this.config.get<string>('KAFKA_TOPIC_EVENTS_RAW', 'events.raw');
  }

  // -------- Throttler ---------------------------------------------------
  get rateLimitTtlSeconds(): number {
    return Number(this.config.get<string>('INGESTION_SERVICE_RATE_LIMIT_TTL', '60'));
  }

  get rateLimitMax(): number {
    return Number(this.config.get<string>('INGESTION_SERVICE_RATE_LIMIT_MAX', '1000'));
  }

  // -------- Observability ----------------------------------------------
  get otelServiceName(): string {
    return this.config.get<string>(
      'OTEL_SERVICE_NAME_INGESTION',
      'ingestion-service',
    );
  }
}
