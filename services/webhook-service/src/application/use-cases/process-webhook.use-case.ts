import { Injectable, Logger, Inject } from '@nestjs/common';
import { ProviderNormalizerPort, NormalizationContext } from '../../domain/ports/provider-normalizer.port';
import { CanonicalEventForwarderPort, CANONICAL_EVENT_FORWARDER_PORT } from '../../domain/ports/canonical-event-forwarder.port';

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
    @Inject(CANONICAL_EVENT_FORWARDER_PORT)
    private readonly forwarder: CanonicalEventForwarderPort,
  ) {}

  async execute(command: ProcessWebhookCommand): Promise<void> {
    const context: NormalizationContext = {
      correlationId: command.correlationId,
      provider: command.provider,
      receivedAt: new Date().toISOString(),
    };

    const events = command.normalizer.normalize(command.rawPayload, context);

    for (const event of events) {
      await this.forwarder.forward(event);
      this.logger.log(
        `Forwarded event ${event.eventId} [${event.eventType}] from ${command.provider}`,
      );
    }
  }
}
