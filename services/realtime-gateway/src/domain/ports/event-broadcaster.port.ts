import { CanonicalEvent } from '@eventstream/contracts';

/**
 * EventStream — logical channel name advertised over the WebSocket.
 * Maps onto Kafka topics but uses domain-friendly identifiers.
 */
export type EventStream = 'events' | 'metrics';

/**
 * EventBroadcasterPort — domain port for delivering events to the connected
 * dashboard clients. Today, it is fulfilled by a Socket.IO gateway.
 */
export interface EventBroadcasterPort {
  broadcast(stream: EventStream, event: CanonicalEvent): void;
  connectedClientsCount(): number;
}

export const EVENT_BROADCASTER_PORT = Symbol('EVENT_BROADCASTER_PORT');
