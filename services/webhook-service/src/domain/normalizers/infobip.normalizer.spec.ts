import { Channel, EventType } from '@eventstream/contracts';
import { InfobipNormalizer } from './infobip.normalizer';
import { NormalizationContext } from '../ports/provider-normalizer.port';

describe('InfobipNormalizer', () => {
  const normalizer = new InfobipNormalizer();
  const ctx: NormalizationContext = {
    correlationId: 'test-correlation-id',
    provider: 'infobip',
    receivedAt: new Date().toISOString(),
  };

  it('emits one CanonicalEvent per delivery report', () => {
    const events = normalizer.normalize(
      {
        results: [
          {
            messageId: 'm1',
            to: '+15550000001',
            status: { id: 5, groupId: 3, groupName: 'DELIVERED', name: 'DELIVERED_TO_HANDSET' },
          },
          {
            messageId: 'm2',
            to: '+15550000002',
            status: { id: 9, groupId: 2, groupName: 'UNDELIVERABLE', name: 'UNDELIVERABLE_REJECTED_OPERATOR' },
          },
        ],
      },
      ctx,
    );

    expect(events).toHaveLength(2);
    expect(events[0].eventType).toBe(EventType.DELIVERY_EVENT);
    expect(events[0].channel).toBe(Channel.SMS);
    expect(events[1].eventType).toBe(EventType.ERROR_EVENT);
  });
});
