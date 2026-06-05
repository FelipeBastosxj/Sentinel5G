/**
 * EventType — Enumeration of all canonical event types supported by the platform.
 *
 * New event types must be added here to be recognized by the processing pipeline.
 * String values are used in Kafka messages and ClickHouse storage.
 */
export enum EventType {
  DELIVERY_EVENT = 'DELIVERY_EVENT',
  STATUS_EVENT = 'STATUS_EVENT',
  ERROR_EVENT = 'ERROR_EVENT',
  METRIC_EVENT = 'METRIC_EVENT',
  ALERT_EVENT = 'ALERT_EVENT',
}
