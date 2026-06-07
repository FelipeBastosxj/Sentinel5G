import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { NgxEchartsDirective } from 'ngx-echarts';
import type { EChartsOption } from 'echarts';
import { RealtimeStatusComponent } from '../../../core/components/realtime-status.component';
import { EventsStore } from '../state/events.store';

/**
 * Chart-style colour palette aligned with the rest of the app.
 * Channel colours match the tag colours used in the Dashboard table.
 */
const COLORS = {
  text:      '#c9d1d9',
  dim:       '#8b949e',
  grid:      '#2a2a3a',
  accent:    '#58a6ff',
  sms:       '#58a6ff',
  whatsapp:  '#3fb950',
  voice:     '#d2a014',
  other:     '#8b949e',
  success:   '#3fb950',
  inFlight:  '#58a6ff',
  failure:   '#f85149',
  palette: ['#58a6ff', '#3fb950', '#d2a014', '#f85149', '#a78bfa', '#00c8ff', '#ffd166', '#ff6b6b'],
};

function channelColor(channel: string): string {
  switch (channel) {
    case 'sms':      return COLORS.sms;
    case 'whatsapp': return COLORS.whatsapp;
    case 'voice':    return COLORS.voice;
    default:         return COLORS.other;
  }
}

/**
 * EventsPageComponent — multi-chart metrics dashboard powered by ECharts.
 *
 * The page is purely derivative: every chart's `[options]` input is a
 * `computed()` over the `EventsStore`, so when a new webhook arrives via
 * Socket.IO the store updates and all charts re-render automatically.
 */
