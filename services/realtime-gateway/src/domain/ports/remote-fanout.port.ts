import { CanonicalEvent } from '@eventstream/contracts';
import { EventStream } from './event-broadcaster.port';

/**
 * RemoteFanoutPort — outbound port for cross-instance fan-out.
 *
 * Implemented today by the Redis pub/sub adapter. Allows multiple gateway
 * instances behind a load balancer to share a single broadcast space
 * (HARDNESS §10 — services must support horizontal scaling).
 */
export interface RemoteFanoutPort {
  publish(stream: EventStream, event: CanonicalEvent): Promise<void>;
  subscribe(handler: RemoteFanoutHandler): Promise<void>;
}

export type RemoteFanoutHandler = (
  stream: EventStream,
  event: CanonicalEvent,
) => void;

export const REMOTE_FANOUT_PORT = Symbol('REMOTE_FANOUT_PORT');
