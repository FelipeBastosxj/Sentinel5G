import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';

export interface ProviderHealth {
  readonly source: string;
  readonly successCount: number;
  readonly errorCount: number;
  readonly successRate: number; // 0..1
  readonly lastSeen: string;
}

@Injectable()
export class IntegrationsStore {
  private readonly client = inject(RealtimeClient);

  private readonly events = signal<CanonicalEvent[]>([]);

  readonly providers = computed<ProviderHealth[]>(() => {
    const map = new Map<
      string,
      { success: number; error: number; lastSeen: string }
    >();
    for (const evt of this.events()) {
      const key = String(evt.source);
      const bucket = map.get(key) ?? { success: 0, error: 0, lastSeen: '' };
      const isError = String(evt.eventType).toUpperCase().includes('ERROR');
      if (isError) bucket.error += 1;
      else bucket.success += 1;
      if (evt.timestamp > bucket.lastSeen) bucket.lastSeen = evt.timestamp;
      map.set(key, bucket);
    }
    return Array.from(map.entries())
      .map(([source, b]) => {
        const total = b.success + b.error;
        return {
          source,
          successCount: b.success,
          errorCount: b.error,
          successRate: total === 0 ? 0 : b.success / total,
          lastSeen: b.lastSeen,
        };
      })
      .sort((a, b) => b.successCount + b.errorCount - (a.successCount + a.errorCount));
  });

  constructor() {
    this.client.connect();
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this.events.update((prev) => {
        const next = [...prev, evt];
        return next.length > 5000 ? next.slice(-5000) : next;
      });
    });
  }
}
