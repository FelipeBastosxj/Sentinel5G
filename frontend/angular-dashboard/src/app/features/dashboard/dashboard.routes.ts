import { Routes } from '@angular/router';

export const DASHBOARD_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () =>
      import('./components/dashboard-page.component').then(
        (m) => m.DashboardPageComponent,
      ),
  },
];
