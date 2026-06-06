import { Module } from '@nestjs/common';
import { TwilioSignatureValidator } from './signatures/twilio-signature.validator';
import { InfobipSignatureValidator } from './signatures/infobip-signature.validator';
import { SendGridSignatureValidator } from './signatures/sendgrid-signature.validator';
import { IngestionHttpClient } from './forwarder/ingestion-http.client';
import { CANONICAL_EVENT_FORWARDER_PORT } from '../domain/ports/canonical-event-forwarder.port';

@Module({
  providers: [
    TwilioSignatureValidator,
    InfobipSignatureValidator,
    SendGridSignatureValidator,
    IngestionHttpClient,
    {
      provide: CANONICAL_EVENT_FORWARDER_PORT,
      useExisting: IngestionHttpClient,
    },
  ],
  exports: [
    TwilioSignatureValidator,
    InfobipSignatureValidator,
    SendGridSignatureValidator,
    CANONICAL_EVENT_FORWARDER_PORT,
  ],
})
export class InfrastructureModule {}
