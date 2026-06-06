import { Injectable, OnDestroy, signal } from '@angular/core';
import { io, Socket } from 'socket.io-client';
import { CanonicalEvent } from '@eventstream/contracts';
import { environment } from '../../../environments/environment';

export type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'disconnected';

/**
 * RealtimeClient — single Socket.IO client shared by every feature.
 *
 * Exposes plain Signals so feature stores can subscribe via `effect()` or
 * `computed()`. No global state library (HARDNESS §7).
 */
@Injectable({ providedIn: 'root' })
export class RealtimeClient implements OnDestroy {
  private socket: Socket | null = null;

  readonly status = signal<ConnectionStatus>('idle');
  readonly lastEvent = signal<CanonicalEvent | null>(null);
  readonly lastMetric = signal<CanonicalEvent | null>(null);

  connect(): void {
    if (this.socket) return;
    this.status.set('connecting');
    this.socket = io(environment.realtimeUrl, {
      path: environment.realtimePath,
      transports: ['websocket'],
    });

    this.socket.on('connect', () => this.status.set('connected'));
    this.socket.on('disconnect', () => this.status.set('disconnected'));
    this.socket.on('events', (payload: CanonicalEvent) => this.lastEvent.set(payload));
    this.socket.on('metrics', (payload: CanonicalEvent) => this.lastMetric.set(payload));
  }

  disconnect(): void {
    this.socket?.disconnect();
    this.socket = null;
    this.status.set('idle');
  }

  ngOnDestroy(): void {
    this.disconnect();
  }
}
