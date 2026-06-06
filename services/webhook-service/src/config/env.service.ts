import { Injectable } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

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
    return Number(this.config.get<string>('WEBHOOK_PORT', '3002'));
  }

  // -------- Provider secrets (HARDNESS §9 — never trust external input) ---
  get twilioSecret(): string | undefined {
    return this.config.get<string>('TWILIO_WEBHOOK_SECRET');
  }

  get infobipSecret(): string | undefined {
    return this.config.get<string>('INFOBIP_WEBHOOK_SECRET');
  }

  get sendgridSecret(): string | undefined {
    return this.config.get<string>('SENDGRID_WEBHOOK_SECRET');
  }

  // -------- Outbound (ingestion-service) ---------------------------------
  get ingestionUrl(): string {
    return this.config.get<string>(
      'INGESTION_BASE_URL',
      'http://localhost:3001',
    );
  }

  // -------- Observability ------------------------------------------------
  get otelServiceName(): string {
    return this.config.get<string>(
      'OTEL_SERVICE_NAME_WEBHOOK',
      'webhook-service',
    );
  }
}
