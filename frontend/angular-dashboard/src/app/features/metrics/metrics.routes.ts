import { Routes } from '@angular/router';

export const METRICS_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () =>
      import('./components/metrics-page.component').then(
        (m) => m.MetricsPageComponent,
      ),
  },
];
