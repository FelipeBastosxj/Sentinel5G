import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { CanonicalEvent } from '@eventstream/contracts';
import { RealtimeClient } from '../../../core/services/realtime-client.service';
import { EventRow, toEventRow } from '../../../core/models/event-row.model';

/**
 * EventsStore — owns the live event stream view-model.
 * Provides filter signals so the component can reactively narrow the list.
 */
@Injectable()
export class EventsStore {
  private readonly client = inject(RealtimeClient);

  private readonly all = signal<EventRow[]>([]);
  readonly channelFilter = signal<string>('all');
  readonly sourceFilter = signal<string>('all');

  readonly channels = computed(() =>
    Array.from(new Set(this.all().map((e) => e.channel))).sort(),
  );
  readonly sources = computed(() =>
    Array.from(new Set(this.all().map((e) => e.source))).sort(),
  );

  readonly filtered = computed<EventRow[]>(() => {
    const ch = this.channelFilter();
    const src = this.sourceFilter();
    return this.all().filter(
      (r) =>
        (ch === 'all' || r.channel === ch) &&
        (src === 'all' || r.source === src),
    );
  });

  readonly status = this.client.status;

  constructor() {
    this.client.connect();
    effect(() => {
      const evt: CanonicalEvent | null = this.client.lastEvent();
      if (!evt) return;
      this.all.update((prev) => {
        const next = [...prev, toEventRow(evt)];
        return next.length > 1000 ? next.slice(-1000) : next;
      });
    });
  }

  setChannel(value: string): void {
    this.channelFilter.set(value);
  }
  setSource(value: string): void {
    this.sourceFilter.set(value);
  }
}
