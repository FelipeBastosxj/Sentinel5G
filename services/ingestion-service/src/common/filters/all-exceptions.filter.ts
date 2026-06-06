import {
  ArgumentsHost,
  Catch,
  ExceptionFilter,
  HttpException,
  HttpStatus,
} from '@nestjs/common';
import type { Response } from 'express';
import { AppLoggerService } from '../logger/logger.module';

/**
 * Global exception filter — translates every error into a structured JSON
 * response and a single log line. Sensitive details stay out of the body.
 */
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  constructor(private readonly logger: AppLoggerService) {}

  catch(exception: unknown, host: ArgumentsHost): void {
    const ctx = host.switchToHttp();
    const response = ctx.getResponse<Response>();

    if (exception instanceof HttpException) {
      const body = exception.getResponse();
      this.logger.warn('HTTP exception', { status: exception.getStatus() });
      response.status(exception.getStatus()).json(body);
      return;
    }

    this.logger.error('Unhandled exception', {
      error: (exception as Error)?.message ?? String(exception),
    });
    response.status(HttpStatus.INTERNAL_SERVER_ERROR).json({
      statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
      error: 'InternalServerError',
      message: 'An unexpected error occurred.',
    });
  }
}
