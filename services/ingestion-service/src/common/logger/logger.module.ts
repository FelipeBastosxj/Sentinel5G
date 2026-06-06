import { Global, Inject, Injectable, Module } from '@nestjs/common';
import { createLogger, StructuredLogger } from '@telecom-webhook/utils';
import { EnvService } from '../../config/env.service';
import { CorrelationService } from '../correlation/correlation.service';

export const APP_LOGGER = Symbol('APP_LOGGER');

/**
 * NestJS-aware wrapper around the {@link StructuredLogger} from `@telecom-webhook/utils`.
 *
 * Automatically merges the active correlation ID into every log record so the
 * caller never has to remember to pass it.
 */
@Injectable()
export class AppLoggerService {
  constructor(
    @Inject(APP_LOGGER) private readonly logger: StructuredLogger,
    private readonly correlation: CorrelationService,
  ) {}

  private withCorrelation(): StructuredLogger {
    const correlationId = this.correlation.getCorrelationId();
    return correlationId ? this.logger.child({ correlationId }) : this.logger;
  }

  debug(message: string, fields?: Record<string, unknown>): void {
    this.withCorrelation().debug(message, fields);
  }

  info(message: string, fields?: Record<string, unknown>): void {
    this.withCorrelation().info(message, fields);
  }

  warn(message: string, fields?: Record<string, unknown>): void {
    this.withCorrelation().warn(message, fields);
  }

  error(message: string, fields?: Record<string, unknown>): void {
    this.withCorrelation().error(message, fields);
  }
}

@Global()
@Module({
  providers: [
    {
      provide: APP_LOGGER,
      inject: [EnvService],
      useFactory: (env: EnvService): StructuredLogger =>
        createLogger({ service: 'ingestion-service', level: env.logLevel }),
    },
    AppLoggerService,
  ],
  exports: [AppLoggerService],
})
export class LoggerModule {}
