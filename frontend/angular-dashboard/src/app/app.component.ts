import { ChangeDetectionStrategy, Component, OnInit, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { WorkspaceSessionService } from './core/services/workspace-session.service';

@Component({
  selector: 'es-root',
  standalone: true,
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="es-header">
      <div class="es-brand">
        <span class="es-brand__logo">🪝</span>
        <span class="es-brand__name">TelecomWebhook</span>
        <span class="es-brand__tag">inspector</span>
      </div>
      <nav class="es-nav">
        <a routerLink="/dashboard"    routerLinkActive="active">Dashboard</a>
        <a routerLink="/events"       routerLinkActive="active">Events</a>
        <a routerLink="/integrations" routerLinkActive="active">Integrations</a>
      </nav>
    </header>

    @if (ws.loading()) {
      <div class="url-bar url-bar--loading">Provisioning your webhook URL…</div>
    } @else if (ws.error()) {
      <div class="url-bar url-bar--error">{{ ws.error() }}</div>
    } @else if (ws.webhookUrl()) {
      <div class="url-bar">
        <span class="url-bar__label">Your webhook URL</span>
        <span class="url-bar__url">{{ ws.webhookUrl() }}</span>
        <button class="url-bar__copy" (click)="copy()" [class.copied]="copied">
          {{ copied ? '✓ Copied' : 'Copy' }}
        </button>
        <button class="url-bar__new" (click)="newUrl()" title="Generate a new URL">New</button>
      </div>
    }

    <main class="es-main">
      <router-outlet />
    </main>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; min-height: 100vh; }
    .es-header {
      display: flex; align-items: center; justify-content: space-between;
      padding: 12px 24px; background: var(--es-panel);
      border-bottom: 1px solid var(--es-border);
    }
    .es-brand { display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 15px; }
    .es-brand__logo { font-size: 18px; }
    .es-brand__tag {
      font-size: 11px; background: rgba(47,129,247,0.15);
      color: var(--es-accent); padding: 2px 7px; border-radius: 999px;
    }
    .es-nav { display: flex; gap: 4px; }
    .es-nav a { color: var(--es-text-dim); padding: 6px 12px; border-radius: 6px; font-size: 13px; }
    .es-nav a:hover { background: rgba(255,255,255,0.06); color: var(--es-text); }
    .es-nav .active { background: rgba(47,129,247,0.12); color: var(--es-accent); }
    .url-bar {
      display: flex; align-items: center; gap: 10px;
      padding: 8px 24px; background: rgba(47,129,247,0.07);
      border-bottom: 1px solid var(--es-border); font-size: 13px;
    }
    .url-bar--loading { color: var(--es-text-dim); font-style: italic; }
    .url-bar--error { color: #f85149; }
    .url-bar__label { color: var(--es-text-dim); white-space: nowrap; }
    .url-bar__url { flex: 1; font-family: monospace; color: var(--es-accent); word-break: break-all; }
    .url-bar__copy, .url-bar__new {
      padding: 4px 12px; border-radius: 6px; font-size: 12px; cursor: pointer;
      border: 1px solid var(--es-border); background: var(--es-panel);
      color: var(--es-text); white-space: nowrap;
    }
    .url-bar__copy:hover { border-color: var(--es-accent); color: var(--es-accent); }
    .url-bar__copy.copied { background: rgba(63,185,80,0.15); color: #3fb950; border-color: #3fb950; }
    .url-bar__new:hover { border-color: #f85149; color: #f85149; }
    .es-main { flex: 1; padding: 24px; }
  `],
})
export class AppComponent implements OnInit {
  readonly ws = inject(WorkspaceSessionService);
  copied = false;

  ngOnInit(): void {
    void this.ws.init();
  }

  copy(): void {
    const url = this.ws.webhookUrl();
    if (!url) return;
    navigator.clipboard.writeText(url).then(() => {
      this.copied = true;
      setTimeout(() => (this.copied = false), 2000);
    });
  }

  newUrl(): void {
    if (confirm('Generate a new webhook URL? The current URL will stop receiving events.')) {
      this.ws.reset();
      void this.ws.init();
    }
  }
}

