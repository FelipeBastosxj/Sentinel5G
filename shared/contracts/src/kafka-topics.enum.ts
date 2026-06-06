/**
 * KafkaTopic — Centralized list of every Kafka topic used by the platform.
 *
 * Topics are defined by the MASTER_PROMPT (Kafka Topics section).
 * Services must import from this enum instead of hard-coding strings, so a
 * topic rename is a one-line refactor.
 */
export enum KafkaTopic {
  /** Raw normalized events freshly produced by the ingestion-service. */
  EVENTS_RAW = 'events.raw',

  /** Enriched events emitted by the processing-service. */
  EVENTS_PROCESSED = 'events.processed',

  /** Aggregated metrics emitted by the processing-service. */
  EVENTS_METRICS = 'events.metrics',

  /** Dead-letter queue for events that exhausted retries. */
  EVENTS_ALERTS = 'events.alerts',
}

/**
 * Kafka consumer-group identifiers per service.
 * Used to give each service its own offset on shared topics.
 */
export enum KafkaConsumerGroup {
  PROCESSING = 'eventstream.processing-service',
  REALTIME_GATEWAY = 'eventstream.realtime-gateway',
  CLICKHOUSE_WRITER = 'eventstream.clickhouse-writer',
}
