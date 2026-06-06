import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';

@Component({
  selector: 'es-root',
  standalone: true,
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-header">
      <div class="es-brand">
        <span class="es-brand__logo">⚡</span>
        <span class="es-brand__name">EventStream</span>
        <span class="es-brand__tag es-tag">observability</span>
      </div>
      <nav class="es-nav">
        <a routerLink="/dashboard" routerLinkActive="es-nav__link--active">Dashboard</a>
        <a routerLink="/events" routerLinkActive="es-nav__link--active">Events</a>
        <a routerLink="/metrics" routerLinkActive="es-nav__link--active">Metrics</a>
        <a routerLink="/integrations" routerLinkActive="es-nav__link--active">Integrations</a>
      </nav>
    </header>
    <main class="es-main">
      <router-outlet />
    </main>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        min-height: 100vh;
      }
      .es-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 12px 24px;
        background: var(--es-panel);
        border-bottom: 1px solid var(--es-border);
      }
      .es-brand { display: flex; align-items: center; gap: 8px; font-weight: 600; }
      .es-brand__logo { font-size: 20px; }
      .es-nav { display: flex; gap: 16px; }
      .es-nav a {
        color: var(--es-text-dim);
        padding: 6px 12px;
        border-radius: 6px;
      }
      .es-nav a:hover { background: rgba(255,255,255,0.05); color: var(--es-text); }
      .es-nav__link--active { background: rgba(47,129,247,0.1); color: var(--es-accent) !important; }
      .es-main { flex: 1; padding: 24px; }
    `,
  ],
})
export class AppComponent {}
