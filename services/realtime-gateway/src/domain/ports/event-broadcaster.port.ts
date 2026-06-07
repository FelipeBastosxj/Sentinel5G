/**
 * EventBroadcasterPort — domain port for delivering webhook events to
 * connected dashboard clients. Fulfilled by the Socket.IO gateway.
 */
export interface EventBroadcasterPort {
  broadcast(workspaceId: string, event: WebhookEventSummary): void;
  connectedClientsCount(): number;
}

/** Minimal event payload pushed over WebSocket. */
export interface WebhookEventSummary {
  id: string;
  workspaceId: string;
  provider: string;
  eventType: string;
  channel?: 'sms' | 'whatsapp' | 'voice' | 'other';
  receivedAt: Date | string;
  messageSid?: string;
  callSid?: string;
  from?: string;
  to?: string;
  status?: string;
}

export const EVENT_BROADCASTER_PORT = Symbol('EVENT_BROADCASTER_PORT');
