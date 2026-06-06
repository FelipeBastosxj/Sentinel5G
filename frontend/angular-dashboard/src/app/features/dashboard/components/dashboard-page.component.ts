import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RealtimeStatusComponent } from '../../../core/components/realtime-status.component';
import { DashboardStore } from '../state/dashboard.store';

@Component({
  selector: 'es-dashboard-page',
  standalone: true,
  imports: [CommonModule, RealtimeStatusComponent],
  providers: [DashboardStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="es-grid">
      <header class="es-row">
        <h1>Dashboard</h1>
        <es-realtime-status />
      </header>

      <div class="es-stats">
        <div class="es-panel es-stat">
          <div class="es-stat__label">Events received</div>
          <div class="es-stat__value es-mono">{{ store.totalEvents() }}</div>
        </div>
        <div class="es-panel es-stat">
          <div class="es-stat__label">Distinct sources</div>
          <div class="es-stat__value es-mono">{{ store.distinctSources() }}</div>
        </div>
        <div class="es-panel es-stat">
          <div class="es-stat__label">Realtime status</div>
          <div class="es-stat__value es-mono">{{ store.status() }}</div>
        </div>
      </div>

      <div class="es-panel">
        <h2>Latest events</h2>
        <table class="es-table">
          <thead>
            <tr>
              <th>Event Type</th>
              <th>Channel</th>
              <th>Source</th>
              <th>Timestamp</th>
              <th>Correlation</th>
            </tr>
          </thead>
          <tbody>
            <tr *ngFor="let evt of store.recentEvents(); trackBy: trackById">
              <td>
                <span class="es-tag">{{ evt.eventType }}</span>
              </td>
              <td>{{ evt.channel }}</td>
              <td>{{ evt.source }}</td>
              <td class="es-mono">{{ evt.timestamp }}</td>
              <td class="es-mono es-tag">{{ evt.correlationId }}</td>
            </tr>
            <tr *ngIf="store.recentEvents().length === 0">
              <td colspan="5" class="es-empty">Waiting for events…</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  `,
  styles: [
    `
      .es-grid { display: flex; flex-direction: column; gap: 16px; }
      .es-row { display: flex; align-items: center; justify-content: space-between; }
      h1 { margin: 0; font-size: 24px; }
      h2 { margin: 0 0 12px; font-size: 16px; color: var(--es-text-dim); }
      .es-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; }
      .es-stat__label { color: var(--es-text-dim); font-size: 13px; }
      .es-stat__value { font-size: 28px; margin-top: 4px; }
      table.es-table { width: 100%; border-collapse: collapse; }
      .es-table th, .es-table td { padding: 8px 12px; border-bottom: 1px solid var(--es-border); text-align: left; }
      .es-table th { color: var(--es-text-dim); font-weight: 500; font-size: 12px; text-transform: uppercase; }
      .es-empty { color: var(--es-text-dim); text-align: center; padding: 24px !important; }
    `,
  ],
})
export class DashboardPageComponent {
  protected readonly store = inject(DashboardStore);
  protected trackById(_index: number, item: { eventId: string }): string {
    return item.eventId;
  }
}
