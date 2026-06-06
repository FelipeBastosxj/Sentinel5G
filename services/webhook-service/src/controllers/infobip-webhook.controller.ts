import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Post,
  UseGuards,
} from '@nestjs/common';
import { ThrottlerGuard } from '@nestjs/throttler';
import {
  ProcessWebhookUseCase,
  ProcessWebhookCommand,
} from '../application/use-cases/process-webhook.use-case';
import { InfobipNormalizer } from '../domain/normalizers/infobip.normalizer';
import { extractOrCreateCorrelationId } from '@eventstream/utils';

@Controller('integrations/infobip')
@UseGuards(ThrottlerGuard)
export class InfobipWebhookController {
  constructor(
    private readonly useCase: ProcessWebhookUseCase,
    private readonly normalizer: InfobipNormalizer,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async handle(
    @Body() body: unknown,
    @Headers() headers: Record<string, string>,
  ): Promise<{ accepted: boolean }> {
    const correlationId = extractOrCreateCorrelationId(headers);
    const command = new ProcessWebhookCommand(
      body,
      this.normalizer,
      correlationId,
      'infobip',
    );
    await this.useCase.execute(command);
    return { accepted: true };
  }
}
