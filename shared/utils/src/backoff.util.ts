/**
 * Computes a stable backoff delay (ms) using exponential growth + full jitter.
 *
 * delay = random(0, baseMs * 2^attempt) capped at maxMs.
 *
 * Used by the processing-service retry policy (HARDNESS §10).
 */
export function exponentialBackoffMs(
  attempt: number,
  baseMs = 250,
  maxMs = 30_000,
): number {
  const exp = Math.min(maxMs, baseMs * 2 ** Math.max(0, attempt));
  return Math.floor(Math.random() * exp);
}

/** Promise-based sleep helper. Used by retry / backpressure code paths. */
export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
