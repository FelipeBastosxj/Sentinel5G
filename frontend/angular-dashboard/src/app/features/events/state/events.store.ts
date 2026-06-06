import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventsApiService } from '../../../core/services/events-api.service';
import { EventRow, toEventRow } from '../../../core/models/event-row.model';

const STORAGE_KEY = 'es:events:all';
const MAX_EVENTS  = 1000;

function loadFromStorage(): EventRow[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as EventRow[]) : [];
  } catch {
    return [];
  }
}

/**
 * EventsStore — owns the live event stream view-model.
 *
 * On init: hydrates from ClickHouse (events missed while browser was closed)
 * then merges with localStorage cache and subscribes to new WebSocket events.
 */
@Injectable()
export class EventsStore {
  private readonly client = inject(RealtimeClient);
  private readonly api    = inject(EventsApiService);

  private readonly all = signal<EventRow[]>(loadFromStorage());
  readonly channelFilter = signal<string>('all');
  readonly sourceFilter  = signal<string>('all');

  readonly channels = computed(() =>
    Array.from(new Set(this.all().map((e) => e.channel))).sort(),
  );
  readonly sources = computed(() =>
    Array.from(new Set(this.all().map((e) => e.source))).sort(),
  );

  readonly filtered = computed<EventRow[]>(() => {
    const ch  = this.channelFilter();
    const src = this.sourceFilter();
    return this.all().filter(
      (r) => (ch === 'all' || r.channel === ch) && (src === 'all' || r.source === src),
    );
  });

  readonly status = this.client.status;

  constructor() {
    this.client.connect();

    // Hydrate from ClickHouse — events that arrived while browser was closed
    this.api.fetchRecent({ limit: 200 }).then((historical: CanonicalEvent[]) => {
      if (historical.length === 0) return;
      this.all.update((current) => {
        const existingIds = new Set(current.map((r) => r.eventId));
        const incoming    = historical
          .filter((e) => !existingIds.has(e.eventId))
          .map(toEventRow);
        const merged = [...incoming, ...current];
        return merged.length > MAX_EVENTS ? merged.slice(-MAX_EVENTS) : merged;
      });
    });

    // Accumulate new events from WebSocket
    effect(() => {
      const evt: CanonicalEvent | null = this.client.lastEvent();
      if (!evt) return;
      this.all.update((prev) => {
        const next = [...prev, toEventRow(evt)];
        return next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next;
      });
    });

    // Persist to localStorage (survives F5 and browser close)
    effect(() => {
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(this.all()));
      } catch {
        localStorage.removeItem(STORAGE_KEY);
      }
    });
  }

  setChannel(value: string): void { this.channelFilter.set(value); }
  setSource(value: string):  void { this.sourceFilter.set(value);  }
}
