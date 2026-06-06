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

  // -------- Database ----------------------------------------------------
  get databaseUrl(): string {
    return this.config.get<string>(
      'DATABASE_URL',
      'postgresql://webhook_user:webhook_pass@localhost:5432/telecom_webhooks',
    );
  }

  // -------- Twilio signature validation ---------------------------------
  get twilioAuthToken(): string {
    return this.config.get<string>('TWILIO_AUTH_TOKEN', '');
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
