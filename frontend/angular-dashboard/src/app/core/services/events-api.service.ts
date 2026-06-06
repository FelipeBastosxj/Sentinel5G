import { Injectable } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { environment } from '../../../environments/environment';

/**
 * EventsApiService — queries the processing-service REST API for historical
 * events stored in ClickHouse.
 *
 * Called once on store init to hydrate the dashboard with events that arrived
 * while the browser was closed (HARDNESS §3 — persistent state in ClickHouse).
 */
@Injectable({ providedIn: 'root' })
export class EventsApiService {
  async fetchRecent(options?: {
    limit?: number;
    channel?: string;
    source?: string;
  }): Promise<CanonicalEvent[]> {
    try {
      const params = new URLSearchParams();
      if (options?.limit)   params.set('limit',   String(options.limit));
      if (options?.channel) params.set('channel', options.channel);
      if (options?.source)  params.set('source',  options.source);

      const url = `${environment.processingUrl}/events/recent?${params}`;
      const res = await fetch(url, { signal: AbortSignal.timeout(5000) });

      if (!res.ok) return [];
      return (await res.json()) as CanonicalEvent[];
    } catch {
      // ClickHouse not available or network error — silent fallback to localStorage
      return [];
    }
  }
}
