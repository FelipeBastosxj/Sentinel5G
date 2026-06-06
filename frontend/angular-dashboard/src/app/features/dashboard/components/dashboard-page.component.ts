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
          <div class="es-stat__value es-mono">{{ store.distinctProviders() }}</div>
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
              <th>Provider</th>
              <th>Event Type</th>
              <th>From</th>
              <th>To</th>
              <th>Status</th>
              <th>Received</th>
            </tr>
          </thead>
          <tbody>
            <tr *ngFor="let evt of store.events(); trackBy: trackById">
              <td><span class="es-tag">{{ evt.provider }}</span></td>
              <td>{{ evt.eventType }}</td>
              <td class="es-mono">{{ evt.from ?? '—' }}</td>
              <td class="es-mono">{{ evt.to ?? '—' }}</td>
              <td>{{ evt.status ?? '—' }}</td>
              <td class="es-mono">{{ evt.receivedAt | date:'HH:mm:ss' }}</td>
            </tr>
            <tr *ngIf="store.events().length === 0">
              <td colspan="6" class="es-empty">Waiting for events…</td>
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
  protected trackById(_i: number, item: { id: string }): string {
    return item.id;
  }
}
