import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import { EnvService } from '../../config/config.module';
import {
  EventStorePort,
  FindRecentOptions,
} from '../../domain/ports/event-store.port';

/** Row shape matched to the ClickHouse `events` table columns. */
interface ChRow {
  event_id: string;
  event_type: string;
  channel: string;
  source: string;
  correlation_id: string;
  timestamp: string;
  version: string;
  metadata: string;
  payload: string;
}

/** Convert ISO-8601 → ClickHouse DateTime64 literal: '2026-06-06 18:00:00.000' */
function toCh(iso: string): string {
  return new Date(iso).toISOString().replace('T', ' ').replace('Z', '');
}

/** Convert ClickHouse row back to CanonicalEvent */
function fromRow(row: ChRow): CanonicalEvent {
  return {
    eventId:       row.event_id,
    eventType:     row.event_type as CanonicalEvent['eventType'],
    channel:       row.channel    as CanonicalEvent['channel'],
    source:        row.source,
    correlationId: row.correlation_id,
    timestamp:     new Date(row.timestamp.replace(' ', 'T') + 'Z').toISOString(),
    version:       row.version,
    metadata:      JSON.parse(row.metadata) as Record<string, unknown>,
    payload:       JSON.parse(row.payload),
  } as unknown as CanonicalEvent;
}

@Injectable()
export class ClickHouseAdapter implements EventStorePort, OnModuleInit {
  private readonly logger = new Logger(ClickHouseAdapter.name);
  private readonly base: string;
  private readonly headers: Record<string, string>;

  constructor(private readonly env: EnvService) {
    this.base = `http://${env.clickhouseHost}:${env.clickhousePort}`;
    this.headers = {
      'X-ClickHouse-User':     env.clickhouseUser,
      'X-ClickHouse-Password': env.clickhousePassword,
      'X-ClickHouse-Database': env.clickhouseDatabase,
    };
  }

  // ─── Lifecycle ──────────────────────────────────────────────────────────────

  async onModuleInit(): Promise<void> {
    try {
      await this.exec(`
        CREATE TABLE IF NOT EXISTS events (
          event_id        String,
          event_type      LowCardinality(String),
          channel         LowCardinality(String),
          source          LowCardinality(String),
          correlation_id  String,
          timestamp       DateTime64(3, 'UTC'),
          version         String           DEFAULT '1.0',
          metadata        String           DEFAULT '{}',
          payload         String           DEFAULT '{}',
          processed_at    DateTime64(3, 'UTC') DEFAULT now64(3)
        )
        ENGINE = MergeTree()
        PARTITION BY toYYYYMM(timestamp)
        ORDER BY (toDate(timestamp), event_id)
        TTL toDateTime(timestamp) + INTERVAL 90 DAY
        SETTINGS index_granularity = 8192
      `);
      this.logger.log('ClickHouse table ready');
    } catch (err) {
      // Non-fatal — service still works without ClickHouse
      this.logger.warn(`ClickHouse init failed (non-fatal): ${(err as Error).message}`);
    }
  }

  // ─── EventStorePort ─────────────────────────────────────────────────────────

  async save(event: CanonicalEvent): Promise<void> {
    const row: ChRow = {
      event_id:       event.eventId,
      event_type:     String(event.eventType),
      channel:        String(event.channel),
      source:         String(event.source),
      correlation_id: event.correlationId,
      timestamp:      toCh(event.timestamp),
      version:        event.version ?? '1.0',
      metadata:       JSON.stringify(event.metadata ?? {}),
      payload:        JSON.stringify(event.payload ?? {}),
    };

    try {
      await this.exec(
        `INSERT INTO events FORMAT JSONEachRow`,
        JSON.stringify(row),
      );
    } catch (err) {
      // Non-fatal — Kafka pipeline must not be blocked by storage failures
      this.logger.warn(`ClickHouse save failed (non-fatal): ${(err as Error).message}`);
    }
  }

  async findRecent(options: FindRecentOptions = {}): Promise<CanonicalEvent[]> {
    const limit   = Math.min(options.limit ?? 100, 500);
    const filters: string[] = [];

    if (options.channel) filters.push(`channel = '${esc(options.channel)}'`);
    if (options.source)  filters.push(`source  = '${esc(options.source)}'`);

    const where = filters.length ? `WHERE ${filters.join(' AND ')}` : '';
    const sql   = `SELECT * FROM events ${where} ORDER BY timestamp DESC LIMIT ${limit} FORMAT JSON`;

    try {
      const res = await fetch(`${this.base}/?query=${encodeURIComponent(sql)}`, {
        headers: this.headers,
      });
      if (!res.ok) throw new Error(`CH query error ${res.status}: ${await res.text()}`);
      const body = await res.json() as { data: ChRow[] };
      return (body.data ?? []).map(fromRow);
    } catch (err) {
      this.logger.warn(`ClickHouse query failed: ${(err as Error).message}`);
      return [];
    }
  }

  // ─── Helpers ────────────────────────────────────────────────────────────────

  /** Execute a DDL or INSERT query via the ClickHouse HTTP interface.
   *  Always uses POST — ClickHouse HTTP requires POST for all write operations
   *  (DDL, INSERT). GET is read-only mode only.
   */
  private async exec(sql: string, body?: string): Promise<void> {
    const url = `${this.base}/?query=${encodeURIComponent(sql.trim())}`;
    const res = await fetch(url, {
      method:  'POST',
      headers: { ...this.headers, 'Content-Type': 'text/plain' },
      body,
    });
    if (!res.ok) {
      throw new Error(`CH exec error ${res.status}: ${await res.text()}`);
    }
  }
}

/** Escape single quotes to prevent SQL injection in literal values. */
function esc(value: string): string {
  return value.replace(/'/g, "\\'");
}
