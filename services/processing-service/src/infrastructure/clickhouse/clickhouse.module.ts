import { Module } from '@nestjs/common';
import { ClickHouseAdapter } from './clickhouse.adapter';
import { EVENT_STORE_PORT } from '../../domain/ports/event-store.port';

@Module({
  providers: [
    ClickHouseAdapter,
    { provide: EVENT_STORE_PORT, useExisting: ClickHouseAdapter },
  ],
  exports: [EVENT_STORE_PORT, ClickHouseAdapter],
})
export class ClickHouseModule {}
