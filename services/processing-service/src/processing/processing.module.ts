import { Module } from '@nestjs/common';
import { KafkaModule } from '../infrastructure/kafka/kafka.module';
import { ClickHouseModule } from '../infrastructure/clickhouse/clickhouse.module';
import { BaseEnricher } from '../domain/enrichment/base.enricher';
import { RetryPolicy } from '../domain/retry/retry.policy';
import { ProcessEventUseCase } from '../application/use-cases/process-event.use-case';
import { EventConsumerController } from '../controllers/event-consumer.controller';
import { EventsQueryController } from '../controllers/events-query.controller';
import { EVENT_ENRICHER_PORT } from '../domain/ports/event-enricher.port';

@Module({
  imports: [KafkaModule, ClickHouseModule],
  providers: [
    BaseEnricher,
    { provide: EVENT_ENRICHER_PORT, useExisting: BaseEnricher },
    RetryPolicy,
    ProcessEventUseCase,
    EventConsumerController,
  ],
  controllers: [EventsQueryController],
})
export class ProcessingModule {}
