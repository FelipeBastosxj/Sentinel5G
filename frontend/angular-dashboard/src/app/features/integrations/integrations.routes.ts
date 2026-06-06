import { Routes } from '@angular/router';

export const INTEGRATIONS_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () =>
      import('./components/integrations-page.component').then(
        (m) => m.IntegrationsPageComponent,
      ),
  },
];
