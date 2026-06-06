import { randomUUID } from 'node:crypto';

/**
 * Generates an RFC 4122 v4 UUID using the Node.js standard library.
 *
 * Wrapped here so callers do not import `node:crypto` directly,
 * keeping room for swapping the implementation (e.g., ULID) without
 * touching every service.
 */
export function generateUuid(): string {
  return randomUUID();
}

/** Lightweight regex check for UUID v1–v5 format. */
const UUID_REGEX =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function isUuid(value: unknown): value is string {
  return typeof value === 'string' && UUID_REGEX.test(value);
}
