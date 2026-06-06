import { CanonicalEvent } from '@eventstream/contracts';

export interface EventPublisherPort {
  publish(topic: string, event: CanonicalEvent): Promise<void>;
}

export const EVENT_PUBLISHER_PORT = Symbol('EVENT_PUBLISHER_PORT');
