import { Injectable } from '@nestjs/common';
import { EventSource } from '@eventstream/contracts';
import { EnvService } from '../../config/env.service';
import { SignatureValidatorPort } from '../../domain/ports/signature-validator.port';

/**
 * Infobip signature validation.
 *
 * Infobip recommends using a shared bearer secret in the Authorization header.
 * Different setups exist; we go with the simplest production pattern:
 *
 *   Authorization: Bearer <INFOBIP_WEBHOOK_SECRET>
 *
 * If INFOBIP_WEBHOOK_SECRET is unset, validation is skipped only in non-prod.
 */
@Injectable()
export class InfobipSignatureValidator implements SignatureValidatorPort {
  readonly providerName = EventSource.INFOBIP;

  constructor(private readonly env: EnvService) {}

  validate(
    _rawBody: string,
    headers: Record<string, string | string[] | undefined>,
  ): boolean {
    const secret = this.env.infobipSecret;
    if (!secret) {
      return this.env.nodeEnv !== 'production';
    }

    const auth = this.firstHeader(headers['authorization']);
    if (!auth) return false;
    const expected = `Bearer ${secret}`;
    return auth === expected;
  }

  private firstHeader(v: string | string[] | undefined): string | undefined {
    return Array.isArray(v) ? v[0] : v;
  }
}
