import { WebhookEvent } from '@telecom-webhook/contracts';
import { Injectable } from '@angular/core';

import { environment } from '../../../environments/environment';

/**
 * EventsApiService — queries the processing-service REST API for historical
 * webhook events stored in PostgreSQL.
 *
 * Returns full WebhookEvent objects (including raw headers + payload).
 * Used by feature stores to hydrate state on first load and to lazily load
 * full event details when a real-time summary needs to be expanded.
 */
@Injectable({ providedIn: 'root' })
export class EventsApiService {
  async fetchRecent(options?: {
    limit?: number;
    workspaceId?: string;
  }): Promise<WebhookEvent[]> {
    try {
      const params = new URLSearchParams();
      if (options?.limit) params.set('limit', String(options.limit));

      const url = options?.workspaceId
        ? `${environment.processingUrl}/events/workspace/${options.workspaceId}?${params}`
        : `${environment.processingUrl}/events/recent?${params}`;

      const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
      if (!res.ok) return [];
      return (await res.json()) as WebhookEvent[];
    } catch {
      return [];
    }
  }

  /**
   * Fetch a single event by ID. Used by the dashboard to lazily load full
   * headers + payload for events that arrived as real-time summaries
   * (WebSocket pushes a lightweight summary without these verbose fields).
   */
  async fetchById(id: string): Promise<WebhookEvent | null> {
    try {
      const res = await fetch(
        `${environment.processingUrl}/events/${encodeURIComponent(id)}`,
        { signal: AbortSignal.timeout(5000) },
      );
      if (!res.ok) return null;
      const data = (await res.json()) as WebhookEvent | null;
      return data ?? null;
    } catch {
      return null;
    }
  }
}
