import { Injectable, Logger } from '@nestjs/common';
import {
  WebSocketGateway,
  WebSocketServer,
  OnGatewayConnection,
  OnGatewayDisconnect,
  OnGatewayInit,
} from '@nestjs/websockets';
import type { Server, Socket } from 'socket.io';
import { EnvService } from '../config/config.module';
import {
  EventBroadcasterPort,
  WebhookEventSummary,
} from '../domain/ports/event-broadcaster.port';
import { MetricsService } from '../common/observability.module';
import { AppLoggerService } from '../common/common-infra.module';

/**
 * Socket.IO gateway exposing a single 'webhook_event' stream.
 *
 * Clients can optionally filter by workspaceId by joining a room:
 *   socket.emit('subscribe', { workspaceId: '...' })
 *
 * Implements EventBroadcasterPort so the application layer can broadcast
 * without depending on socket.io directly.
 */
@Injectable()
@WebSocketGateway({
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
      `WebSocket gateway initialized path=${this.env.wsPath} cors=${this.env.corsOrigin.join(',')}`,
    );
  }

  handleConnection(client: Socket): void {
    this.metrics.incConnectedClients();
    this.logger.info('Client connected', { clientId: client.id });

    // Allow clients to subscribe to a specific workspace room
    client.on('subscribe', (data: { workspaceId?: string }) => {
      if (data?.workspaceId) {
        void client.join(data.workspaceId);
        this.logger.info('Client subscribed to workspace', {
          clientId: client.id,
          workspaceId: data.workspaceId,
        });
      }
    });
  }

  handleDisconnect(client: Socket): void {
    this.metrics.decConnectedClients();
    this.logger.info('Client disconnected', { clientId: client.id });
  }

  // -------- EventBroadcasterPort --------------------------------------
  broadcast(workspaceId: string, event: WebhookEventSummary): void {
    if (!this.server) return;
    // Broadcast to the workspace room + global 'all' listener
    this.server.to(workspaceId).emit('webhook_event', event);
    this.server.emit('webhook_event_all', event);
    this.metrics.incBroadcasts();
  }

  connectedClientsCount(): number {
    return this.server?.engine?.clientsCount ?? 0;
  }
}
