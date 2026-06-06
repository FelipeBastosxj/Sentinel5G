import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RealtimeStatusComponent } from '../../../core/components/realtime-status.component';
import { MetricsStore } from '../state/metrics.store';

@Component({
  selector: 'es-metrics-page',
  standalone: true,
  imports: [CommonModule, RealtimeStatusComponent],
  providers: [MetricsStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-row">
      <h1>Metrics</h1>
      <es-realtime-status />
    </header>

    <div class="es-grid">
      <section class="es-panel">
        <h2>Events per channel</h2>
        <ul class="es-bars">
          <li *ngFor="let b of store.perChannel()">
            <span class="es-bar__label">{{ b.channel }}</span>
            <span class="es-bar__bar" [style.width.%]="barWidth(b.count)"></span>
            <span class="es-bar__value es-mono">{{ b.count }}</span>
          </li>
          <li *ngIf="store.perChannel().length === 0" class="es-empty">No data yet.</li>
        </ul>
      </section>

      <section class="es-panel">
        <h2>Events per type</h2>
        <ul class="es-bars">
          <li *ngFor="let b of store.perEventType()">
            <span class="es-bar__label">{{ b.eventType }}</span>
            <span class="es-bar__bar" [style.width.%]="barWidth(b.count)"></span>
            <span class="es-bar__value es-mono">{{ b.count }}</span>
          </li>
          <li *ngIf="store.perEventType().length === 0" class="es-empty">No data yet.</li>
        </ul>
      </section>

      <section class="es-panel">
        <h2>Throughput per minute</h2>
        <ul class="es-bars">
          <li *ngFor="let b of store.perMinute()">
            <span class="es-bar__label es-mono">{{ b.minute }}</span>
            <span class="es-bar__bar" [style.width.%]="barWidth(b.count)"></span>
            <span class="es-bar__value es-mono">{{ b.count }}</span>
          </li>
          <li *ngIf="store.perMinute().length === 0" class="es-empty">No data yet.</li>
        </ul>
      </section>
    </div>
  `,
  styles: [
    `
      .es-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
      h1 { margin: 0; font-size: 24px; }
      h2 { margin: 0 0 12px; font-size: 14px; color: var(--es-text-dim); }
      .es-grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); }
      ul.es-bars { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 6px; }
      .es-bars li { display: grid; grid-template-columns: 140px 1fr 60px; align-items: center; gap: 12px; }
      .es-bar__bar { display: inline-block; height: 8px; background: var(--es-accent); border-radius: 999px; min-width: 4px; }
      .es-bar__value { text-align: right; }
      .es-empty { display: block; color: var(--es-text-dim); padding: 12px 0; }
    `,
  ],
})
export class MetricsPageComponent {
  protected readonly store = inject(MetricsStore);

  protected barWidth(count: number): number {
    const total = this.store.total();
    if (total === 0) return 0;
    return Math.min(100, Math.round((count / Math.max(total, 1)) * 100));
  }
}
