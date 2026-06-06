import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { RealtimeStatusComponent } from '../../../core/components/realtime-status.component';
import { EventsStore } from '../state/events.store';

@Component({
  selector: 'es-events-page',
  standalone: true,
  imports: [CommonModule, FormsModule, RealtimeStatusComponent],
  providers: [EventsStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-row">
      <h1>Events</h1>
      <es-realtime-status />
    </header>

    <div class="es-filters es-panel">
      <label>
        Channel
        <select
          [ngModel]="store.channelFilter()"
          (ngModelChange)="store.setChannel($event)"
        >
          <option value="all">all</option>
          <option *ngFor="let c of store.channels()" [value]="c">{{ c }}</option>
        </select>
      </label>
      <label>
        Source
        <select
          [ngModel]="store.sourceFilter()"
          (ngModelChange)="store.setSource($event)"
        >
          <option value="all">all</option>
          <option *ngFor="let s of store.sources()" [value]="s">{{ s }}</option>
        </select>
      </label>
      <span class="es-counter">
        {{ store.filtered().length }} matching
      </span>
    </div>

    <div class="es-panel">
      <table class="es-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Provider</th>
            <th>Type</th>
            <th>From</th>
            <th>To</th>
            <th>Status</th>
            <th>Received</th>
          </tr>
        </thead>
        <tbody>
          <tr *ngFor="let r of store.filtered(); trackBy: trackById">
            <td class="es-mono">{{ r.id | slice:0:8 }}…</td>
            <td><span class="es-tag">{{ r.provider }}</span></td>
            <td>{{ r.eventType }}</td>
            <td class="es-mono">{{ r.from }}</td>
            <td class="es-mono">{{ r.to }}</td>
            <td>{{ r.status }}</td>
            <td class="es-mono">{{ r.receivedAt | date:'HH:mm:ss' }}</td>
          </tr>
          <tr *ngIf="store.filtered().length === 0">
            <td colspan="7" class="es-empty">No events match the filters yet.</td>
          </tr>
        </tbody>
      </table>
    </div>
  `,
  styles: [
    `
      .es-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
      h1 { margin: 0; font-size: 24px; }
      .es-filters { display: flex; gap: 16px; align-items: center; margin-bottom: 16px; }
      .es-filters label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--es-text-dim); }
      .es-filters select {
        padding: 6px 10px;
        background: var(--es-bg);
        color: var(--es-text);
        border: 1px solid var(--es-border);
        border-radius: 6px;
      }
      .es-counter { margin-left: auto; color: var(--es-text-dim); }
      table.es-table { width: 100%; border-collapse: collapse; }
      .es-table th, .es-table td { padding: 8px 12px; border-bottom: 1px solid var(--es-border); text-align: left; }
      .es-table th { color: var(--es-text-dim); font-weight: 500; font-size: 12px; text-transform: uppercase; }
      .es-empty { color: var(--es-text-dim); text-align: center; padding: 24px !important; }
    `,
  ],
})
export class EventsPageComponent {
  protected readonly store = inject(EventsStore);
  protected trackById(_i: number, r: { id: string }): string {
    return r.id;
  }
}
