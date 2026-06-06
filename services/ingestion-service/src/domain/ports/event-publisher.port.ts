/**
 * WebhookForwarderPort — outbound port (hexagonal architecture).
 *
 * The ingestion-service accepts and captures raw webhooks, then delegates
 * normalisation to the processing-service via this port.
 * Domain stays framework and transport agnostic (HARDNESS §6).
 */
export interface WebhookForwarderPort {
  forward(capture: RawWebhookCapture): Promise<void>;
}

export interface RawWebhookCapture {
  readonly workspaceId: string;
  readonly provider: string;
  readonly headers: Record<string, string>;
  readonly body: Record<string, unknown>;
  readonly receivedAt: Date;
}

export const WEBHOOK_FORWARDER_PORT = Symbol('WEBHOOK_FORWARDER_PORT');
