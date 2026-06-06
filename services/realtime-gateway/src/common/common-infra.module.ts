import { AsyncLocalStorage } from 'node:async_hooks';
import { Global, Inject, Injectable, Module } from '@nestjs/common';
import { createLogger, StructuredLogger } from '@eventstream/utils';
import { EnvService } from '../config/config.module';

@Injectable()
export class CorrelationService {
  private readonly storage = new AsyncLocalStorage<{ correlationId: string }>();

  run<T>(correlationId: string, callback: () => T): T {
    return this.storage.run({ correlationId }, callback);
  }

  getCorrelationId(): string | undefined {
    return this.storage.getStore()?.correlationId;
  }
}

export const APP_LOGGER = Symbol('APP_LOGGER');

@Injectable()
export class AppLoggerService {
  constructor(
    @Inject(APP_LOGGER) private readonly logger: StructuredLogger,
    private readonly correlation: CorrelationService,
  ) {}

  private with(): StructuredLogger {
    const id = this.correlation.getCorrelationId();
    return id ? this.logger.child({ correlationId: id }) : this.logger;
  }

  debug(m: string, f?: Record<string, unknown>): void { this.with().debug(m, f); }
  info(m: string, f?: Record<string, unknown>): void { this.with().info(m, f); }
  warn(m: string, f?: Record<string, unknown>): void { this.with().warn(m, f); }
  error(m: string, f?: Record<string, unknown>): void { this.with().error(m, f); }
}

@Global()
@Module({
  providers: [
    CorrelationService,
    {
      provide: APP_LOGGER,
      inject: [EnvService],
      useFactory: (env: EnvService): StructuredLogger =>
        createLogger({ service: 'realtime-gateway', level: env.logLevel }),
    },
    AppLoggerService,
  ],
  exports: [CorrelationService, AppLoggerService],
})
export class CommonInfraModule {}
