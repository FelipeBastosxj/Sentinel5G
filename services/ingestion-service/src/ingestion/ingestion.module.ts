import { Module } from '@nestjs/common';
import { KafkaModule } from '../infrastructure/kafka/kafka.module';
import { IngestEventUseCase } from '../application/use-cases/ingest-event.use-case';
import { IngestController } from '../controllers/ingest.controller';

@Module({
  imports: [KafkaModule],
  controllers: [IngestController],
  providers: [IngestEventUseCase],
})
export class IngestionModule {}
