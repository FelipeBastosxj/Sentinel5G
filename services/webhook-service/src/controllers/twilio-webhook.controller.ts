import { Controller, Post, Body, Headers, HttpCode, HttpStatus, UseGuards } from '@nestjs/common';
import { ThrottlerGuard } from '@nestjs/throttler';
import { ProcessWebhookUseCase, ProcessWebhookCommand } from '../application/use-cases/process-webhook.use-case';
import { TwilioNormalizer } from '../domain/normalizers/twilio.normalizer';
import { extractOrCreateCorrelationId } from '@eventstream/utils';

@Controller('integrations/twilio')
@UseGuards(ThrottlerGuard)
export class TwilioWebhookController {
  constructor(
    private readonly useCase: ProcessWebhookUseCase,
    private readonly normalizer: TwilioNormalizer,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async handle(@Body() body: unknown, @Headers() headers: Record<string, string>): Promise<{ accepted: boolean }> {
    const correlationId = extractOrCreateCorrelationId(headers);
    const command = new ProcessWebhookCommand(body, this.normalizer, correlationId, 'twilio');
    await this.useCase.execute(command);
    return { accepted: true };
  }
}
