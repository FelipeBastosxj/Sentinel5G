import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Post,
} from '@nestjs/common';
import {
  infobipWebhookSchema,
  validateOrThrow,
} from '@eventstream/schemas';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import { InfobipNormalizer } from '../domain/normalizers/infobip.normalizer';
import { InfobipSignatureValidator } from '../infrastructure/signatures/infobip-signature.validator';

@Controller('integrations/infobip')
export class InfobipWebhookController {
  constructor(
    private readonly processWebhook: ProcessWebhookUseCase,
    private readonly normalizer: InfobipNormalizer,
    private readonly validator: InfobipSignatureValidator,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async receive(
    @Headers() headers: Record<string, string | string[] | undefined>,
    @Body() body: unknown,
  ): Promise<{ accepted: number }> {
    const parsed = validateOrThrow(infobipWebhookSchema, body);
    const result = await this.processWebhook.execute({
      providerName: 'infobip',
      channelLabel: 'sms',
      rawBody: JSON.stringify(body),
      parsedPayload: parsed,
      headers,
      validator: this.validator,
      normalizer: this.normalizer,
    });
    return { accepted: result.accepted };
  }
}
