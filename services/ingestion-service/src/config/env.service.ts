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

  // -------- HTTP --------------------------------------------------------
  get httpPort(): number {
    return Number(this.config.get<string>('INGESTION_PORT', '3001'));
  }

  // -------- Database ----------------------------------------------------
  get databaseUrl(): string {
    return this.config.get<string>(
      'DATABASE_URL',
      'postgresql://webhook_user:webhook_pass@localhost:5432/telecom_webhooks',
    );
  }

  // -------- Processing service (internal forwarding) --------------------
  get processingBaseUrl(): string {
    return this.config.get<string>('PROCESSING_BASE_URL', 'http://localhost:3003');
  }

  // -------- Redis -------------------------------------------------------
  get redisHost(): string {
    return this.config.get<string>('REDIS_HOST', 'localhost');
  }

  get redisPort(): number {
    return Number(this.config.get<string>('REDIS_PORT', '6379'));
  }

  // -------- Throttler ---------------------------------------------------
  get rateLimitTtlSeconds(): number {
    return Number(this.config.get<string>('RATE_LIMIT_TTL_SECONDS', '60'));
  }

  get rateLimitMax(): number {
    return Number(this.config.get<string>('RATE_LIMIT_MAX', '500'));
  }
}

