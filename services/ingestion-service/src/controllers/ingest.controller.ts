import {
  Body,
  Controller,
  HttpCode,
  HttpStatus,
  Post,
} from '@nestjs/common';
import { Throttle } from '@nestjs/throttler';
import { CanonicalEvent } from '@eventstream/contracts';
import { IngestEventUseCase } from '../application/use-cases/ingest-event.use-case';

interface IngestResponse {
  readonly accepted: true;
  readonly eventId: string;
  readonly correlationId: string;
}

/**
 * HTTP entry point for the ingestion pipeline.
 *
 * Stays intentionally thin (HARDNESS §6 — controllers must remain thin).
 * Validation, normalization, and Kafka publishing live in the use case.
 */
@Controller('ingest')
export class IngestController {
  constructor(private readonly ingestEvent: IngestEventUseCase) {}

  /**
   * Ingest a single CanonicalEvent.
   *
   * Rate-limited per HARDNESS §9. The actual TTL/limit values come from
   * `INGESTION_SERVICE_RATE_LIMIT_*` and are applied by the global Throttler.
   */
  @Post()
  @HttpCode(HttpStatus.ACCEPTED)
  @Throttle({ default: { ttl: 60_000, limit: 1000 } })
  async ingest(@Body() body: unknown): Promise<IngestResponse> {
    const event = await this.ingestEvent.execute({ payload: body });
    return {
      accepted: true,
      eventId: event.eventId,
      correlationId: event.correlationId,
    };
  }
}
