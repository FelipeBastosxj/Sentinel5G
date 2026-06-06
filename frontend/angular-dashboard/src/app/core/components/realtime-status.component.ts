import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
} from '@angular/core';
import { RealtimeClient } from '../services/realtime-client.service';

/**
 * Tiny status pill that reflects the realtime connection state.
 * Used in feature pages to keep the user informed of the live link.
 */
@Component({
  selector: 'es-realtime-status',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span
      class="es-tag"
      [class.es-tag--success]="status() === 'connected'"
      [class.es-tag--warn]="status() === 'connecting'"
      [class.es-tag--error]="status() === 'disconnected'"
    >
      {{ label() }}
    </span>
  `,
})
export class RealtimeStatusComponent {
  private readonly client = inject(RealtimeClient);
  protected readonly status = this.client.status;
  protected readonly label = computed(() => `realtime: ${this.status()}`);
}
