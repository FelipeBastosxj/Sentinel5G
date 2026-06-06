import { Controller, Get, Inject, Param, Query } from '@nestjs/common';
import { Client } from 'pg';
import { WebhookEvent } from '@telecom-webhook/contracts';
import { DATABASE_CLIENT } from '../infrastructure/database/database.module';

/**
 * EventsQueryController — REST endpoints for historical event queries.
 *
 * Backed by PostgreSQL webhook_events table.
 * Used by the Angular dashboard on startup to hydrate the event store with
 * recent events that arrived while the browser was closed.
 *
 * GET /events/recent?limit=100
 * GET /events/workspace/:workspaceId?limit=50
 */
@Controller('events')
export class EventsQueryController {
  constructor(@Inject(DATABASE_CLIENT) private readonly db: Client) {}

  @Get('recent')
  async findRecent(
    @Query('limit') limit?: string,
  ): Promise<WebhookEvent[]> {
    const n = Math.min(Number(limit ?? 100), 500);
    const { rows } = await this.db.query<WebhookEvent>(
      `SELECT id, workspace_id AS "workspaceId", provider, event_type AS "eventType",
              received_at AS "receivedAt", request_headers AS headers,
              request_payload AS payload, message_sid AS "messageSid",
              call_sid AS "callSid", from_number AS "from", to_number AS "to",
              status, processed_at AS "processedAt", processing_ms AS "processingMs"
       FROM webhook_events
       ORDER BY received_at DESC
       LIMIT $1`,
      [n],
    );
    return rows;
  }

  @Get('workspace/:workspaceId')
  async findByWorkspace(
    @Param('workspaceId') workspaceId: string,
    @Query('limit') limit?: string,
  ): Promise<WebhookEvent[]> {
    const n = Math.min(Number(limit ?? 100), 500);
    const { rows } = await this.db.query<WebhookEvent>(
      `SELECT id, workspace_id AS "workspaceId", provider, event_type AS "eventType",
              received_at AS "receivedAt", request_headers AS headers,
              request_payload AS payload, message_sid AS "messageSid",
              call_sid AS "callSid", from_number AS "from", to_number AS "to",
              status, processed_at AS "processedAt", processing_ms AS "processingMs"
       FROM webhook_events
       WHERE workspace_id = $1
       ORDER BY received_at DESC
       LIMIT $2`,
      [workspaceId, n],
    );
    return rows;
  }
}

