import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';

interface MinuteBucket {
  readonly minute: string;
  readonly count: number;
}

@Injectable()
export class MetricsStore {
  private readonly client = inject(RealtimeClient);

  private readonly events = signal<CanonicalEvent[]>([]);

  /** Count of events by channel for the current session window. */
  readonly perChannel = computed(() => {
    const map = new Map<string, number>();
    for (const evt of this.events()) {
      const key = String(evt.channel);
      map.set(key, (map.get(key) ?? 0) + 1);
    }
    return Array.from(map.entries())
      .map(([channel, count]) => ({ channel, count }))
      .sort((a, b) => b.count - a.count);
  });

  /** Count of events by event type. */
  readonly perEventType = computed(() => {
    const map = new Map<string, number>();
    for (const evt of this.events()) {
      const key = String(evt.eventType);
      map.set(key, (map.get(key) ?? 0) + 1);
    }
    return Array.from(map.entries()).map(([eventType, count]) => ({
      eventType,
      count,
    }));
  });

  /** Events per minute over the last 30 minutes. */
  readonly perMinute = computed<MinuteBucket[]>(() => {
    const buckets = new Map<string, number>();
    for (const evt of this.events()) {
      const d = new Date(evt.timestamp);
      const minute = `${d.toISOString().slice(0, 16)}Z`;
      buckets.set(minute, (buckets.get(minute) ?? 0) + 1);
    }
    return Array.from(buckets.entries())
      .map(([minute, count]) => ({ minute, count }))
      .sort((a, b) => a.minute.localeCompare(b.minute))
      .slice(-30);
  });

  readonly total = computed(() => this.events().length);

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
