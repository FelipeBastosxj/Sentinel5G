import { generateUuid, isUuid } from './uuid.util';

describe('uuid.util', () => {
  it('generates a valid v4 UUID', () => {
    const id = generateUuid();
    expect(isUuid(id)).toBe(true);
  });

  it('rejects non-UUID strings', () => {
    expect(isUuid('not-a-uuid')).toBe(false);
    expect(isUuid('')).toBe(false);
    expect(isUuid(undefined)).toBe(false);
    expect(isUuid(123)).toBe(false);
  });

  it('produces unique values', () => {
    const a = generateUuid();
    const b = generateUuid();
    expect(a).not.toEqual(b);
  });
});
