import {
  All,
  Controller,
  HttpCode,
  HttpStatus,
  Param,
  Req,
} from '@nestjs/common';
import { Request } from 'express';
import { Throttle } from '@nestjs/throttler';
import { IngestEventUseCase } from '../application/use-cases/ingest-event.use-case';

/**
 * HTTP entry point for all inbound webhook providers.
 *
 * Endpoint: ANY /:workspaceId/:endpointToken
 *
 * Captures the HTTP method and original URL as synthetic `x-original-*`
 * headers so that downstream debugging shows the exact request shape,
 * even when the body is empty or the content-type is unexpected.
 */
@Controller(':workspaceId/:endpointToken')
export class IngestController {
  constructor(private readonly ingestEvent: IngestEventUseCase) {}

  @All()
  @HttpCode(HttpStatus.ACCEPTED)
  @Throttle({ default: { ttl: 60_000, limit: 500 } })
  async receive(
    @Param('workspaceId') workspaceId: string,
    @Req() req: Request,
  ): Promise<{ accepted: true }> {
    const headers: Record<string, string> = Object.fromEntries(
      Object.entries(req.headers).map(([k, v]) => [
        k.toLowerCase(),
        Array.isArray(v) ? v.join(', ') : (v ?? ''),
      ]),
    );

    // Synthetic headers — make HTTP method + URL visible to normalisers
    // and to the dashboard, since req.body alone can be ambiguous.
    headers['x-original-method'] = req.method;
    headers['x-original-url'] = req.originalUrl;
    headers['x-client-ip'] = req.ip ?? '';

    await this.ingestEvent.execute({
      workspaceId,
      headers,
      body: (req.body as Record<string, unknown>) ?? {},
    });

    return { accepted: true };
  }
}
