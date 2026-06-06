import { Inject, Injectable } from '@nestjs/common';
import { CanonicalEvent } from '@eventstream/contracts';
import { ProviderNormalizerPort } from '../../domain/ports/provider-normalizer.port';
import { SignatureValidatorPort } from '../../domain/ports/signature-validator.port';
import {
  CANONICAL_EVENT_FORWARDER_PORT,
  CanonicalEventForwarderPort,
} from '../../domain/ports/canonical-event-forwarder.port';
import { InvalidSignatureError } from '../../domain/errors';
import { CorrelationService } from '../../common/correlation/correlation.module';
import { MetricsService } from '../../common/metrics/metrics.module';
import { AppLoggerService } from '../../common/logger/logger.module';

export interface ProcessWebhookCommand<RawPayload> {
  readonly providerName: string;
  readonly channelLabel: string;
  readonly rawBody: string;
  readonly parsedPayload: RawPayload;
  readonly headers: Record<string, string | string[] | undefined>;
  readonly fullUrl?: string;
  readonly validator: SignatureValidatorPort;
  readonly normalizer: ProviderNormalizerPort<RawPayload>;
}

/**
 * Use case: validate signature → normalize payload → forward to ingestion.
 *
 * Single use case shared by every provider controller. Keeps the policy in
 * one place (HARDNESS §6 — business logic in Application/Domain).
 */
@Injectable()
export class ProcessWebhookUseCase {
  constructor(
    @Inject(CANONICAL_EVENT_FORWARDER_PORT)
    private readonly forwarder: CanonicalEventForwarderPort,
    private readonly correlation: CorrelationService,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {}

  async execute<RawPayload>(
    command: ProcessWebhookCommand<RawPayload>,
  ): Promise<{ accepted: number; events: CanonicalEvent[] }> {
    this.metrics.webhookReceivedTotal.inc({
      provider: command.providerName,
      channel: command.channelLabel,
    });

    const ok = command.validator.validate(
      command.rawBody,
      command.headers,
      command.fullUrl,
    );
    if (!ok) {
      this.metrics.webhookRejectedTotal.inc({
        provider: command.providerName,
        reason: 'signature',
      });
      throw new InvalidSignatureError(command.providerName);
    }

    const correlationId =
      this.correlation.getCorrelationId() ?? command.providerName + '-no-correlation';

    let events: CanonicalEvent[];
    try {
      events = command.normalizer.normalize(command.parsedPayload, {
        correlationId,
      });
    } catch (err) {
      this.metrics.webhookRejectedTotal.inc({
        provider: command.providerName,
        reason: 'normalization',
      });
      throw err;
    }

    for (const event of events) {
      await this.forwarder.forward(event);
    }

    this.logger.info('Webhook processed', {
      provider: command.providerName,
      events: events.length,
    });

    return { accepted: events.length, events };
  }
}
