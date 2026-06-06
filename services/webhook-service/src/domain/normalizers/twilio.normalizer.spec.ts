import { Channel, EventType } from '@eventstream/contracts';
import { TwilioNormalizer } from './twilio.normalizer';
import { NormalizationContext } from '../ports/provider-normalizer.port';

describe('TwilioNormalizer', () => {
  const normalizer = new TwilioNormalizer();
  const ctx: NormalizationContext = {
    correlationId: 'test-correlation-id',
    provider: 'twilio',
    receivedAt: new Date().toISOString(),
  };

  it('maps a delivered event to DELIVERY_EVENT on SMS channel', () => {
    const [event] = normalizer.normalize(
      {
        MessageSid: 'SM1',
        AccountSid: 'AC1',
        From: '+15550000001',
        To: '+15550000002',
        MessageStatus: 'delivered',
      },
      ctx,
    );
    expect(event.eventType).toBe(EventType.DELIVERY_EVENT);
    expect(event.channel).toBe(Channel.SMS);
    expect(event.source).toBe('twilio');
    expect(event.correlationId).toBe('test-correlation-id');
    expect(event.payload).toMatchObject({ messageSid: 'SM1', status: 'delivered' });
  });

  it('maps failed/undelivered to ERROR_EVENT', () => {
    const [event] = normalizer.normalize(
      {
        MessageSid: 'SM2',
        AccountSid: 'AC1',
        From: '+1',
        To: '+2',
        MessageStatus: 'failed',
        ErrorCode: '30007',
      },
      ctx,
    );
    expect(event.eventType).toBe(EventType.ERROR_EVENT);
    expect(event.payload).toMatchObject({ errorCode: '30007' });
  });

  it('falls back to STATUS_EVENT for unknown statuses', () => {
    const [event] = normalizer.normalize(
      {
        MessageSid: 'SM3',
        AccountSid: 'AC1',
        From: '+1',
        To: '+2',
        MessageStatus: 'queued',
      },
      ctx,
    );
    expect(event.eventType).toBe(EventType.STATUS_EVENT);
  });
});
