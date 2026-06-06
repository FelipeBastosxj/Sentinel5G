import {
  CORRELATION_ID_HEADER,
  extractOrCreateCorrelationId,
} from './correlation-id.util';

describe('correlation-id.util', () => {
  it('returns the header value when present', () => {
    const id = extractOrCreateCorrelationId({
      [CORRELATION_ID_HEADER]: 'corr-123',
    });
    expect(id).toBe('corr-123');
  });

  it('takes the first value when header is an array', () => {
    const id = extractOrCreateCorrelationId({
      [CORRELATION_ID_HEADER]: ['first', 'second'],
    });
    expect(id).toBe('first');
  });

  it('generates a new id when missing', () => {
    const a = extractOrCreateCorrelationId({});
    const b = extractOrCreateCorrelationId(undefined);
    expect(a).not.toEqual(b);
    expect(a.length).toBeGreaterThan(0);
  });
});
