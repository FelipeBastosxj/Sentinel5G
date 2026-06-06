import { AsyncLocalStorage } from 'node:async_hooks';
import { Injectable } from '@nestjs/common';

interface CorrelationContext {
  readonly correlationId: string;
}

/**
 * AsyncLocalStorage-backed correlation context.
 *
 * Lets any service downstream of an HTTP / Kafka entry point read the
 * correlation ID without explicitly threading it through every function call.
 * Required by HARDNESS §8 (correlation IDs propagate through all services).
 */
@Injectable()
export class CorrelationService {
  private readonly storage = new AsyncLocalStorage<CorrelationContext>();

  run<T>(correlationId: string, callback: () => T): T {
    return this.storage.run({ correlationId }, callback);
  }

  getCorrelationId(): string | undefined {
    return this.storage.getStore()?.correlationId;
  }
}
