import { Injectable } from '@nestjs/common';
import { exponentialBackoffMs, sleep } from '@eventstream/utils';

export interface RetryOptions {
  readonly attempts: number;
  readonly baseDelayMs: number;
}

/**
 * RetryPolicy — runs an async fn with exponential backoff + full jitter.
 *
 * Used by the processing pipeline; failures past `attempts` propagate so the
 * caller can route the event to the DLQ (HARDNESS §10).
 */
@Injectable()
export class RetryPolicy {
  async run<T>(
    operation: () => Promise<T>,
    options: RetryOptions,
    onRetry?: (attempt: number, err: Error) => void,
  ): Promise<T> {
    let lastErr: unknown;
    for (let attempt = 0; attempt <= options.attempts; attempt++) {
      try {
        return await operation();
      } catch (err) {
        lastErr = err;
        if (attempt >= options.attempts) break;
        onRetry?.(attempt + 1, err as Error);
        const delay = exponentialBackoffMs(attempt, options.baseDelayMs);
        await sleep(delay);
      }
    }
    throw lastErr;
  }
}
