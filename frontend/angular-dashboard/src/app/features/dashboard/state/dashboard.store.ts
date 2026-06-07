import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEvent, WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';
import { EventsApiService } from '../../../core/services/events-api.service';

/**
 * DashboardStore — feature-scoped state for the live events dashboard.
 *
 * Historical events come from the REST API as full WebhookEvent objects
 * (with headers + payload). Real-time events arrive as WebhookEventSummary
 * over Socket.IO (no payload, to keep pg_notify under its 8KB limit) and
 * are merged in as partial events.
 *
 * Events whose provider could not be identified (`provider === 'unknown'`)
 * are filtered out of the dashboard UI by default — they are still persisted
 * and visible via direct DB queries, but they would only add noise to the
 * operator's view (typically empty health-check pings or proxy validations).
 */
@Injectable()
export class DashboardStore {
  private readonly client  = inject(RealtimeClient);
  private readonly ws      = inject(WorkspaceSessionService);
  private readonly api     = inject(EventsApiService);

  private readonly _events = signal<WebhookEvent[]>([]);

  /** Only events that came from a recognised provider. */
  private readonly _known = computed(() =>
    this._events().filter((e) => e.provider !== 'unknown'),
  );

  /** Most-recent-first, with `unknown` already filtered out. */
  readonly events            = computed(() => this._known().slice().reverse());
  readonly totalEvents       = computed(() => this._known().length);
  readonly distinctProviders = computed(
    () => new Set(this._known().map((e) => e.provider)).size,
  );
  readonly status            = this.client.status;

  constructor() {
    // Connect once workspace session is ready
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
      // Hydrate with full historical events (includes headers + payload)
      void this.api
        .fetchRecent({ limit: 50, workspaceId: session.workspaceId })
        .then((rows) => this._events.set(rows));
    });

    // Append incoming real-time events. The push message is a Summary
    // (no headers/payload), so we merge with empty placeholders. Clicking
    // the row will fetch the full event lazily via fetchFull().
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      const session = this.ws.session();
      if (session && evt.workspaceId !== session.workspaceId) return;
      const full = this.summaryToEvent(evt);
      this._events.update((prev) => [...prev.slice(-499), full]);
    });
  }

  /**
   * Lazily fetch the complete event (headers + payload) by ID and merge it
   * back into the `_events` signal.
   *
   * Called by the dashboard component when the user expands a row that
   * arrived as a real-time summary (payload is an empty object at that point).
   * No-op if the event already has payload data.
   */
  async fetchFull(id: string): Promise<void> {
    // Skip if we already have the full data
    const existing = this._events().find((e) => e.id === id);
    const isEmpty = (obj: unknown): boolean =>
      !obj || typeof obj !== 'object' || Object.keys(obj as object).length === 0;
    if (existing && !isEmpty(existing.payload)) return;

    const full = await this.api.fetchById(id);
    if (!full) return;

    this._events.update((prev) =>
      prev.map((e) => (e.id === id ? full : e)),
    );
  }

  private summaryToEvent(s: WebhookEventSummary): WebhookEvent {
    return {
      ...s,
      headers: {},
      payload: {},
    } as WebhookEvent;
  }
}
