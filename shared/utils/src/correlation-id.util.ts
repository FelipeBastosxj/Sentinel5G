import { generateUuid } from './uuid.util';

/**
 * HTTP header used to propagate the correlation ID across services.
 * Mirrors the value defined in `.env.example` (`CORRELATION_ID_HEADER`).
 */
export const CORRELATION_ID_HEADER = 'x-correlation-id';

/** Kafka header key for correlation propagation. */
export const CORRELATION_ID_KAFKA_HEADER = 'x-correlation-id';

/**
 * Returns the correlation ID from any header bag, generating one when absent.
 * Accepts string | string[] to be compatible with Node http and Express.
 */
export function extractOrCreateCorrelationId(
  headers: Record<string, string | string[] | undefined> | undefined,
): string {
  const raw = headers?.[CORRELATION_ID_HEADER];
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (typeof value === 'string' && value.length > 0) {
    return value;
  }
  return generateUuid();
}
