import { createHmac, timingSafeEqual } from 'node:crypto';
import { Injectable } from '@nestjs/common';
import { EventSource } from '@eventstream/contracts';
import { EnvService } from '../../config/env.service';
import { SignatureValidatorPort } from '../../domain/ports/signature-validator.port';

/**
 * SendGrid event-webhook signature validation (HMAC-SHA256 of timestamp+body
 * signed with the verification key, base64 encoded).
 *
 * Reference: https://docs.sendgrid.com/for-developers/tracking-events/getting-started-event-webhook-security-features
 *
 * Headers:
 *   - X-Twilio-Email-Event-Webhook-Signature
 *   - X-Twilio-Email-Event-Webhook-Timestamp
 */
@Injectable()
export class SendGridSignatureValidator implements SignatureValidatorPort {
  readonly providerName = EventSource.SENDGRID;

  constructor(private readonly env: EnvService) {}

  validate(
    rawBody: string,
    headers: Record<string, string | string[] | undefined>,
  ): boolean {
    const secret = this.env.sendgridSecret;
    if (!secret) {
      return this.env.nodeEnv !== 'production';
    }

    const signature = this.firstHeader(
      headers['x-twilio-email-event-webhook-signature'],
    );
    const timestamp = this.firstHeader(
      headers['x-twilio-email-event-webhook-timestamp'],
    );
    if (!signature || !timestamp) return false;

    const expected = createHmac('sha256', secret)
      .update(timestamp + rawBody)
      .digest('base64');

    return SendGridSignatureValidator.safeEqual(expected, signature);
  }

  private firstHeader(v: string | string[] | undefined): string | undefined {
    return Array.isArray(v) ? v[0] : v;
  }

  private static safeEqual(a: string, b: string): boolean {
    if (a.length !== b.length) return false;
    return timingSafeEqual(Buffer.from(a), Buffer.from(b));
  }
}
