import { Channel, EventType } from '@eventstream/contracts';
import { canonicalEventSchema } from './canonical-event.schema';
import { validateOrThrow, SchemaValidationError } from './validate.helper';

const validEvent = {
  eventId: '550e8400-e29b-41d4-a716-446655440000',
  eventType: EventType.DELIVERY_EVENT,
  channel: Channel.SMS,
  timestamp: '2026-06-05T12:00:00.000Z',
  source: 'twilio',
  correlationId: 'corr-1',
};

describe('canonicalEventSchema', () => {
  it('accepts a valid CanonicalEvent', () => {
    expect(() => validateOrThrow(canonicalEventSchema, validEvent)).not.toThrow();
  });

  it('rejects events missing required fields', () => {
    expect(() =>
      validateOrThrow(canonicalEventSchema, { ...validEvent, eventId: undefined }),
    ).toThrow(SchemaValidationError);
    expect(() =>
      validateOrThrow(canonicalEventSchema, { ...validEvent, channel: undefined }),
    ).toThrow(SchemaValidationError);
    expect(() =>
      validateOrThrow(canonicalEventSchema, { ...validEvent, correlationId: '' }),
    ).toThrow(SchemaValidationError);
  });

  it('rejects unknown channel values', () => {
    expect(() =>
      validateOrThrow(canonicalEventSchema, { ...validEvent, channel: 'mms' }),
    ).toThrow(SchemaValidationError);
  });

  it('rejects events with extra unknown properties', () => {
    expect(() =>
      validateOrThrow(canonicalEventSchema, { ...validEvent, extra: 'nope' }),
    ).toThrow(SchemaValidationError);
  });
});
