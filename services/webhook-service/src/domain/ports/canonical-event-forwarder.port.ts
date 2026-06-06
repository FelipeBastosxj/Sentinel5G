import { CanonicalEvent } from '@eventstream/contracts';

/**
 * CanonicalEventForwarderPort — outbound port (hexagonal architecture).
 *
 * The webhook-service does NOT publish to Kafka itself. Per the system
 * architecture, normalized events are forwarded to the ingestion-service
 * which is the single owner of `events.raw` (HARDNESS §5).
 */
export interface CanonicalEventForwarderPort {
  forward(event: CanonicalEvent): Promise<void>;
}

export const CANONICAL_EVENT_FORWARDER_PORT = Symbol(
  'CANONICAL_EVENT_FORWARDER_PORT',
);
