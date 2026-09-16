import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

export type RuntimeHealthState = 'connected' | 'unavailable' | 'unreachable';

export interface RuntimeHealthStatus {
  state: RuntimeHealthState;
  aiProvider: string | null;
}

interface HealthResponse {
  status: string;
  codexAppServer: string;
  aiRuntime?: string;
  aiProvider: string;
}

const HEALTH_POLL_INTERVAL_MS = 30_000;

@Injectable({ providedIn: 'root' })
export class RuntimeHealthService {
  private readonly http = inject(HttpClient);
  private initialized = false;

  readonly status = signal<RuntimeHealthStatus | null>(null);

  initialize(): void {
    if (this.initialized) return;
    this.initialized = true;
    void this.refresh();
    window.setInterval(() => void this.refresh(), HEALTH_POLL_INTERVAL_MS);
  }

  async refresh(): Promise<void> {
    try {
      const health = await firstValueFrom(this.http.get<HealthResponse>('/api/health'));
      this.status.set({
        state: (health.aiRuntime ?? health.codexAppServer) === 'connected' ? 'connected' : 'unavailable',
        aiProvider: health.aiProvider,
      });
    } catch {
      this.status.set({ state: 'unreachable', aiProvider: null });
    }
  }
}
