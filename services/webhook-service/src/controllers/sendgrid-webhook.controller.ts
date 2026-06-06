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
  sendgridWebhookSchema,
  validateOrThrow,
} from '@eventstream/schemas';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import { SendGridNormalizer } from '../domain/normalizers/sendgrid.normalizer';
import { SendGridSignatureValidator } from '../infrastructure/signatures/sendgrid-signature.validator';

@Controller('integrations/sendgrid')
export class SendGridWebhookController {
  constructor(
    private readonly processWebhook: ProcessWebhookUseCase,
    private readonly normalizer: SendGridNormalizer,
    private readonly validator: SendGridSignatureValidator,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async receive(
    @Req() req: Request,
    @Headers() headers: Record<string, string | string[] | undefined>,
    @Body() body: unknown,
  ): Promise<{ accepted: number }> {
    const parsed = validateOrThrow(sendgridWebhookSchema, body);
    // The SendGrid HMAC is computed over the *raw* request body. We reconstruct
    // it from the parsed JSON; for high-fidelity verification, the raw body
    // should be captured by an upstream raw-body parser (future work).
    const rawBody = (req as Request & { rawBody?: string }).rawBody ?? JSON.stringify(body);

    const result = await this.processWebhook.execute({
      providerName: 'sendgrid',
      channelLabel: 'email',
      rawBody,
      parsedPayload: parsed,
      headers,
      validator: this.validator,
      normalizer: this.normalizer,
    });
    return { accepted: result.accepted };
  }
}
