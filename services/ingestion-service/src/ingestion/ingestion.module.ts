import { Module } from '@nestjs/common';
import { IngestEventUseCase } from '../application/use-cases/ingest-event.use-case';
import { IngestController } from '../controllers/ingest.controller';
import { ProcessingForwarderAdapter } from '../infrastructure/forwarder/processing-forwarder.adapter';
import { WEBHOOK_FORWARDER_PORT } from '../domain/ports/event-publisher.port';

@Module({
  controllers: [IngestController],
  providers: [
    IngestEventUseCase,
    ProcessingForwarderAdapter,
    { provide: WEBHOOK_FORWARDER_PORT, useExisting: ProcessingForwarderAdapter },
  ],
})
export class IngestionModule {}
