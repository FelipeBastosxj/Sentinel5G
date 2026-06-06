import { Body, Controller, HttpCode, HttpStatus, Post } from '@nestjs/common';
import { ProcessEventUseCase } from '../application/use-cases/process-event.use-case';

export interface ProcessRequest {
  workspaceId: string;
  provider: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
  receivedAt: string;
}

/**
 * WebhookReceiveController — internal HTTP endpoint consumed only by the
 * ingestion-service.
 *
 * POST /internal/process
 *   → validates workspace token, normalises payload, writes to PostgreSQL,
 *     and emits pg_notify for real-time fanout.
 */
@Controller('internal')
export class WebhookReceiveController {
  constructor(private readonly useCase: ProcessEventUseCase) {}

  @Post('process')
  @HttpCode(HttpStatus.ACCEPTED)
  async receive(@Body() dto: ProcessRequest): Promise<{ ok: boolean }> {
    await this.useCase.execute({
      workspaceId: dto.workspaceId,
      provider: dto.provider,
      headers: dto.headers,
      body: dto.body,
      receivedAt: new Date(dto.receivedAt),
    });
    return { ok: true };
  }
}
