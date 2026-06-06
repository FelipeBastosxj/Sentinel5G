import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { WebhookEventSummary } from '@telecom-webhook/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { WorkspaceSessionService } from '../../../core/services/workspace-session.service';

export interface ProviderHealth {
  readonly source: string;
  readonly successCount: number;
  readonly errorCount: number;
  readonly successRate: number;
  readonly lastSeen: string;
}

@Injectable()
export class IntegrationsStore {
  private readonly client = inject(RealtimeClient);
  private readonly ws     = inject(WorkspaceSessionService);

  private readonly events = signal<WebhookEventSummary[]>([]);

  readonly providers = computed<ProviderHealth[]>(() => {
    const map = new Map<string, { success: number; error: number; lastSeen: string }>();
    for (const evt of this.events()) {
      const key    = evt.provider;
      const bucket = map.get(key) ?? { success: 0, error: 0, lastSeen: '' };
      const isError = evt.eventType.toLowerCase().includes('fail') ||
                      evt.eventType.toLowerCase().includes('error') ||
                      evt.status?.toLowerCase().includes('fail') || false;
      if (isError) bucket.error += 1;
      else bucket.success += 1;
      const ts = evt.receivedAt instanceof Date
        ? evt.receivedAt.toISOString()
        : String(evt.receivedAt);
      if (ts > bucket.lastSeen) bucket.lastSeen = ts;
      map.set(key, bucket);
    }
    return Array.from(map.entries())
      .map(([source, b]) => {
        const total = b.success + b.error;
        return {
          source,
          successCount: b.success,
          errorCount: b.error,
          successRate: total === 0 ? 0 : b.success / total,
          lastSeen: b.lastSeen,
        };
      })
      .sort((a, b) => (b.successCount + b.errorCount) - (a.successCount + a.errorCount));
  });

  constructor() {
    effect(() => {
      const session = this.ws.session();
      if (!session) return;
      this.client.connect(session.workspaceId);
    });
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this.events.update((prev) => {
        const next = [...prev, evt];
        return next.length > 5000 ? next.slice(-5000) : next;
      });
    });
  }
}
