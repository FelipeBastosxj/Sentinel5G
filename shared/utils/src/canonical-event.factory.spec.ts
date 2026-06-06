import { Channel, EventType } from '@eventstream/contracts';
import { buildCanonicalEvent } from './canonical-event.factory';
import { isUuid } from './uuid.util';
import { isIsoTimestamp } from './time.util';

describe('canonical-event.factory', () => {
  const baseInput = {
    eventType: EventType.DELIVERY_EVENT,
    channel: Channel.SMS,
    source: 'twilio',
    correlationId: 'corr-1',
  };

  it('fills required identity fields', () => {
    const event = buildCanonicalEvent(baseInput);
    expect(isUuid(event.eventId)).toBe(true);
    expect(isIsoTimestamp(event.timestamp)).toBe(true);
    expect(event.version).toBe('1.0');
    expect(event.channel).toBe(Channel.SMS);
    expect(event.eventType).toBe(EventType.DELIVERY_EVENT);
    expect(event.source).toBe('twilio');
    expect(event.correlationId).toBe('corr-1');
  });

  it('honors caller-provided eventId / timestamp / version', () => {
    const event = buildCanonicalEvent({
      ...baseInput,
      eventId: '11111111-1111-4111-8111-111111111111',
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    });
    expect(event.eventId).toBe('11111111-1111-4111-8111-111111111111');
    expect(event.timestamp).toBe('2026-01-01T00:00:00.000Z');
    expect(event.version).toBe('2.0');
  });

  it('returns an immutable (frozen) event', () => {
    const event = buildCanonicalEvent(baseInput);
    expect(Object.isFrozen(event)).toBe(true);
  });
});
