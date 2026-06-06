import { CanonicalEvent } from '@eventstream/contracts';

/**
 * EventStorePort — outbound port for persistent event storage.
 *
 * The processing-service writes every successfully processed event here
 * (HARDNESS §3 — persistent state belongs to ClickHouse/Kafka/Redis).
 * The HTTP query controller reads from here to answer historical queries.
 */
export interface FindRecentOptions {
  readonly limit?: number;
  readonly channel?: string;
  readonly source?: string;
}

export interface EventStorePort {
  save(event: CanonicalEvent): Promise<void>;
  findRecent(options?: FindRecentOptions): Promise<CanonicalEvent[]>;
}

export const EVENT_STORE_PORT = Symbol('EVENT_STORE_PORT');
