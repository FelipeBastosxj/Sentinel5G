import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Injectable,
  Post,
  UseGuards,
} from '@nestjs/common';
import { ThrottlerGuard } from '@nestjs/throttler';
import {
  ProcessWebhookUseCase,
  ProcessWebhookCommand,
} from '../application/use-cases/process-webhook.use-case';
import { CustomNormalizer } from '../domain/normalizers/custom.normalizer';
import { extractOrCreateCorrelationId } from '@eventstream/utils';

/**
 * NoopSignatureValidator — always passes. Used by the custom integration
 * endpoint where authentication is delegated to the platform's networking
 * layer (private VPC, mTLS, API gateway). Documented in integrations.md.
 */
@Injectable()
export class NoopSignatureValidator {
  readonly providerName = 'custom';
  validate(): boolean {
    return true;
  }
}

@Controller('integrations/custom')
@UseGuards(ThrottlerGuard)
export class CustomWebhookController {
  constructor(
    private readonly useCase: ProcessWebhookUseCase,
    private readonly normalizer: CustomNormalizer,
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
      'custom',
    );
    await this.useCase.execute(command);
    return { accepted: true };
  }
}
