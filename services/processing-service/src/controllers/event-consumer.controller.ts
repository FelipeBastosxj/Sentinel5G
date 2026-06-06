import { Injectable, OnModuleInit } from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import { KafkaConsumerAdapter } from '../infrastructure/kafka/kafka-consumer.adapter';
import { ProcessEventUseCase } from '../application/use-cases/process-event.use-case';
import { AppLoggerService } from '../common/logger/logger.module';

/**
 * EventConsumerController — wires the Kafka consumer adapter to the use case.
 *
 * Despite the name, it is intentionally not an `@Controller()` — it is the
 * "input adapter" in the hexagonal sense for asynchronous Kafka traffic.
 * Stays a thin pass-through (HARDNESS §6).
 */
@Injectable()
export class EventConsumerController implements OnModuleInit {
  constructor(
    private readonly consumer: KafkaConsumerAdapter,
    private readonly useCase: ProcessEventUseCase,
    private readonly logger: AppLoggerService,
  ) {}

  onModuleInit(): void {
    this.consumer.registerHandler(async (event: CanonicalEvent) => {
      try {
        await this.useCase.execute(event);
      } catch (err) {
        // Use case already DLQ'd. Log here for visibility.
        this.logger.error('Unrecoverable error in processing pipeline', {
          eventId: event.eventId,
          error: (err as Error).message,
        });
      }
    });
  }
}
