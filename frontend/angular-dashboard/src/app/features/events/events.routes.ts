import { Routes } from '@angular/router';

export const EVENTS_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () =>
      import('./components/events-page.component').then(
        (m) => m.EventsPageComponent,
      ),
  },
];
