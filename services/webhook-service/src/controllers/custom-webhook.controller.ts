import {
  Body,
  Controller,
  Headers,
  HttpCode,
  HttpStatus,
  Injectable,
  Post,
} from '@nestjs/common';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import {
  CustomNormalizer,
  CustomWebhookPayload,
} from '../domain/normalizers/custom.normalizer';
import { SignatureValidatorPort } from '../domain/ports/signature-validator.port';

/**
 * NoopSignatureValidator — always passes. Used by the custom integration
 * endpoint where authentication is delegated to the platform's networking
 * layer (private VPC, mTLS, API gateway). Documented in integrations.md.
 */
@Injectable()
export class NoopSignatureValidator implements SignatureValidatorPort {
  readonly providerName = 'custom';
  validate(): boolean {
    return true;
  }
}

@Controller('integrations/custom')
export class CustomWebhookController {
  constructor(
    private readonly processWebhook: ProcessWebhookUseCase,
    private readonly normalizer: CustomNormalizer,
    private readonly validator: NoopSignatureValidator,
  ) {}

  @Post('webhook')
  @HttpCode(HttpStatus.ACCEPTED)
  async receive(
    @Headers() headers: Record<string, string | string[] | undefined>,
    @Body() body: CustomWebhookPayload,
  ): Promise<{ accepted: number }> {
    const result = await this.processWebhook.execute({
      providerName: 'custom',
      channelLabel: String(body?.channel ?? 'unknown'),
      rawBody: JSON.stringify(body),
      parsedPayload: body,
      headers,
      validator: this.validator,
      normalizer: this.normalizer,
    });
    return { accepted: result.accepted };
  }
}
