import {
  ArgumentsHost,
  Catch,
  ExceptionFilter,
  HttpException,
  HttpStatus,
} from '@nestjs/common';
import type { Response } from 'express';
import { SchemaValidationError } from '@eventstream/schemas';
import { AppLoggerService } from '../logger/logger.module';

/**
 * Global exception filter that translates every error type into a
 * structured JSON response and a single log line.
 *
 * - {@link SchemaValidationError} → 400 with the offending field paths.
 * - {@link HttpException}         → preserves status and body.
 * - everything else                → 500 with a generic error code.
 *
 * Sensitive details are kept out of the response body (HARDNESS §9).
 */
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  constructor(private readonly logger: AppLoggerService) {}

  catch(exception: unknown, host: ArgumentsHost): void {
    const ctx = host.switchToHttp();
    const response = ctx.getResponse<Response>();

    if (exception instanceof SchemaValidationError) {
      this.logger.warn('Schema validation failed', { issues: exception.issues });
      response.status(HttpStatus.BAD_REQUEST).json({
        statusCode: HttpStatus.BAD_REQUEST,
        error: 'SchemaValidationError',
        message: 'Payload does not satisfy CanonicalEvent schema.',
        issues: exception.issues,
      });
      return;
    }

    if (exception instanceof HttpException) {
      const status = exception.getStatus();
      const body = exception.getResponse();
      this.logger.warn('HTTP exception', { status, body });
      response.status(status).json(typeof body === 'string' ? { message: body } : body);
      return;
    }

    const err = exception as Error;
    this.logger.error('Unhandled exception', {
      name: err?.name,
      message: err?.message,
      stack: err?.stack,
    });
    response.status(HttpStatus.INTERNAL_SERVER_ERROR).json({
      statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
      error: 'InternalServerError',
      message: 'An unexpected error occurred.',
    });
  }
}
