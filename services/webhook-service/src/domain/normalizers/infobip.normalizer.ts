import { Injectable } from '@nestjs/common';
import {
  CanonicalEvent,
  Channel,
  EventSource,
  EventType,
  InfobipDeliveryReport,
  InfobipStatusGroup,
  InfobipWebhookPayload,
} from '@eventstream/contracts';
import { buildCanonicalEvent } from '@eventstream/utils';
import {
  NormalizationContext,
  ProviderNormalizerPort,
} from '../ports/provider-normalizer.port';

/**
 * Maps an Infobip delivery-report batch to one CanonicalEvent per result.
 */
@Injectable()
export class InfobipNormalizer implements ProviderNormalizerPort
{
  readonly providerName = EventSource.INFOBIP;

  normalize(
    payload: unknown,
    context: NormalizationContext,
  ): CanonicalEvent[] {
    const p = payload as InfobipWebhookPayload;
    return p.results.map((report) => this.toEvent(report, context));
  }

  private toEvent(
    report: InfobipDeliveryReport,
    ctx: NormalizationContext,
  ): CanonicalEvent {
    return buildCanonicalEvent({
      eventType: InfobipNormalizer.mapEventType(report.status.groupName),
      channel: Channel.SMS,
      source: EventSource.INFOBIP,
      correlationId: ctx.correlationId,
      timestamp: report.doneAt ?? report.sentAt,
      metadata: {
        provider: EventSource.INFOBIP,
        bulkId: report.bulkId,
        smsCount: report.smsCount,
        mccMnc: report.mccMnc,
      },
      payload: {
        messageId: report.messageId,
        from: report.from,
        to: report.to,
        statusGroup: report.status.groupName,
        statusName: report.status.name,
        statusDescription: report.status.description,
        error: report.error,
        price: report.price,
      },
    });
  }

  private static mapEventType(group: InfobipStatusGroup): EventType {
    switch (group) {
      case 'DELIVERED':
        return EventType.DELIVERY_EVENT;
      case 'UNDELIVERABLE':
      case 'EXPIRED':
      case 'REJECTED':
        return EventType.ERROR_EVENT;
      default:
        return EventType.STATUS_EVENT;
    }
  }
}
