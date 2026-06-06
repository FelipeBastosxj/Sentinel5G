import { Module } from '@nestjs/common';
import { InfrastructureModule } from '../infrastructure/infrastructure.module';
import { EventsGateway } from '../controllers/events.gateway';
import { BroadcastEventUseCase } from '../application/use-cases/broadcast-event.use-case';
import { RealtimeWiring } from './realtime-wiring.service';
import { EVENT_BROADCASTER_PORT } from '../domain/ports/event-broadcaster.port';

@Module({
  imports: [InfrastructureModule],
  providers: [
    EventsGateway,
    { provide: EVENT_BROADCASTER_PORT, useExisting: EventsGateway },
    BroadcastEventUseCase,
    RealtimeWiring,
  ],
})
export class RealtimeModule {}
