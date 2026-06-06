import { Injectable, Logger, Inject } from '@nestjs/common';
import { ProviderNormalizerPort, NormalizationContext } from '../../domain/ports/provider-normalizer.port';
import { EventPublisherPort } from '../../domain/ports/event-publisher.port';
import { EVENT_PUBLISHER_TOKEN } from '../../domain/ports/injection-tokens';

export class ProcessWebhookCommand {
  constructor(
    readonly rawPayload: unknown,
    readonly normalizer: ProviderNormalizerPort,
    readonly correlationId: string,
    readonly provider: string,
  ) {}
}

@Injectable()
export class ProcessWebhookUseCase {
  private readonly logger = new Logger(ProcessWebhookUseCase.name);

  constructor(
    @Inject(EVENT_PUBLISHER_TOKEN)
    private readonly publisher: EventPublisherPort,
  ) {}

  async execute(command: ProcessWebhookCommand): Promise<void> {
    const context: NormalizationContext = {
      correlationId: command.correlationId,
      provider: command.provider,
      receivedAt: new Date().toISOString(),
    };

    const events = command.normalizer.normalize(command.rawPayload, context);

    for (const event of events) {
      await this.publisher.publish(event);
      this.logger.log(
        `Published event ${event.eventId} [${event.eventType}] from ${command.provider}`,
      );
    }
  }
}