@Component({
  selector: 'es-events-page',
  standalone: true,
  imports: [CommonModule, NgxEchartsDirective, RealtimeStatusComponent],
  providers: [EventsStore],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-row">
      <h1>Metrics</h1>
      <es-realtime-status />
    </header>

    <!-- Headline stats ─────────────────────────────────────────────── -->
    <div class="es-stats">
      <div class="es-panel es-stat">
        <div class="es-stat__label">Total events</div>
        <div class="es-stat__value es-mono">{{ store.total() }}</div>
      </div>
      <div class="es-panel es-stat">
        <div class="es-stat__label">Event types</div>
        <div class="es-stat__value es-mono">{{ store.distinctTypes() }}</div>
      </div>
      <div class="es-panel es-stat">
        <div class="es-stat__label">Routes</div>
        <div class="es-stat__value es-mono">{{ store.distinctRoutes() }}</div>
      </div>
      <div class="es-panel es-stat">
        <div class="es-stat__label">Success rate</div>
        <div class="es-stat__value es-mono">{{ successRatePct() }}%</div>
      </div>
    </div>

    <!-- Charts grid ─────────────────────────────────────────────────── -->
    <div class="es-grid">

      <section class="es-panel es-panel--wide">
        <h2>Throughput by channel (last 30 min)</h2>
        @if (hasThroughput()) {
          <div echarts [options]="throughputOpt()" class="es-chart es-chart--tall"></div>
        } @else {
          <p class="es-empty">No data yet — waiting for the first event.</p>
        }
      </section>

      <section class="es-panel">
        <h2>Events per channel</h2>
        @if (store.perChannel().length > 0) {
          <div echarts [options]="channelOpt()" class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

      <section class="es-panel">
        <h2>Events per provider</h2>
        @if (store.perProvider().length > 0) {
          <div echarts [options]="providerOpt()" class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

      <section class="es-panel">
        <h2>Events per type</h2>
        @if (store.perEventType().length > 0) {
          <div echarts [options]="typeOpt()" class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

      <section class="es-panel">
        <h2>Delivery health</h2>
        @if (hasStatusData()) {
          <div echarts [options]="statusOpt()" class="es-chart"></div>
        } @else {
          <p class="es-empty">No status updates yet.</p>
        }
      </section>

      <section class="es-panel es-panel--wide">
        <h2>Top sources</h2>
        @if (store.topSources().length > 0) {
          <div echarts [options]="topSourcesOpt()" class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

    </div>
  `,
  styles: [`
    .es-row     { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
    h1          { margin: 0; font-size: 24px; }
    h2          { margin: 0 0 12px; font-size: 14px; color: var(--es-text-dim); }
    .es-stats   { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px; margin-bottom: 16px; }
    .es-stat__label { color: var(--es-text-dim); font-size: 13px; }
    .es-stat__value { font-size: 26px; margin-top: 4px; }
    .es-grid          { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); }
    .es-panel--wide   { grid-column: 1 / -1; }
    .es-chart         { width: 100%; height: 260px; }
    .es-chart--tall   { height: 320px; }
    .es-empty         { color: var(--es-text-dim); padding: 32px 0; margin: 0; text-align: center; }
  `],
})
export class EventsPageComponent {
  protected readonly store = inject(EventsStore);

  // ── Derived chart options (re-evaluated whenever the store changes) ─

  protected readonly hasThroughput = computed(() => this.store.perMinute().length > 0);
  protected readonly hasStatusData = computed(() => {
    const r = this.store.statusRing();
    return r.success + r.inFlight + r.failure + r.other > 0;
  });

  protected readonly successRatePct = computed(() => {
    const r = this.store.statusRing();
    const meaningful = r.success + r.failure;
    if (meaningful === 0) return '—';
    return ((r.success / meaningful) * 100).toFixed(1);
  });

  // ── Charts ───────────────────────────────────────────────────────────
  protected readonly throughputOpt = computed<EChartsOption>(() => {
    const { minutes, series } = this.store.perMinuteByChannel();
    return {
      tooltip:  { trigger: 'axis', axisPointer: { type: 'shadow' } },
      legend:   { textStyle: { color: COLORS.text }, top: 0 },
      grid:     { left: 16, right: 24, top: 32, bottom: 8, containLabel: true },
      xAxis:    { type: 'category', data: minutes, axisLine: { lineStyle: { color: '#555' } }, axisLabel: { color: COLORS.dim } },
      yAxis:    { type: 'value', splitLine: { lineStyle: { color: COLORS.grid } }, axisLabel: { color: COLORS.dim } },
      series:   series.map((s) => ({
        name:      s.name,
        type:      'bar',
        stack:     'total',
        data:      s.data,
        itemStyle: { color: channelColor(s.name) },
        emphasis:  { focus: 'series' },
      })),
    };
  });

  protected readonly channelOpt = computed<EChartsOption>(() => ({
    tooltip:  { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
    legend:   { orient: 'vertical', right: 8, top: 'middle', textStyle: { color: COLORS.text } },
    series:   [{
      type:    'pie',
      radius:  ['45%', '70%'],
      center:  ['38%', '50%'],
      avoidLabelOverlap: true,
      label:    { show: false },
      emphasis: { label: { show: true, fontSize: 13, fontWeight: 'bold', color: COLORS.text } },
      data:     this.store.perChannel().map((d) => ({
        name:      d.key,
        value:     d.count,
        itemStyle: { color: channelColor(d.key) },
      })),
    }],
  }));

  protected readonly providerOpt = computed<EChartsOption>(() => {
    const data = [...this.store.perProvider()].reverse();
    return {
      tooltip:  { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid:     { left: 16, right: 24, top: 8, bottom: 8, containLabel: true },
      xAxis:    { type: 'value', splitLine: { lineStyle: { color: COLORS.grid } }, axisLabel: { color: COLORS.dim } },
      yAxis:    { type: 'category', data: data.map((d) => d.key), axisLabel: { color: COLORS.text } },
      series:   [{
        type:      'bar',
        data:      data.map((d) => d.count),
        itemStyle: { color: COLORS.accent, borderRadius: [0, 4, 4, 0] },
        label:     { show: true, position: 'right', color: COLORS.text },
      }],
    };
  });

  protected readonly typeOpt = computed<EChartsOption>(() => ({
    tooltip:  { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
    legend:   { orient: 'vertical', right: 8, top: 'middle', textStyle: { color: COLORS.text, fontSize: 11 } },
    series:   [{
      type:    'pie',
      radius:  ['45%', '70%'],
      center:  ['38%', '50%'],
      avoidLabelOverlap: true,
      label:    { show: false },
      emphasis: { label: { show: true, fontSize: 12, fontWeight: 'bold', color: COLORS.text } },
      data:     this.store.perEventType().map((d, i) => ({
        name:      d.key,
        value:     d.count,
        itemStyle: { color: COLORS.palette[i % COLORS.palette.length] },
      })),
    }],
  }));

  protected readonly statusOpt = computed<EChartsOption>(() => {
    const r = this.store.statusRing();
    return {
      tooltip:  { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
      legend:   { orient: 'vertical', right: 8, top: 'middle', textStyle: { color: COLORS.text } },
      series:   [{
        type:    'pie',
        radius:  ['55%', '75%'],
        center:  ['38%', '50%'],
        label:   { show: true, formatter: '{d}%', color: COLORS.text, fontSize: 11 },
        data:    [
          { name: 'success',   value: r.success,  itemStyle: { color: COLORS.success } },
          { name: 'in-flight', value: r.inFlight, itemStyle: { color: COLORS.inFlight } },
          { name: 'failure',   value: r.failure,  itemStyle: { color: COLORS.failure } },
          { name: 'other',     value: r.other,    itemStyle: { color: COLORS.other } },
        ].filter((d) => d.value > 0),
      }],
    };
  });

  protected readonly topSourcesOpt = computed<EChartsOption>(() => {
    const data = [...this.store.topSources()].reverse();
    return {
      tooltip:  { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid:     { left: 16, right: 24, top: 8, bottom: 8, containLabel: true },
      xAxis:    { type: 'value', splitLine: { lineStyle: { color: COLORS.grid } }, axisLabel: { color: COLORS.dim } },
      yAxis:    { type: 'category', data: data.map((d) => d.key), axisLabel: { color: COLORS.text } },
      series:   [{
        type:      'bar',
        data:      data.map((d) => d.count),
        itemStyle: { color: COLORS.accent, borderRadius: [0, 4, 4, 0] },
        label:     { show: true, position: 'right', color: COLORS.text },
      }],
    };
  });
}
