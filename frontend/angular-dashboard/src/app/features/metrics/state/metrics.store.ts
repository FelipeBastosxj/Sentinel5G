import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventsApiService } from '../../../core/services/events-api.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';

interface MinuteBucket {
  readonly minute: string;
  readonly count: number;
}

const STORAGE_KEY = 'es:metrics:events';
const MAX_EVENTS  = 5000;

function loadFromStorage(): WebhookEventSummary[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as WebhookEventSummary[]) : [];
  } catch {
    return [];
  }
}

@Injectable()
export class MetricsStore {
  private readonly client = inject(RealtimeClient);
  private readonly api    = inject(EventsApiService);
  private readonly ws     = inject(WorkspaceSessionService);

  private readonly events = signal<WebhookEventSummary[]>(loadFromStorage());

  readonly perChannel = computed(() => {
    const map = new Map<string, number>();
    for (const evt of this.events()) {
      const key = evt.provider;
      map.set(key, (map.get(key) ?? 0) + 1);
    }
    return Array.from(map.entries())
      .map(([channel, count]) => ({ channel, count }))
      .sort((a, b) => b.count - a.count);
  });

  readonly perEventType = computed(() => {
    const map = new Map<string, number>();
    for (const evt of this.events()) {
      map.set(evt.eventType, (map.get(evt.eventType) ?? 0) + 1);
    }
    return Array.from(map.entries()).map(([eventType, count]) => ({ eventType, count }));
  });

  readonly perMinute = computed<MinuteBucket[]>(() => {
    const buckets = new Map<string, number>();
    for (const evt of this.events()) {
      const d      = new Date(evt.receivedAt instanceof Date ? evt.receivedAt : String(evt.receivedAt));
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
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);

      // Hydrate from PostgreSQL
      this.api.fetchRecent({ limit: 500, workspaceId: session.workspaceId }).then((historical) => {
        if (historical.length === 0) return;
        this.events.update((current) => {
          const existingIds = new Set(current.map((e) => e.id));
          const merged = [
            ...historical.filter((e) => !existingIds.has(e.id)),
            ...current,
          ];
          return merged.length > MAX_EVENTS ? merged.slice(-MAX_EVENTS) : merged;
        });
      });
    });

    // Accumulate new events from WebSocket
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this.events.update((prev) => {
        const next = [...prev, evt];
        return next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next;
      });
    });

    // Persist to localStorage (survives F5 and browser close)
    effect(() => {
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(this.events()));
      } catch {
        localStorage.removeItem(STORAGE_KEY);
      }
    });
  }
}
