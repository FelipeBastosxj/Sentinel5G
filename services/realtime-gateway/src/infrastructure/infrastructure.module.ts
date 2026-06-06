import { Module } from '@nestjs/common';
import { PgListenerAdapter } from './pg-listener/pg-listener.adapter';
import { RedisFanoutAdapter } from './redis/redis-fanout.adapter';
import { REMOTE_FANOUT_PORT } from '../domain/ports/remote-fanout.port';

@Module({
  providers: [
    PgListenerAdapter,
    RedisFanoutAdapter,
    { provide: REMOTE_FANOUT_PORT, useExisting: RedisFanoutAdapter },
  ],
  exports: [PgListenerAdapter, RedisFanoutAdapter, REMOTE_FANOUT_PORT],
})
export class InfrastructureModule {}
