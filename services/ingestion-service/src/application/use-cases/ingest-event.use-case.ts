import { Inject, Injectable } from '@nestjs/common';
import { AppLoggerService } from '../../common/logger/logger.module';
import {
  WEBHOOK_FORWARDER_PORT,
  WebhookForwarderPort,
  RawWebhookCapture,
} from '../../domain/ports/event-publisher.port';

export interface CaptureWebhookCommand {
  readonly workspaceId: string;
  readonly headers: Record<string, string>;
  readonly body: Record<string, unknown>;
}

/**
 * Use case: capture a raw inbound webhook and forward it to the
 * processing-service for normalisation and persistence.
 *
 * This layer has no awareness of HTTP details, provider formats, or
 * database internals — it only depends on the outbound port (HARDNESS §6).
 */
@Injectable()
export class IngestEventUseCase {
  constructor(
    @Inject(WEBHOOK_FORWARDER_PORT)
    private readonly forwarder: WebhookForwarderPort,
    private readonly logger: AppLoggerService,
  ) {}

  async execute(command: CaptureWebhookCommand): Promise<void> {
    const capture: RawWebhookCapture = {
      workspaceId: command.workspaceId,
      provider: this.detectProvider(command.headers),
      headers: command.headers,
      body: command.body,
      receivedAt: new Date(),
    };

    await this.forwarder.forward(capture);

    this.logger.info('Webhook captured and forwarded', {
      workspaceId: capture.workspaceId,
      provider: capture.provider,
    });
  }

  /** Detect provider from request headers (e.g. Twilio sends X-Twilio-Signature). */
  private detectProvider(headers: Record<string, string>): string {
    if (headers['x-twilio-signature']) return 'twilio';
    if (headers['x-vonage-signature']) return 'vonage';
    if (headers['messagebird-signature-jwt']) return 'messagebird';
    return 'unknown';
  }
}
