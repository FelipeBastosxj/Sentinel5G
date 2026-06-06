import {
  Injectable,
  Logger,
  OnApplicationShutdown,
  OnModuleInit,
} from '@nestjs/common';
import { Client } from 'pg';
import { EnvService } from '../../config/config.module';

export type PgNotifyHandler = (payload: string) => void;

/**
 * PgListenerAdapter — maintains a dedicated pg.Client in LISTEN mode.
 *
 * Fires registered handlers whenever a NOTIFY arrives on the
 * 'webhook_events' channel (sent by processing-service after INSERT).
 * A dedicated client is required because a LISTEN connection cannot
 * issue normal queries concurrently.
 */
@Injectable()
export class PgListenerAdapter implements OnModuleInit, OnApplicationShutdown {
  private readonly logger = new Logger(PgListenerAdapter.name);
  private readonly client: Client;
  private readonly handlers: PgNotifyHandler[] = [];

  constructor(private readonly env: EnvService) {
    this.client = new Client({ connectionString: this.env.databaseUrl });
  }

  registerHandler(handler: PgNotifyHandler): void {
    this.handlers.push(handler);
  }

  async onModuleInit(): Promise<void> {
    await this.client.connect();
    await this.client.query('LISTEN webhook_events');

    this.client.on('notification', (msg) => {
      if (msg.channel === 'webhook_events' && msg.payload) {
        for (const h of this.handlers) {
          try { h(msg.payload); } catch (err) {
            this.logger.warn('pg notify handler threw', (err as Error).message);
          }
        }
      }
    });

    this.client.on('error', (err) =>
      this.logger.error('pg LISTEN client error', err.message),
    );

    this.logger.log('Listening on PostgreSQL channel: webhook_events');
  }

  async onApplicationShutdown(): Promise<void> {
    await this.client.end().catch(() => undefined);
  }
}
