import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Post,
  Req,
} from '@nestjs/common';
import type { Request } from 'express';
import {
  twilioWebhookSchema,
  validateOrThrow,
} from '@eventstream/schemas';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import { TwilioNormalizer } from '../domain/normalizers/twilio.normalizer';
import { TwilioSignatureValidator } from '../infrastructure/signatures/twilio-signature.validator';

@Controller('integrations/twilio')
export class TwilioWebhookController {
  constructor(
    private readonly processWebhook: ProcessWebhookUseCase,
    private readonly normalizer: TwilioNormalizer,
    private readonly validator: TwilioSignatureValidator,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async receive(
    @Req() req: Request,
    @Headers() headers: Record<string, string | string[] | undefined>,
    @Body() body: Record<string, string>,
  ): Promise<{ accepted: number }> {
    const parsed = validateOrThrow(twilioWebhookSchema, body);
    const fullUrl = `${req.protocol}://${req.get('host')}${req.originalUrl}`;
    const rawBody = TwilioWebhookController.encodeForm(body);

    const result = await this.processWebhook.execute({
      providerName: 'twilio',
      channelLabel: 'sms',
      rawBody,
      parsedPayload: parsed,
      headers,
      fullUrl,
      validator: this.validator,
      normalizer: this.normalizer,
    });
    return { accepted: result.accepted };
  }

  /**
   * Re-encodes a parsed form-urlencoded body back to its canonical wire form
   * (sorted by key) so the HMAC computation matches Twilio's reference impl.
   */
  private static encodeForm(body: Record<string, string>): string {
    return Object.entries(body)
      .map(
        ([k, v]) =>
          `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`,
      )
      .join('&');
  }
}
