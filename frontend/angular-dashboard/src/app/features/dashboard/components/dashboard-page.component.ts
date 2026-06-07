import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
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
          <div class="es-stat__label">Distinct providers</div>
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
              <th></th>
              <th>Provider</th>
              <th>Channel</th>
              <th>Event Type</th>
              <th>Direction</th>
              <th>From</th>
              <th>To</th>
              <th>SID</th>
              <th>Status</th>
              <th>Received</th>
            </tr>
          </thead>
          <tbody>
            <ng-container *ngFor="let evt of store.events(); trackBy: trackById">
              <tr
                class="es-row-clickable"
                [class.es-row-active]="expandedId() === evt.id"
                (click)="toggle(evt)"
              >
                <td class="es-mono es-chevron">{{ expandedId() === evt.id ? '▾' : '▸' }}</td>
                <td>
                  <span class="es-tag">{{ evt.provider }}</span>
                </td>
                <td>
                  <span
                    class="es-tag"
                    [class.es-tag--sms]="evt.channel === 'sms'"
                    [class.es-tag--whatsapp]="evt.channel === 'whatsapp'"
                    [class.es-tag--voice]="evt.channel === 'voice'"
                  >
                    {{ channelLabel(evt.channel) }}
                  </span>
                </td>
                <td>{{ evt.eventType }}</td>
                <td>
                  <span class="es-dir">{{ directionOf(evt.eventType) }}</span>
                </td>
                <td class="es-mono">{{ evt.from ?? '—' }}</td>
                <td class="es-mono">{{ evt.to ?? '—' }}</td>
                <td class="es-mono es-truncate">{{ evt.messageSid ?? evt.callSid ?? '—' }}</td>
                <td>{{ evt.status ?? '—' }}</td>
                <td class="es-mono">{{ evt.receivedAt | date:'HH:mm:ss' }}</td>
              </tr>
              <tr *ngIf="expandedId() === evt.id" class="es-row-details">
                <td colspan="10">
                  <div class="es-details">
                    <div class="es-details__col">
                      <h3>Payload</h3>
                      <pre class="es-json">{{ pretty(evt.payload) }}</pre>
                    </div>
                    <div class="es-details__col">
                      <h3>Headers</h3>
                      <pre class="es-json">{{ pretty(evt.headers) }}</pre>
                    </div>
                  </div>
                </td>
              </tr>
            </ng-container>
            <tr *ngIf="store.events().length === 0">
              <td colspan="10" class="es-empty">Waiting for events…</td>
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
      h3 { margin: 0 0 8px; font-size: 12px; color: var(--es-text-dim); text-transform: uppercase; letter-spacing: 0.06em; }
      .es-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; }
      .es-stat__label { color: var(--es-text-dim); font-size: 13px; }
      .es-stat__value { font-size: 28px; margin-top: 4px; }
      table.es-table { width: 100%; border-collapse: collapse; }
      .es-table th, .es-table td { padding: 8px 12px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: middle; }
      .es-table th { color: var(--es-text-dim); font-weight: 500; font-size: 12px; text-transform: uppercase; }
      .es-row-clickable { cursor: pointer; }
      .es-row-clickable:hover { background: rgba(255, 255, 255, 0.03); }
      .es-row-active { background: rgba(255, 255, 255, 0.05); }
      .es-row-details td { background: rgba(0, 0, 0, 0.2); padding: 16px; }
      .es-chevron { width: 20px; color: var(--es-text-dim); }
      .es-dir { font-size: 12px; color: var(--es-text-dim); text-transform: uppercase; letter-spacing: 0.05em; }
      .es-truncate { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .es-details { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
      .es-json {
        background: rgba(0, 0, 0, 0.3);
        border: 1px solid var(--es-border);
        border-radius: 4px;
        padding: 12px;
        margin: 0;
        font-size: 12px;
        line-height: 1.5;
        max-height: 320px;
        overflow: auto;
        white-space: pre-wrap;
        word-break: break-word;
      }
      .es-tag--sms      { background: rgba(47, 129, 247, 0.18);  color: #58a6ff; }
      .es-tag--whatsapp { background: rgba(63, 185, 80, 0.18);   color: #3fb950; }
      .es-tag--voice    { background: rgba(210, 153, 34, 0.18);  color: #d2a014; }
      .es-empty { color: var(--es-text-dim); text-align: center; padding: 24px !important; }
      @media (max-width: 800px) {
        .es-details { grid-template-columns: 1fr; }
      }
    `,
  ],
})
export class DashboardPageComponent {
  protected readonly store = inject(DashboardStore);
  protected readonly expandedId = signal<string | null>(null);

  protected trackById(_i: number, item: { id: string }): string {
    return item.id;
  }

  /**
   * Toggle the detail row for the given event.
   *
   * When expanding a row whose payload is empty (it came in as a real-time
   * summary over WebSocket), we kick off a lazy fetch so the detail panel
   * fills in within ~100 ms without blocking the open animation.
   */
  protected toggle(evt: { id: string; payload: unknown }): void {
    const opening = this.expandedId() !== evt.id;
    this.expandedId.set(opening ? evt.id : null);

    if (opening) {
      const isEmpty =
        !evt.payload ||
        typeof evt.payload !== 'object' ||
        Object.keys(evt.payload as object).length === 0;
      if (isEmpty) {
        void this.store.fetchFull(evt.id);
      }
    }
  }

  protected channelLabel(channel: string | undefined): string {
    switch (channel) {
      case 'sms':      return 'SMS';
      case 'whatsapp': return 'WhatsApp';
      case 'voice':    return 'Voice';
      default:         return channel ?? '—';
    }
  }

  /** Infer call/message direction from the event type. */
  protected directionOf(eventType: string): string {
    if (eventType === 'message.inbound' || eventType === 'call.inbound') return 'inbound';
    if (eventType === 'call.outbound')                                   return 'outbound';
    if (eventType.startsWith('message.status.'))                         return 'outbound';
    if (eventType.startsWith('call.status.'))                            return '—';
    return '—';
  }

  protected pretty(obj: unknown): string {
    if (!obj || (typeof obj === 'object' && Object.keys(obj as object).length === 0)) {
      return '(loading…)';
    }
    try {
      return JSON.stringify(obj, null, 2);
    } catch {
      return String(obj);
    }
  }
}
