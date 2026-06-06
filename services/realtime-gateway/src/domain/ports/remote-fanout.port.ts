import { WebhookEventSummary } from './event-broadcaster.port';

/**
 * RemoteFanoutPort — outbound port for cross-instance fan-out over Redis.
 * Allows multiple gateway instances behind a load balancer to share a
 * single broadcast space.
 */
export interface RemoteFanoutPort {
  publish(workspaceId: string, event: WebhookEventSummary): Promise<void>;
  subscribe(handler: RemoteFanoutHandler): Promise<void>;
}

export type RemoteFanoutHandler = (
  workspaceId: string,
  event: WebhookEventSummary,
) => void;

export const REMOTE_FANOUT_PORT = Symbol('REMOTE_FANOUT_PORT');
