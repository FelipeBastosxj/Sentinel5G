import { Module } from '@nestjs/common';
import { ProcessWebhookUseCase } from '../application/use-cases/process-webhook.use-case';
import { EVENT_PUBLISHER_TOKEN } from '../domain/ports/injection-tokens';
import { IngestionHttpClient } from '../infrastructure/forwarder/ingestion-http.client';
import { TwilioNormalizer } from '../domain/normalizers/twilio.normalizer';
import { InfobipNormalizer } from '../domain/normalizers/infobip.normalizer';
import { SendGridNormalizer } from '../domain/normalizers/sendgrid.normalizer';
import { CustomNormalizer } from '../domain/normalizers/custom.normalizer';
import { TwilioWebhookController } from '../controllers/twilio-webhook.controller';
import { InfobipWebhookController } from '../controllers/infobip-webhook.controller';
import { SendGridWebhookController } from '../controllers/sendgrid-webhook.controller';
import { CustomWebhookController } from '../controllers/custom-webhook.controller';

@Module({
  controllers: [
    TwilioWebhookController,
    InfobipWebhookController,
    SendGridWebhookController,
    CustomWebhookController,
  ],
  providers: [
    ProcessWebhookUseCase,
    {
      provide: EVENT_PUBLISHER_TOKEN,
      useClass: IngestionHttpClient,
    },
    TwilioNormalizer,
    InfobipNormalizer,
    SendGridNormalizer,
    CustomNormalizer,
  ],
})
export class IntegrationsModule {}
