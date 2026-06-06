import { CanonicalEvent } from '@eventstream/contracts';

/**
 * EventEnricherPort — domain port that augments a CanonicalEvent with derived
 * data (e.g., normalized phone numbers, geolocation, computed latencies).
 *
 * Enrichers must be pure: produce a new event and never mutate the input.
 */
export interface EventEnricherPort {
  enrich(event: CanonicalEvent): CanonicalEvent;
}

export const EVENT_ENRICHER_PORT = Symbol('EVENT_ENRICHER_PORT');
