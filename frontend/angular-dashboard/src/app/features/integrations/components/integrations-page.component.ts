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
            <th>Provider</th>
            <th>Channels</th>
            <th>Total</th>
            <th>Success</th>
            <th>In-flight</th>
            <th>Errors</th>
            <th>Success rate</th>
            <th>Last event</th>
          </tr>
        </thead>
        <tbody>
          <tr *ngFor="let p of store.providers(); trackBy: trackByProvider">
            <td><span class="es-tag">{{ p.provider }}</span></td>
            <td>
              <ng-container *ngIf="p.channels.length > 0; else noChannel">
                <span
                  *ngFor="let c of p.channels"
                  class="es-tag es-tag--inline"
                  [class.es-tag--sms]="c === 'sms'"
                  [class.es-tag--whatsapp]="c === 'whatsapp'"
                  [class.es-tag--voice]="c === 'voice'"
                >
                  {{ channelLabel(c) }}
                </span>
              </ng-container>
              <ng-template #noChannel><span class="es-mono">—</span></ng-template>
            </td>
            <td class="es-mono">{{ p.totalEvents }}</td>
            <td class="es-mono">{{ p.successCount }}</td>
            <td class="es-mono">{{ p.inFlightCount }}</td>
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
            <td class="es-mono">{{ p.lastSeen ? (p.lastSeen | date:'HH:mm:ss') : '—' }}</td>
          </tr>
          <tr *ngIf="store.providers().length === 0">
            <td colspan="8" class="es-empty">
              No integration traffic observed yet. Send a webhook to
              <span class="es-mono">/&#123;workspaceId&#125;/&#123;endpointToken&#125;</span>
              to populate this view.
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
      .es-table th, .es-table td { padding: 8px 12px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: middle; }
      .es-table th { color: var(--es-text-dim); font-weight: 500; font-size: 12px; text-transform: uppercase; }
      .es-tag--inline   { margin-right: 4px; }
      .es-tag--sms      { background: rgba(47, 129, 247, 0.18); color: #58a6ff; }
      .es-tag--whatsapp { background: rgba(63, 185, 80, 0.18);  color: #3fb950; }
      .es-tag--voice    { background: rgba(210, 153, 34, 0.18); color: #d2a014; }
      .es-empty { color: var(--es-text-dim); text-align: center; padding: 24px !important; }
    `,
  ],
})
export class IntegrationsPageComponent {
  protected readonly store = inject(IntegrationsStore);
  protected trackByProvider(_i: number, p: { provider: string }): string {
    return p.provider;
  }

  protected channelLabel(channel: string): string {
    switch (channel) {
      case 'sms':      return 'SMS';
      case 'whatsapp': return 'WhatsApp';
      case 'voice':    return 'Voice';
      default:         return channel;
    }
  }
}
