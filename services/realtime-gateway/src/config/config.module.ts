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
    return Number(this.config.get<string>('GATEWAY_PORT', '3004'));
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

  get databaseUrl(): string {
    return this.config.get<string>(
      'DATABASE_URL',
      'postgresql://postgres:postgres@localhost:5432/telecom_webhook',
    );
  }

  get redisHost(): string {
    return this.config.get<string>('REDIS_HOST', 'localhost');
  }

  get redisPort(): number {
    return Number(this.config.get<string>('REDIS_PORT', '6379'));
  }

  get redisPassword(): string | undefined {
    return this.config.get<string>('REDIS_PASSWORD') || undefined;
  }
}

@Global()
@Module({
  imports: [
    ConfigModule.forRoot({ isGlobal: true, envFilePath: ['.env', '../../.env'] }),
  ],
  providers: [EnvService],
  exports: [EnvService],
})
export class AppConfigModule {}
