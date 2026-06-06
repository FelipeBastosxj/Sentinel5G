import { Routes } from '@angular/router';

/**
 * Top-level routes — every feature is lazy-loaded as a standalone route file.
 * Feature-based architecture (HARDNESS §7).
 */
export const APP_ROUTES: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'dashboard' },
  {
    path: 'dashboard',
    loadChildren: () =>
      import('./features/dashboard/dashboard.routes').then((m) => m.DASHBOARD_ROUTES),
  },
  {
    path: 'events',
    loadChildren: () =>
      import('./features/events/events.routes').then((m) => m.EVENTS_ROUTES),
  },
  {
    path: 'metrics',
    loadChildren: () =>
      import('./features/metrics/metrics.routes').then((m) => m.METRICS_ROUTES),
  },
  {
    path: 'integrations',
    loadChildren: () =>
      import('./features/integrations/integrations.routes').then(
        (m) => m.INTEGRATIONS_ROUTES,
      ),
  },
  { path: '**', redirectTo: 'dashboard' },
];
