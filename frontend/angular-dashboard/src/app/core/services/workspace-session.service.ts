import { Injectable, computed, signal } from '@angular/core';
import { environment } from '../../../environments/environment';

export interface WorkspaceSession {
  workspaceId: string;
  endpointToken: string;
  name: string;
}

const STORAGE_KEY = 'twh_session';

/**
 * WorkspaceSessionService — webhook.site-style anonymous session.
 *
 * On first visit a UUID token is generated and stored in localStorage.
 * POST /workspace/auto is called to provision (or retrieve) a workspace.
 * The resulting webhookUrl is shown in the header, ready to paste into
 * any telecom provider dashboard.
 */
@Injectable({ providedIn: 'root' })
export class WorkspaceSessionService {
  readonly session  = signal<WorkspaceSession | null>(null);
  readonly loading  = signal(false);
  readonly error    = signal<string | null>(null);

  /** Full webhook receiver URL — reactive Signal, safe to call in templates. */
  readonly webhookUrl = computed<string | null>(() => {
    const s = this.session();
    return s
      ? `${environment.ingestionUrl}/${s.workspaceId}/${s.endpointToken}`
      : null;
  });

  async init(): Promise<void> {
    // Restore from localStorage
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      try {
        this.session.set(JSON.parse(stored));
        return;
      } catch { localStorage.removeItem(STORAGE_KEY); }
    }

    // First visit: generate a token, provision workspace
    const token = crypto.randomUUID();
    this.loading.set(true);
    this.error.set(null);

    try {
      const res = await fetch(`${environment.processingUrl}/workspace/auto`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
        signal: AbortSignal.timeout(8000),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const session: WorkspaceSession = await res.json();
      this.session.set(session);
      localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
    } catch (err) {
      this.error.set('Could not provision workspace. Is the backend running?');
      console.error('[WorkspaceSession]', err);
    } finally {
      this.loading.set(false);
    }
  }

  /** Reset — generates a new token on next init. */
  reset(): void {
    localStorage.removeItem(STORAGE_KEY);
    this.session.set(null);
  }
}
