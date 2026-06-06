import {
  ChangeDetectionStrategy,
  Component,
  ViewChild,
  ElementRef,
  OnDestroy,
  computed,
  effect,
  inject,
  afterNextRender,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import type { ECharts, EChartsOption } from 'echarts';
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
        @if (store.perChannel().length > 0) {
          <div #channelChart class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

      <section class="es-panel">
        <h2>Events per type</h2>
        @if (store.perEventType().length > 0) {
          <div #typeChart class="es-chart"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

      <section class="es-panel es-panel--wide">
        <h2>Throughput per minute</h2>
        @if (store.perMinute().length > 0) {
          <div #throughputChart class="es-chart es-chart--tall"></div>
        } @else {
          <p class="es-empty">No data yet.</p>
        }
      </section>

    </div>
  `,
  styles: [`
    .es-row           { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
    h1                { margin: 0; font-size: 24px; }
    h2                { margin: 0 0 12px; font-size: 14px; color: var(--es-text-dim); }
    .es-grid          { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); }
    .es-panel--wide   { grid-column: 1 / -1; }
    .es-chart         { width: 100%; height: 220px; }
    .es-chart--tall   { height: 280px; }
    .es-empty         { color: var(--es-text-dim); padding: 24px 0; margin: 0; }
  `],
})
export class MetricsPageComponent implements OnDestroy {
  protected readonly store = inject(MetricsStore);

  @ViewChild('channelChart')    private channelChartEl!: ElementRef<HTMLDivElement>;
  @ViewChild('typeChart')       private typeChartEl!: ElementRef<HTMLDivElement>;
  @ViewChild('throughputChart') private throughputChartEl!: ElementRef<HTMLDivElement>;

  private channelChart:    ECharts | null = null;
  private typeChart:       ECharts | null = null;
  private throughputChart: ECharts | null = null;

  // ─── Computed ECharts options (derived from Signals — HARDNESS §7) ───────

  protected readonly channelChartOptions = computed<EChartsOption>(() => {
    const data = [...this.store.perChannel()].reverse();
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 16, right: 24, top: 8, bottom: 8, containLabel: true },
      xAxis: { type: 'value', splitLine: { lineStyle: { color: '#2a2a3a' } }, axisLabel: { color: '#aaa' } },
      yAxis: { type: 'category', data: data.map(d => d.channel), axisLabel: { color: '#ccc' } },
      series: [{
        type: 'bar',
        data: data.map(d => d.count),
        itemStyle: { color: '#6c63ff', borderRadius: [0, 4, 4, 0] },
        label: { show: true, position: 'right', color: '#ccc' },
      }],
    };
  });

  protected readonly typeChartOptions = computed<EChartsOption>(() => {
    const palette = ['#6c63ff', '#00c8ff', '#ff6b6b', '#ffd166', '#06d6a0', '#a78bfa'];
    return {
      tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
      legend: { orient: 'vertical', right: 8, top: 'center', textStyle: { color: '#ccc' } },
      series: [{
        type: 'pie',
        radius: ['40%', '70%'],
        center: ['38%', '50%'],
        avoidLabelOverlap: true,
        label: { show: false },
        emphasis: { label: { show: true, fontSize: 13, fontWeight: 'bold' } },
        data: this.store.perEventType().map((d, i) => ({
          name: d.eventType,
          value: d.count,
          itemStyle: { color: palette[i % palette.length] },
        })),
      }],
    };
  });

  protected readonly throughputChartOptions = computed<EChartsOption>(() => {
    const buckets = this.store.perMinute();
    return {
      tooltip: { trigger: 'axis' },
      grid: { left: 16, right: 24, top: 16, bottom: 8, containLabel: true },
      xAxis: {
        type: 'category',
        data: buckets.map(b => b.minute.slice(11, 16)),
        axisLine: { lineStyle: { color: '#555' } },
        axisLabel: { color: '#aaa' },
      },
      yAxis: { type: 'value', splitLine: { lineStyle: { color: '#2a2a3a' } }, axisLabel: { color: '#aaa' } },
      series: [{
        type: 'line',
        data: buckets.map(b => b.count),
        smooth: true,
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { color: '#6c63ff', width: 2 },
        itemStyle: { color: '#6c63ff' },
        areaStyle: { color: 'rgba(108,99,255,0.15)' },
      }],
    };
  });

  constructor() {
    // Initialize charts after first render — dynamic import keeps echarts out of the main bundle
    afterNextRender(() => {
      import('echarts').then(({ init }) => {
        if (this.channelChartEl?.nativeElement)
          this.channelChart = init(this.channelChartEl.nativeElement);
        if (this.typeChartEl?.nativeElement)
          this.typeChart = init(this.typeChartEl.nativeElement);
        if (this.throughputChartEl?.nativeElement)
          this.throughputChart = init(this.throughputChartEl.nativeElement);

        this.channelChart?.setOption(this.channelChartOptions());
        this.typeChart?.setOption(this.typeChartOptions());
        this.throughputChart?.setOption(this.throughputChartOptions());
      });
    });

    // Reactively update charts whenever Signal data changes (HARDNESS §7 — Signals only)
    effect(() => { this.channelChart?.setOption(this.channelChartOptions()); });
    effect(() => { this.typeChart?.setOption(this.typeChartOptions()); });
    effect(() => { this.throughputChart?.setOption(this.throughputChartOptions()); });
  }

  ngOnDestroy(): void {
    this.channelChart?.dispose();
    this.typeChart?.dispose();
    this.throughputChart?.dispose();
  }
}
