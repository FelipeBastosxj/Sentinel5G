import { AsyncLocalStorage } from 'node:async_hooks';
import { Global, Injectable, Module } from '@nestjs/common';

@Injectable()
export class CorrelationService {
  private readonly storage = new AsyncLocalStorage<{ correlationId: string }>();

  run<T>(correlationId: string, callback: () => T): T {
    return this.storage.run({ correlationId }, callback);
  }

  getCorrelationId(): string | undefined {
    return this.storage.getStore()?.correlationId;
  }
}

@Global()
@Module({
  providers: [CorrelationService],
  exports: [CorrelationService],
})
export class CorrelationModule {}
