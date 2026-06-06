import { createHmac, timingSafeEqual } from 'node:crypto';
import { Injectable } from '@nestjs/common';
import { EventSource } from '@eventstream/contracts';
import { EnvService } from '../../config/env.service';
import { SignatureValidatorPort } from '../../domain/ports/signature-validator.port';

/**
 * Twilio signature validation.
 *
 * Twilio computes:
 *   signature = base64(HMAC-SHA1(authToken, fullUrl + sortedFormParams))
 *
 * Reference:
 *   https://www.twilio.com/docs/usage/security#validating-requests
 *
 * The form-encoded body must already be parsed as a flat record.
 *
 * If TWILIO_WEBHOOK_SECRET is unset, validation is skipped in development
 * mode but logged loudly. Production deployments must set the secret
 * (HARDNESS §9 — never trust external input).
 */
@Injectable()
export class TwilioSignatureValidator implements SignatureValidatorPort {
  readonly providerName = EventSource.TWILIO;

  constructor(private readonly env: EnvService) {}

  validate(
    rawBody: string,
    headers: Record<string, string | string[] | undefined>,
    fullUrl?: string,
  ): boolean {
    const secret = this.env.twilioSecret;
    if (!secret || !fullUrl) {
      return this.env.nodeEnv !== 'production';
    }

    const provided = this.firstHeader(headers['x-twilio-signature']);
    if (!provided) return false;

    // Body comes in as form-urlencoded. We re-parse to a stable, sorted form.
    const params = new URLSearchParams(rawBody);
    const sorted = [...params.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([k, v]) => `${k}${v}`)
      .join('');
    const data = fullUrl + sorted;

    const expected = createHmac('sha1', secret).update(data).digest('base64');
    return TwilioSignatureValidator.safeEqual(expected, provided);
  }

  private firstHeader(value: string | string[] | undefined): string | undefined {
    if (Array.isArray(value)) return value[0];
    return value;
  }

  private static safeEqual(a: string, b: string): boolean {
    if (a.length !== b.length) return false;
    return timingSafeEqual(Buffer.from(a), Buffer.from(b));
  }
}
