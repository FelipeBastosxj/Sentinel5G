import { Injectable, NestMiddleware } from '@nestjs/common';
import type { Request, Response, NextFunction } from 'express';
import {
  CORRELATION_ID_HEADER,
  extractOrCreateCorrelationId,
} from '@eventstream/utils';
import { CorrelationService } from './correlation.module';

@Injectable()
export class CorrelationMiddleware implements NestMiddleware {
  constructor(private readonly correlation: CorrelationService) {}

  use(req: Request, res: Response, next: NextFunction): void {
    const correlationId = extractOrCreateCorrelationId(
      req.headers as Record<string, string | string[] | undefined>,
    );
    res.setHeader(CORRELATION_ID_HEADER, correlationId);
    this.correlation.run(correlationId, () => next());
  }
}
