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
 * database internals — it only depends on the outbound port.
 */
@Injectable()
export class IngestEventUseCase {
  constructor(
    @Inject(WEBHOOK_FORWARDER_PORT)
    private readonly forwarder: WebhookForwarderPort,
    private readonly logger: AppLoggerService,
  ) {}

  async execute(command: CaptureWebhookCommand): Promise<void> {
    const provider = this.detectProvider(command.headers, command.body);

    const bodyKeys = Object.keys(command.body ?? {}).length;
    if (bodyKeys === 0) {
      this.logger.warn('Webhook received with empty body', {
        workspaceId: command.workspaceId,
        method: command.headers['x-original-method'] ?? 'unknown',
        contentType: command.headers['content-type'] ?? 'none',
        provider,
      });
    }

    const capture: RawWebhookCapture = {
      workspaceId: command.workspaceId,
      provider,
      headers: command.headers,
      body: command.body,
      receivedAt: new Date(),
    };

    await this.forwarder.forward(capture);

    this.logger.info('Webhook captured and forwarded', {
      workspaceId: capture.workspaceId,
      provider: capture.provider,
      bodyKeys,
    });
  }

  /**
   * Detect provider from request headers, with a body-shape fallback.
   *
   * Header detection is preferred because it works even when the body is
   * empty (e.g. provider verification pings). Body fallback handles the
   * case where a reverse proxy or tunnel strips custom headers.
   */
  private detectProvider(
    headers: Record<string, string>,
    body: Record<string, unknown>,
  ): string {
    // ── Header-based detection (preferred) ───────────────────────────
    if (headers['x-twilio-signature']) return 'twilio';
    if (headers['x-vonage-signature']) return 'vonage';
    if (headers['messagebird-signature-jwt']) return 'messagebird';

    // ── Body-shape fallback ──────────────────────────────────────────
    if (body && typeof body === 'object') {
      // Twilio: MessageSid/CallSid/SmsSid present, or AccountSid starts with 'AC'
      if (
        typeof body['MessageSid'] === 'string' ||
        typeof body['CallSid'] === 'string' ||
        typeof body['SmsSid'] === 'string' ||
        typeof body['SmsMessageSid'] === 'string'
      ) {
        return 'twilio';
      }
      const accountSid = body['AccountSid'];
      if (typeof accountSid === 'string' && accountSid.startsWith('AC')) {
        return 'twilio';
      }

      // Vonage: messageId + msisdn
      if (
        typeof body['messageId'] === 'string' &&
        typeof body['msisdn'] === 'string'
      ) {
        return 'vonage';
      }
    }

    return 'unknown';
  }
}
