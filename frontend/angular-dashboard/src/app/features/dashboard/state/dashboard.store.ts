import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';

/**
 * Feature-scoped state for the dashboard page.
 *
 * Owned by the feature, never imported elsewhere (HARDNESS §7 — no global
 * stores, no shared mutable state).
 */
@Injectable()
export class DashboardStore {
  private readonly client = inject(RealtimeClient);

  private readonly window = signal<CanonicalEvent[]>([]);
  private readonly metricsWindow = signal<CanonicalEvent[]>([]);

  /** Total CanonicalEvents observed since the page was opened. */
  readonly totalEvents = computed(() => this.window().length);
  /** Distinct providers seen on this session. */
  readonly distinctSources = computed(
    () => new Set(this.window().map((e) => String(e.source))).size,
  );
  /** Last 50 events (latest first). */
  readonly recentEvents = computed(() => this.window().slice(-50).reverse());
  /** Last 50 metric events. */
  readonly recentMetrics = computed(() => this.metricsWindow().slice(-50).reverse());
  /** Pass-through of connection status from RealtimeClient. */
  readonly status = this.client.status;

  constructor() {
    this.client.connect();

    // Append every new event into the dashboard window.
    effect(() => {
      const evt = this.client.lastEvent();
      if (!evt) return;
      this.window.update((prev) => [...prev.slice(-499), evt]);
    });
    effect(() => {
      const m = this.client.lastMetric();
      if (!m) return;
      this.metricsWindow.update((prev) => [...prev.slice(-499), m]);
    });
  }
}
