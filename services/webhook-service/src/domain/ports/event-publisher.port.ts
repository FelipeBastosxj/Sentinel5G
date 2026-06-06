import { CanonicalEvent } from '@eventstream/contracts';

export interface EventPublisherPort {
  publish(event: CanonicalEvent): Promise<void>;
}
