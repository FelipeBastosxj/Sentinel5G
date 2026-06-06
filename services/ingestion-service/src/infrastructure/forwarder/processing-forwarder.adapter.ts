import { Injectable, Logger, OnModuleInit } from '@nestjs/common';
import { EnvService } from '../../config/env.service';
import {
  WebhookForwarderPort,
  RawWebhookCapture,
} from '../../domain/ports/event-publisher.port';

/**
 * ProcessingForwarderAdapter — infrastructure implementation of WebhookForwarderPort.
 *
 * Forwards raw webhook captures to the processing-service over HTTP using
 * Node 22 native fetch. No extra dependencies required (HARDNESS §6).
 */
@Injectable()
export class ProcessingForwarderAdapter
  implements WebhookForwarderPort, OnModuleInit
{
  private readonly logger = new Logger(ProcessingForwarderAdapter.name);
  private processingUrl: string;

  constructor(private readonly env: EnvService) {}

  onModuleInit(): void {
    this.processingUrl = `${this.env.processingBaseUrl}/internal/process`;
    this.logger.log(`Processing forwarder → ${this.processingUrl}`);
  }

  async forward(capture: RawWebhookCapture): Promise<void> {
    const res = await fetch(this.processingUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        workspaceId: capture.workspaceId,
        provider: capture.provider,
        headers: capture.headers,
        body: capture.body,
        receivedAt: capture.receivedAt.toISOString(),
      }),
    });

    if (!res.ok) {
      const text = await res.text().catch(() => '');
      this.logger.error(
        `Processing-service returned ${res.status}: ${text}`,
        { workspaceId: capture.workspaceId },
      );
      // Non-fatal in MVP — event is captured but processing may be delayed.
    }
  }
}
