import { Module } from '@nestjs/common';
import { InfrastructureModule } from '../infrastructure/infrastructure.module';
import { TwilioNormalizer } from '../domain/normalizers/twilio.normalizer';
import { InfobipNormalizer } from '../domain/normalizers/infobip.normalizer';
import { SendGridNormalizer } from '../domain/normalizers/sendgrid.normalizer';
import { CustomNormalizer } from '../domain/normalizers/custom.normalizer';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import { TwilioWebhookController } from '../controllers/twilio-webhook.controller';
import { InfobipWebhookController } from '../controllers/infobip-webhook.controller';
import { SendGridWebhookController } from '../controllers/sendgrid-webhook.controller';
import {
  CustomWebhookController,
  NoopSignatureValidator,
} from '../controllers/custom-webhook.controller';

@Module({
  imports: [InfrastructureModule],
  controllers: [
    TwilioWebhookController,
    InfobipWebhookController,
    SendGridWebhookController,
    CustomWebhookController,
  ],
  providers: [
    ProcessWebhookUseCase,
    TwilioNormalizer,
    InfobipNormalizer,
    SendGridNormalizer,
    CustomNormalizer,
    NoopSignatureValidator,
  ],
})
export class IntegrationsModule {}
