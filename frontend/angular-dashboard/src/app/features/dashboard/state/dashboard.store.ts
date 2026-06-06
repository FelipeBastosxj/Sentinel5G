import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';
import { EventsApiService } from '../../../core/services/events-api.service';

/**
 * DashboardStore — feature-scoped state for the live events dashboard.
 *
 * Connects to the realtime gateway only after the workspace session is ready,
 * then hydrates with recent historical events from the REST API.
 */
@Injectable()
export class DashboardStore {
  private readonly client  = inject(RealtimeClient);
  private readonly ws      = inject(WorkspaceSessionService);
  private readonly api     = inject(EventsApiService);

  private readonly _events = signal<WebhookEventSummary[]>([]);

  readonly events         = computed(() => this._events().slice().reverse());
  readonly totalEvents    = computed(() => this._events().length);
  readonly distinctProviders = computed(
    () => new Set(this._events().map((e) => e.provider)).size,
  );
  readonly status         = this.client.status;

  constructor() {
    // Connect once workspace session is ready
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
      // Hydrate with historical events
      void this.api
        .fetchRecent({ limit: 50, workspaceId: session.workspaceId })
        .then((rows) => this._events.set(rows));
    });

    // Append incoming real-time events
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      const session = this.ws.session();
      if (session && evt.workspaceId !== session.workspaceId) return;
      this._events.update((prev) => [...prev.slice(-499), evt]);
    });
  }
}
