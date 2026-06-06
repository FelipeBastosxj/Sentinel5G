import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Post,
  Req,
  UseGuards,
} from '@nestjs/common';
import { ThrottlerGuard } from '@nestjs/throttler';
import type { Request } from 'express';
import {
  sendgridWebhookSchema,
  validateOrThrow,
} from '@eventstream/schemas';
import { ProcessWebhookUseCase, ProcessWebhookCommand } from '../application/use-cases/process-webhook.use-case';
import { SendGridNormalizer } from '../domain/normalizers/sendgrid.normalizer';
import { extractOrCreateCorrelationId } from '@eventstream/utils';

@Controller('integrations/sendgrid')
@UseGuards(ThrottlerGuard)
export class SendGridWebhookController {
  constructor(
    private readonly useCase: ProcessWebhookUseCase,
    private readonly normalizer: SendGridNormalizer,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async handle(
    @Req() req: Request,
    @Headers() headers: Record<string, string | string[] | undefined>,
    @Body() body: unknown,
  ): Promise<{ accepted: boolean }> {
    const correlationId = extractOrCreateCorrelationId(headers);
    const command = new ProcessWebhookCommand(body, this.normalizer, correlationId, 'sendgrid');
    await this.useCase.execute(command);
    return { accepted: true };
  }
}
