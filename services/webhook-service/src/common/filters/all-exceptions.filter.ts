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
import { InvalidSignatureError } from '../../domain/errors';

@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  constructor(private readonly logger: AppLoggerService) {}

  catch(exception: unknown, host: ArgumentsHost): void {
    const response = host.switchToHttp().getResponse<Response>();

    if (exception instanceof InvalidSignatureError) {
      this.logger.warn('Invalid webhook signature', { provider: exception.provider });
      response.status(HttpStatus.UNAUTHORIZED).json({
        statusCode: HttpStatus.UNAUTHORIZED,
        error: 'InvalidSignature',
        message: 'Webhook signature validation failed.',
      });
      return;
    }

    if (exception instanceof SchemaValidationError) {
      this.logger.warn('Provider payload validation failed', {
        issues: exception.issues,
      });
      response.status(HttpStatus.BAD_REQUEST).json({
        statusCode: HttpStatus.BAD_REQUEST,
        error: 'SchemaValidationError',
        message: 'Provider payload does not satisfy expected schema.',
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
