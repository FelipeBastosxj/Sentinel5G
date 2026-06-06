import { Controller, Get, Inject, Query } from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import {
  EVENT_STORE_PORT,
  EventStorePort,
} from '../domain/ports/event-store.port';

/**
 * EventsQueryController — REST endpoint for historical event queries.
 *
 * Backed by ClickHouse (HARDNESS §3 — analytical queries go to ClickHouse).
 * Used by the Angular dashboard on startup to hydrate the event and metrics
 * stores with recent events that arrived while the browser was closed.
 *
 * GET /events/recent?limit=100&channel=SMS&source=twilio
 */
@Controller('events')
export class EventsQueryController {
  constructor(
    @Inject(EVENT_STORE_PORT) private readonly store: EventStorePort,
  ) {}

  @Get('recent')
  findRecent(
    @Query('limit')   limit?:   string,
    @Query('channel') channel?: string,
    @Query('source')  source?:  string,
  ): Promise<CanonicalEvent[]> {
    return this.store.findRecent({
      limit:   limit ? Math.min(Number(limit), 500) : 100,
      channel: channel || undefined,
      source:  source  || undefined,
    });
  }
}
