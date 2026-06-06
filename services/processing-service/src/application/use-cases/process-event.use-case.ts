import { Inject, Injectable, Logger } from '@nestjs/common';
import { Client } from 'pg';
import { WebhookEvent } from '@telecom-webhook/contracts';
import { generateUuid } from '@telecom-webhook/utils';
import { DATABASE_CLIENT } from '../../infrastructure/database/database.module';
import { TwilioNormalizer } from '../../infrastructure/normalizers/twilio.normalizer';

export interface ProcessCommand {
  workspaceId: string;
  provider: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
  receivedAt: Date;
}

/**
 * ProcessEventUseCase - normalises a raw webhook capture and persists it.
 *
 * Pipeline:
 *   1. Normalise raw payload to WebhookEvent via the appropriate normaliser.
 *   2. INSERT into webhook_events (PostgreSQL).
 *   3. Emit pg_notify for real-time fanout to the realtime-gateway.
 */
@Injectable()
export class ProcessEventUseCase {
  private readonly logger = new Logger(ProcessEventUseCase.name);

  constructor(
    @Inject(DATABASE_CLIENT) private readonly db: Client,
    private readonly twilio: TwilioNormalizer,
  ) {}

  async execute(cmd: ProcessCommand): Promise<void> {
    const startMs = Date.now();
    const id = generateUuid();
    const event: WebhookEvent = this.normalise(id, cmd);

    try {
      await this.db.query(
        `INSERT INTO webhook_events (
           id, workspace_id, provider, event_type,
           message_sid, call_sid, from_number, to_number, status,
           request_headers, request_payload, received_at, processed_at, processing_ms
         ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW(),$13)`,
        [
          event.id,
          event.workspaceId,
          event.provider,
          event.eventType,
          event.messageSid ?? null,
          event.callSid ?? null,
          event.from ?? null,
          event.to ?? null,
          event.status ?? null,
          JSON.stringify(event.headers),
          JSON.stringify(event.payload),
          event.receivedAt,
          Date.now() - startMs,
        ],
      );

      const notify = JSON.stringify({
        id: event.id,
        workspaceId: event.workspaceId,
        provider: event.provider,
        eventType: event.eventType,
        receivedAt: event.receivedAt,
        from: event.from,
        to: event.to,
        status: event.status,
      });
      await this.db.query(`SELECT pg_notify('webhook_events', $1)`, [notify]);

      this.logger.log(
        `Processed ${event.id} (${event.provider}/${event.eventType}) in ${Date.now() - startMs}ms`,
      );
    } catch (err) {
      this.logger.error('Failed to persist webhook event', (err as Error).message);
      throw err;
    }
  }

  private normalise(id: string, cmd: ProcessCommand): WebhookEvent {
    const capture = {
      workspaceId: cmd.workspaceId,
      headers: cmd.headers,
      body: cmd.body,
      receivedAt: cmd.receivedAt,
    };
    switch (cmd.provider) {
      case 'twilio':
        return this.twilio.normalize(id, capture);
      default:
        return {
          id,
          workspaceId: cmd.workspaceId,
          provider: cmd.provider as WebhookEvent['provider'],
          eventType: 'unknown',
          receivedAt: cmd.receivedAt,
          headers: cmd.headers,
          payload: cmd.body,
        };
    }
  }
}
