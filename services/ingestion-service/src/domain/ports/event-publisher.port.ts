import { CanonicalEvent } from '@eventstream/contracts';

/**
 * EventPublisherPort — outbound port (hexagonal architecture).
 *
 * Application use cases depend on this interface, never on a concrete
 * messaging library. Infrastructure provides the implementation
 * (today: KafkaProducerAdapter) — HARDNESS §6.
 */
export interface EventPublisherPort {
  publish(topic: string, event: CanonicalEvent): Promise<void>;
}

export const EVENT_PUBLISHER_PORT = Symbol('EVENT_PUBLISHER_PORT');
