/**
 * Returns the current time as an ISO-8601 string in UTC.
 * Centralized so the entire platform agrees on the timestamp format
 * required by the Canonical Event contract.
 */
export function nowIsoUtc(): string {
  return new Date().toISOString();
}

/** True when the input is a valid ISO-8601 timestamp parseable by Date. */
export function isIsoTimestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false;
  const ms = Date.parse(value);
  return Number.isFinite(ms);
}
