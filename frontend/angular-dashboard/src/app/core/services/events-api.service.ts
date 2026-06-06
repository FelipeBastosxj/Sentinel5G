import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { Injectable } from '@angular/core';

import { environment } from '../../../environments/environment';

/**
 * EventsApiService — queries the processing-service REST API for historical
 * webhook events stored in PostgreSQL.
 *
 * Called once on store init to hydrate the dashboard with events that arrived
 * while the browser was closed.
 */
@Injectable({ providedIn: 'root' })
export class EventsApiService {
  async fetchRecent(options?: {
    limit?: number;
    workspaceId?: string;
  }): Promise<WebhookEventSummary[]> {
    try {
      const params = new URLSearchParams();
      if (options?.limit) params.set('limit', String(options.limit));

      const url = options?.workspaceId
        ? `${environment.processingUrl}/events/workspace/${options.workspaceId}?${params}`
        : `${environment.processingUrl}/events/recent?${params}`;

      const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
      if (!res.ok) return [];
      return (await res.json()) as WebhookEventSummary[];
    } catch {
      // Network error — silent fallback
      return [];
    }
  }
}
