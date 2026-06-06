import { Injectable, NestMiddleware } from '@nestjs/common';
import { Request, Response, NextFunction } from 'express';
import {
  CORRELATION_ID_HEADER,
  extractOrCreateCorrelationId,
} from '@eventstream/utils';
import { CorrelationService } from './correlation.service';

/**
 * Reads or generates the correlation ID for every incoming HTTP request,
 * stores it in AsyncLocalStorage, and echoes it back on the response.
 */
@Injectable()
export class CorrelationMiddleware implements NestMiddleware {
  constructor(private readonly correlation: CorrelationService) {}

  use(req: Request, res: Response, next: NextFunction): void {
    const correlationId = extractOrCreateCorrelationId(req.headers as Record<string, string | string[] | undefined>);
    res.setHeader(CORRELATION_ID_HEADER, correlationId);
    this.correlation.run(correlationId, () => next());
  }
}
