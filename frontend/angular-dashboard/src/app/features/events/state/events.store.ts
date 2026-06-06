import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventsApiService } from '../../../core/services/events-api.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';
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
 * On init: hydrates from PostgreSQL (events missed while browser was closed)
 * then merges with localStorage cache and subscribes to new WebSocket events.
 */
@Injectable()
export class EventsStore {
  private readonly client = inject(RealtimeClient);
  private readonly api    = inject(EventsApiService);
  private readonly ws     = inject(WorkspaceSessionService);

  private readonly all = signal<EventRow[]>(loadFromStorage());
  readonly providerFilter = signal<string>('all');
  readonly typeFilter     = signal<string>('all');

  readonly providers = computed(() =>
    Array.from(new Set(this.all().map((e) => e.provider))).sort(),
  );
  readonly eventTypes = computed(() =>
    Array.from(new Set(this.all().map((e) => e.eventType))).sort(),
  );

  readonly filtered = computed<EventRow[]>(() => {
    const prov = this.providerFilter();
    const type = this.typeFilter();
    return this.all().filter(
      (r) => (prov === 'all' || r.provider === prov) && (type === 'all' || r.eventType === type),
    );
  });

  readonly status = this.client.status;

  // Keep backwards-compat aliases used by the filter dropdowns
  readonly channelFilter = this.providerFilter;
  readonly sourceFilter  = this.typeFilter;
  readonly channels      = this.providers;
  readonly sources       = this.eventTypes;

  constructor() {
    // Connect once workspace is ready
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
      this.api.fetchRecent({ limit: 200, workspaceId: session.workspaceId }).then((historical: WebhookEventSummary[]) => {
        if (historical.length === 0) return;
        this.all.update((current) => {
          const existingIds = new Set(current.map((r) => r.id));
          const incoming    = historical.filter((e) => !existingIds.has(e.id)).map(toEventRow);
          const merged = [...incoming, ...current];
          return merged.length > MAX_EVENTS ? merged.slice(-MAX_EVENTS) : merged;
        });
      });
    });

    // Accumulate new events from WebSocket
    effect(() => {
      const evt: WebhookEventSummary | null = this.client.lastEvent();
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

  setChannel(value: string): void { this.providerFilter.set(value); }
  setSource(value: string):  void { this.typeFilter.set(value); }
}
