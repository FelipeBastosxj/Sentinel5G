import { Global, Inject, Injectable, Module } from '@nestjs/common';
import { createLogger, StructuredLogger } from '@eventstream/utils';
import { EnvService } from '../../config/env.service';
import { CorrelationService } from '../correlation/correlation.module';

export const APP_LOGGER = Symbol('APP_LOGGER');

@Injectable()
export class AppLoggerService {
  constructor(
    @Inject(APP_LOGGER) private readonly logger: StructuredLogger,
    private readonly correlation: CorrelationService,
  ) {}

  private withCorrelation(): StructuredLogger {
    const id = this.correlation.getCorrelationId();
    return id ? this.logger.child({ correlationId: id }) : this.logger;
  }

  debug(m: string, f?: Record<string, unknown>): void {
    this.withCorrelation().debug(m, f);
  }
  info(m: string, f?: Record<string, unknown>): void {
    this.withCorrelation().info(m, f);
  }
  warn(m: string, f?: Record<string, unknown>): void {
    this.withCorrelation().warn(m, f);
  }
  error(m: string, f?: Record<string, unknown>): void {
    this.withCorrelation().error(m, f);
  }
}

@Global()
@Module({
  providers: [
    {
      provide: APP_LOGGER,
      inject: [EnvService],
      useFactory: (env: EnvService): StructuredLogger =>
        createLogger({ service: 'webhook-service', level: env.logLevel }),
    },
    AppLoggerService,
  ],
  exports: [AppLoggerService],
})
export class LoggerModule {}
