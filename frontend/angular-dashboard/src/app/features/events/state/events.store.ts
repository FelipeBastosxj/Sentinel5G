import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEvent, WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventsApiService } from '../../../core/services/events-api.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';

export interface CountSlice {
  readonly key: string;
  readonly count: number;
}

export interface MinuteBucket {
  readonly minute: string;          // 'HH:mm' (local)
  readonly isoMinute: string;       // full ISO bucket for sorting
  readonly count: number;
}

export interface StatusRing {
  readonly success: number;         // delivered, sent, received, completed, in-progress, ringing
  readonly inFlight: number;        // queued, scheduled, sending, accepted, initiated
  readonly failure: number;         // undelivered, failed, busy, no-answer, canceled
  readonly other: number;
}

const STORAGE_KEY = 'es:metrics:events:v2';
const MAX_EVENTS  = 5000;

function loadFromStorage(): WebhookEventSummary[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as WebhookEventSummary[]) : [];
  } catch {
    return [];
  }
}

/** True for events that arrived through a recognised provider. */
function isKnown(e: { provider: string }): boolean {
  return e.provider !== 'unknown';
}

/**
 * EventsStore — derives aggregated metrics from the unified event stream.
 *
 * This store powers the Events page, which is itself a multi-chart metrics
 * dashboard (ECharts). It hydrates from PostgreSQL on first load, then
 * appends live events from the WebSocket gateway. Events with
 * `provider === 'unknown'` are excluded from every aggregation.
 */
@Injectable()
export class EventsStore {
  private readonly client = inject(RealtimeClient);
  private readonly api    = inject(EventsApiService);
  private readonly ws     = inject(WorkspaceSessionService);

  private readonly _all = signal<WebhookEventSummary[]>(loadFromStorage());

  /** Filtered stream — only events from recognised providers. */
  private readonly _events = computed(() => this._all().filter(isKnown));

  // ── Headline stats ────────────────────────────────────────────────────
  readonly total          = computed(() => this._events().length);
  readonly distinctTypes  = computed(() => new Set(this._events().map((e) => e.eventType)).size);
  readonly distinctRoutes = computed(() => new Set(this._events().map((e) => `${e.from ?? ''}->${e.to ?? ''}`)).size);

  // ── Aggregations ──────────────────────────────────────────────────────
  readonly perProvider = computed<CountSlice[]>(() =>
    this.groupBy(this._events(), (e) => e.provider),
  );

  readonly perChannel = computed<CountSlice[]>(() =>
    this.groupBy(this._events(), (e) => e.channel ?? 'other'),
  );

  readonly perEventType = computed<CountSlice[]>(() =>
    this.groupBy(this._events(), (e) => e.eventType),
  );

  readonly perStatus = computed<CountSlice[]>(() =>
    this.groupBy(
      this._events().filter((e) => !!e.status),
      (e) => e.status as string,
    ),
  );

  readonly statusRing = computed<StatusRing>(() => {
    let success = 0;
    let inFlight = 0;
    let failure = 0;
    let other = 0;
    for (const e of this._events()) {
      const s = (e.status ?? '').toLowerCase();
      if (!s)                                                                                            { other++;     continue; }
      if (['delivered', 'sent', 'received', 'completed', 'in-progress', 'ringing', 'read'].includes(s))  success++;
      else if (['queued', 'scheduled', 'sending', 'accepted', 'initiated'].includes(s))                  inFlight++;
      else if (['undelivered', 'failed', 'busy', 'no-answer', 'canceled'].includes(s))                   failure++;
      else                                                                                                other++;
    }
    return { success, inFlight, failure, other };
  });

  /** Top N source numbers (whoever is sending us the most webhooks). */
  readonly topSources = computed<CountSlice[]>(() =>
    this.groupBy(
      this._events().filter((e) => !!e.from),
      (e) => e.from as string,
    ).slice(0, 8),
  );

  /** Per-minute throughput for the last 30 minutes. */
  readonly perMinute = computed<MinuteBucket[]>(() => {
    const buckets = new Map<string, MinuteBucket>();
    for (const evt of this._events()) {
      const d = new Date(evt.receivedAt instanceof Date ? evt.receivedAt : String(evt.receivedAt));
      d.setSeconds(0, 0);
      const isoMinute = d.toISOString();
      const minute    = d.toTimeString().slice(0, 5);
      const cur       = buckets.get(isoMinute);
      buckets.set(isoMinute, {
        minute,
        isoMinute,
        count: (cur?.count ?? 0) + 1,
      });
    }
    return Array.from(buckets.values())
      .sort((a, b) => a.isoMinute.localeCompare(b.isoMinute))
      .slice(-30);
  });

  /** Stacked throughput per minute, split by channel. */
  readonly perMinuteByChannel = computed<{ minutes: string[]; series: { name: string; data: number[] }[] }>(() => {
    const channelOrder = ['sms', 'whatsapp', 'voice', 'other'];
    const matrix       = new Map<string, Map<string, number>>(); // isoMinute → channel → count
    for (const evt of this._events()) {
      const d = new Date(evt.receivedAt instanceof Date ? evt.receivedAt : String(evt.receivedAt));
      d.setSeconds(0, 0);
      const isoMinute = d.toISOString();
      const channel   = evt.channel ?? 'other';
      const row       = matrix.get(isoMinute) ?? new Map<string, number>();
      row.set(channel, (row.get(channel) ?? 0) + 1);
      matrix.set(isoMinute, row);
    }
    const isoMinutes = Array.from(matrix.keys()).sort().slice(-30);
    const minutes    = isoMinutes.map((iso) => new Date(iso).toTimeString().slice(0, 5));
    const series     = channelOrder.map((channel) => ({
      name: channel,
      data: isoMinutes.map((iso) => matrix.get(iso)?.get(channel) ?? 0),
    }));
    return { minutes, series };
  });

  readonly status = this.client.status;

  constructor() {
    // Connect once workspace is ready
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
      this.api.fetchRecent({ limit: 500, workspaceId: session.workspaceId }).then((historical: WebhookEvent[]) => {
        if (historical.length === 0) return;
        this._all.update((current) => {
          const existingIds = new Set(current.map((e) => e.id));
          const incoming = historical
            .filter((e) => !existingIds.has(e.id))
            .map((e) => this.toSummary(e));
          const merged = [...incoming, ...current];
          return merged.length > MAX_EVENTS ? merged.slice(-MAX_EVENTS) : merged;
        });
      });
    });

    // Accumulate new events from WebSocket
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this._all.update((prev) => {
        const next = [...prev, evt];
        return next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next;
      });
    });

    // Persist to localStorage (survives F5 and browser close)
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

  private groupBy<T>(items: readonly T[], key: (item: T) => string): CountSlice[] {
    const map = new Map<string, number>();
    for (const it of items) {
      const k = key(it);
      map.set(k, (map.get(k) ?? 0) + 1);
    }
    return Array.from(map.entries())
      .map(([k, count]) => ({ key: k, count }))
      .sort((a, b) => b.count - a.count);
  }
}
