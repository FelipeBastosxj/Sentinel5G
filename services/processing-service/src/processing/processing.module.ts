import { Module } from '@nestjs/common';
import { DatabaseModule } from '../infrastructure/database/database.module';
import { TwilioNormalizer } from '../infrastructure/normalizers/twilio.normalizer';
import { ProcessEventUseCase } from '../application/use-cases/process-event.use-case';
import { WebhookReceiveController } from '../controllers/webhook-receive.controller';
import { EventsQueryController } from '../controllers/events-query.controller';
import { WorkspaceController } from '../controllers/workspace.controller';
import { HealthController } from '../controllers/health.controller';

@Module({
  imports: [DatabaseModule],
  providers: [TwilioNormalizer, ProcessEventUseCase],
  controllers: [WebhookReceiveController, EventsQueryController, WorkspaceController, HealthController],
})
export class ProcessingModule {}
