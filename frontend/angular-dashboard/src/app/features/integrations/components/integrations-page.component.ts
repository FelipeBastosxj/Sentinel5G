import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RealtimeStatusComponent } from '../../../core/components/realtime-status.component';
import { IntegrationsStore } from '../state/integrations.store';

@Component({
  selector: 'es-integrations-page',
  standalone: true,
  imports: [CommonModule, RealtimeStatusComponent],
  providers: [IntegrationsStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-row">
      <h1>Integrations</h1>
      <es-realtime-status />
    </header>

    <div class="es-panel">
      <table class="es-table">
        <thead>
          <tr>
            <th>Source</th>
            <th>Success</th>
            <th>Errors</th>
            <th>Success rate</th>
            <th>Last event</th>
          </tr>
        </thead>
        <tbody>
          <tr *ngFor="let p of store.providers(); trackBy: trackBySource">
            <td><span class="es-tag">{{ p.source }}</span></td>
            <td class="es-mono">{{ p.successCount }}</td>
            <td class="es-mono">{{ p.errorCount }}</td>
            <td>
              <span
                class="es-tag"
                [class.es-tag--success]="p.successRate >= 0.95"
                [class.es-tag--warn]="p.successRate >= 0.8 && p.successRate < 0.95"
                [class.es-tag--error]="p.successRate < 0.8"
              >
                {{ (p.successRate * 100).toFixed(1) }}%
              </span>
            </td>
            <td class="es-mono">{{ p.lastSeen || '—' }}</td>
          </tr>
          <tr *ngIf="store.providers().length === 0">
            <td colspan="5" class="es-empty">
              No integration traffic observed yet. Send a webhook to
              <span class="es-mono">/integrations/&#123;provider&#125;/webhook</span>.
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  `,
  styles: [
    `
      .es-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
      h1 { margin: 0; font-size: 24px; }
      table.es-table { width: 100%; border-collapse: collapse; }
      .es-table th, .es-table td { padding: 8px 12px; border-bottom: 1px solid var(--es-border); text-align: left; }
      .es-table th { color: var(--es-text-dim); font-weight: 500; font-size: 12px; text-transform: uppercase; }
      .es-empty { color: var(--es-text-dim); text-align: center; padding: 24px !important; }
    `,
  ],
})
export class IntegrationsPageComponent {
  protected readonly store = inject(IntegrationsStore);
  protected trackBySource(_i: number, p: { source: string }): string {
    return p.source;
  }
}
