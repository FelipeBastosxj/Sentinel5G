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
 * Endpoint: POST /:workspaceId/:endpointToken
 *
 * Stays thin (HARDNESS §6): token validation, header capture, and body
 * capture are delegated to the use case.
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
    const headers = Object.fromEntries(
      Object.entries(req.headers).map(([k, v]) => [
        k.toLowerCase(),
        Array.isArray(v) ? v.join(', ') : (v ?? ''),
      ]),
    );

    await this.ingestEvent.execute({
      workspaceId,
      headers,
      body: (req.body as Record<string, unknown>) ?? {},
    });

    return { accepted: true };
  }
}
