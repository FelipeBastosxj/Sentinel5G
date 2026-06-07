import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEvent, WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventsApiService } from '../../../core/services/events-api.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';

export interface IntegrationHealth {
  readonly provider: string;
  readonly channels: readonly string[]; // ['sms','whatsapp','voice']
  readonly successCount: number;
  readonly inFlightCount: number;
  readonly errorCount: number;
  readonly successRate: number;         // 0..1, calculated from success vs error only
  readonly totalEvents: number;
  readonly lastSeen: string;            // ISO timestamp
}

const SUCCESS_STATUSES  = ['delivered', 'sent', 'received', 'completed', 'in-progress', 'ringing', 'read'];
const IN_FLIGHT_STATUSES = ['queued', 'scheduled', 'sending', 'accepted', 'initiated'];
const ERROR_STATUSES    = ['undelivered', 'failed', 'busy', 'no-answer', 'canceled'];

const STORAGE_KEY = 'es:integrations:events:v1';
const MAX_EVENTS  = 5000;

function loadFromStorage(): WebhookEventSummary[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as WebhookEventSummary[]) : [];
  } catch {
    return [];
  }
}

/**
 * IntegrationsStore — health summary per provider integration.
 *
 * Hydrates from PostgreSQL (last 500 events) on first load, then keeps
 * appending live events from the WebSocket gateway. Events with
 * `provider === 'unknown'` are excluded — they represent ingress probes,
 * not real integration traffic.
 */
@Injectable()
export class IntegrationsStore {
  private readonly client = inject(RealtimeClient);
  private readonly ws     = inject(WorkspaceSessionService);
  private readonly api    = inject(EventsApiService);

  private readonly _all = signal<WebhookEventSummary[]>(loadFromStorage());

  private readonly _events = computed(() =>
    this._all().filter((e) => e.provider !== 'unknown'),
  );

  readonly total = computed(() => this._events().length);

  readonly providers = computed<IntegrationHealth[]>(() => {
    const map = new Map<string, {
      success: number;
      inFlight: number;
      error: number;
      total: number;
      channels: Set<string>;
      lastSeen: string;
    }>();

    for (const evt of this._events()) {
      const key    = evt.provider;
      const bucket = map.get(key) ?? {
        success: 0, inFlight: 0, error: 0, total: 0,
        channels: new Set<string>(), lastSeen: '',
      };

      bucket.total += 1;
      if (evt.channel) bucket.channels.add(evt.channel);

      const status = (evt.status ?? '').toLowerCase();
      const typeBased =
        evt.eventType.toLowerCase().includes('fail')  ||
        evt.eventType.toLowerCase().includes('error') ||
        evt.eventType.toLowerCase().includes('undelivered');

      if (typeBased || ERROR_STATUSES.includes(status)) bucket.error    += 1;
      else if (SUCCESS_STATUSES.includes(status))       bucket.success  += 1;
      else if (IN_FLIGHT_STATUSES.includes(status))     bucket.inFlight += 1;
      else                                              bucket.success  += 1; // be optimistic for inbound msgs etc.

      const ts = evt.receivedAt instanceof Date
        ? evt.receivedAt.toISOString()
        : String(evt.receivedAt);
      if (ts > bucket.lastSeen) bucket.lastSeen = ts;

      map.set(key, bucket);
    }

    return Array.from(map.entries())
      .map(([provider, b]) => {
        const decisive = b.success + b.error;
        return {
          provider,
          channels:      Array.from(b.channels).sort(),
          successCount:  b.success,
          inFlightCount: b.inFlight,
          errorCount:    b.error,
          successRate:   decisive === 0 ? 1 : b.success / decisive,
          totalEvents:   b.total,
          lastSeen:      b.lastSeen,
        };
      })
      .sort((a, b) => b.totalEvents - a.totalEvents);
  });

  constructor() {
    // Connect + hydrate
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
      this.api.fetchRecent({ limit: 500, workspaceId: session.workspaceId }).then((historical: WebhookEvent[]) => {
        if (historical.length === 0) return;
        this._all.update((current) => {
          const existing = new Set(current.map((e) => e.id));
          const incoming = historical
            .filter((e) => !existing.has(e.id))
            .map((e) => this.toSummary(e));
          const merged = [...incoming, ...current];
          return merged.length > MAX_EVENTS ? merged.slice(-MAX_EVENTS) : merged;
        });
      });
    });

    // Append realtime events
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this._all.update((prev) => {
        const next = [...prev, evt];
        return next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next;
      });
    });

    // Persist
    effect(() => {
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(this._all()));
      } catch {
        localStorage.removeItem(STORAGE_KEY);
      }
    });
  }

  private toSummary(e: WebhookEvent): WebhookEventSummary {
    return {
      id:           e.id,
      workspaceId:  e.workspaceId,
      provider:     e.provider,
      eventType:    e.eventType,
      channel:      e.channel,
      receivedAt:   e.receivedAt,
      messageSid:   e.messageSid,
      callSid:      e.callSid,
      from:         e.from,
      to:           e.to,
      status:       e.status,
      processingMs: e.processingMs,
    };
  }
}
