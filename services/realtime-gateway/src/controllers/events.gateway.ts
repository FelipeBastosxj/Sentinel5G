import {
  Injectable,
  Logger,
  OnGatewayConnection,
  OnGatewayDisconnect,
  OnGatewayInit,
  WebSocketGateway,
  WebSocketServer,
} from '@nestjs/websockets';
import type { Server, Socket } from 'socket.io';
import { CanonicalEvent } from '@eventstream/contracts';
import { EnvService } from '../config/config.module';
import {
  EventBroadcasterPort,
  EventStream,
} from '../domain/ports/event-broadcaster.port';
import { MetricsService } from '../common/observability.module';
import { AppLoggerService } from '../common/common-infra.module';

/**
 * Socket.IO gateway exposing two streams:
 *   - "events"   → CanonicalEvents from Kafka topic events.processed
 *   - "metrics"  → CanonicalEvents from Kafka topic events.metrics
 *
 * Implements EventBroadcasterPort so the application layer can broadcast
 * without depending on socket.io directly.
 */
@Injectable()
@WebSocketGateway({
  // path is set dynamically through createIOServer at bootstrap
  cors: { origin: '*' },
  transports: ['websocket'],
})
export class EventsGateway
  implements
    EventBroadcasterPort,
    OnGatewayInit,
    OnGatewayConnection,
    OnGatewayDisconnect
{
  private readonly nest = new Logger(EventsGateway.name);

  @WebSocketServer()
  private server!: Server;

  constructor(
    private readonly env: EnvService,
    private readonly metrics: MetricsService,
    private readonly logger: AppLoggerService,
  ) {}

  afterInit(): void {
    this.nest.log(
      `WebSocket gateway initialized on path=${this.env.wsPath} cors=${this.env.corsOrigin.join(',')}`,
    );
  }

  handleConnection(client: Socket): void {
    this.metrics.connectedClients.inc();
    this.logger.info('Client connected', { clientId: client.id });
  }

  handleDisconnect(client: Socket): void {
    this.metrics.connectedClients.dec();
    this.logger.info('Client disconnected', { clientId: client.id });
  }

  // -------- EventBroadcasterPort --------------------------------------
  broadcast(stream: EventStream, event: CanonicalEvent): void {
    if (!this.server) return;
    this.server.emit(stream, event);
  }

  connectedClientsCount(): number {
    return this.server?.engine?.clientsCount ?? 0;
  }
}
