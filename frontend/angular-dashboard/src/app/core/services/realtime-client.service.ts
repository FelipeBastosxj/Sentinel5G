import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { Injectable, OnDestroy, signal } from '@angular/core';
import { io, Socket } from 'socket.io-client';
import { environment } from '../../../environments/environment';

export type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'disconnected';

/**
 * RealtimeClient — single Socket.IO connection shared across features.
 *
 * After connecting it joins the workspace-specific room so events are
 * filtered server-side (only this workspace's webhooks arrive).
 */
@Injectable({ providedIn: 'root' })
export class RealtimeClient implements OnDestroy {
  private socket: Socket | null = null;
  private workspaceId: string | null = null;

  readonly status    = signal<ConnectionStatus>('idle');
  readonly lastEvent = signal<WebhookEventSummary | null>(null);

  connect(workspaceId: string): void {
    if (this.socket && this.workspaceId === workspaceId) return;

    // Reconnect if workspace changed
    if (this.socket) this.disconnect();

    this.workspaceId = workspaceId;
    this.status.set('connecting');

    this.socket = io(environment.realtimeUrl, {
      path: environment.realtimePath,
      transports: ['websocket'],
    });

    this.socket.on('connect', () => {
      this.status.set('connected');
      // Join workspace room — server will only push events for this workspace
      this.socket!.emit('subscribe', { workspaceId });
    });

    this.socket.on('disconnect', () => this.status.set('disconnected'));

    // Primary stream: new webhook event arrived
    this.socket.on('webhook_event',     (e: WebhookEventSummary) => this.lastEvent.set(e));
    // Fallback: global stream (all workspaces) — same model
    this.socket.on('webhook_event_all', (e: WebhookEventSummary) => this.lastEvent.set(e));
  }

  disconnect(): void {
    this.socket?.disconnect();
    this.socket = null;
    this.workspaceId = null;
    this.status.set('idle');
  }

  ngOnDestroy(): void {
    this.disconnect();
  }
}
