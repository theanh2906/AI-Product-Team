import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { DesktopWindowService } from './desktop-window.service';
import { ProjectApiService } from './project-api.service';
import { ProjectBoard } from './work-item.models';

export interface AppUpdateStatus {
  enabled: boolean;
  state: 'disabled' | 'current' | 'available' | 'unavailable';
  currentVersion: string;
  latestVersion?: string;
  notes?: string;
  message: string;
}

export interface AppUpdateActiveTask {
  key: string;
  title: string;
}

const UPDATE_INTERVAL_MS = 30 * 1000;

@Injectable({ providedIn: 'root' })
export class AppUpdateService {
  private readonly desktop = inject(DesktopWindowService);
  private readonly projects = inject(ProjectApiService);
  private readonly http = inject(HttpClient);
  private initialized = false;
  private updatePath = '';
  private installConfirmationResolver?: (confirmed: boolean) => void;

  readonly status = signal<AppUpdateStatus | null>(null);
  readonly checking = signal(false);
  readonly installing = signal(false);
  readonly dismissedVersion = signal<string | null>(null);
  readonly pendingInstallConfirmation = signal<AppUpdateActiveTask | null>(null);

  initialize(): void {
    if (this.initialized || !this.desktop.isDesktop) return;
    this.initialized = true;
    void this.check();
    window.setInterval(() => void this.check(), UPDATE_INTERVAL_MS);
  }

  async check(): Promise<void> {
    if (!this.desktop.isDesktop || this.checking()) return;
    this.checking.set(true);
    try {
      const settings = await firstValueFrom(this.projects.getSettings());
      this.updatePath = settings.updatePath;
      const { invoke } = await import('@tauri-apps/api/core');
      this.status.set(await invoke<AppUpdateStatus>('check_for_update', { updatePath: this.updatePath }));
    } catch (error) {
      this.status.set({
        enabled: true,
        state: 'unavailable',
        currentVersion: this.status()?.currentVersion ?? 'unknown',
        message: error instanceof Error ? error.message : 'Could not check for ProductCrew updates.',
      });
    } finally {
      this.checking.set(false);
    }
  }

  dismiss(): void {
    this.dismissedVersion.set(this.status()?.latestVersion ?? null);
  }

  shouldShowToast(): boolean {
    const status = this.status();
    return status?.state === 'available' && this.dismissedVersion() !== status.latestVersion;
  }

  async install(): Promise<void> {
    if (!this.desktop.isDesktop || !this.updatePath || this.installing() || this.pendingInstallConfirmation()) return;
    const activeTask = await this.activeTaskInProgress();
    if (activeTask && !(await this.confirmInstallDuringActiveTask(activeTask))) return;

    this.installing.set(true);
    try {
      const { invoke } = await import('@tauri-apps/api/core');
      await invoke('install_update', { updatePath: this.updatePath });
    } finally {
      this.installing.set(false);
    }
  }

  confirmInstall(): void {
    this.resolveInstallConfirmation(true);
  }

  cancelInstallConfirmation(): void {
    this.resolveInstallConfirmation(false);
  }

  private async activeTaskInProgress(): Promise<AppUpdateActiveTask | null> {
    try {
      const boards = await firstValueFrom(this.http.get<ProjectBoard[]>('/api/boards'));
      for (const board of boards) {
        const task = board.tasks.find((candidate) => candidate.status === 'in_progress' || candidate.status === 'verifying');
        if (task) return { key: task.key, title: task.title };
      }
    } catch {
      return null;
    }
    return null;
  }

  private confirmInstallDuringActiveTask(task: AppUpdateActiveTask): Promise<boolean> {
    if (this.installConfirmationResolver) return Promise.resolve(false);
    this.pendingInstallConfirmation.set(task);
    return new Promise((resolve) => {
      this.installConfirmationResolver = resolve;
    });
  }

  private resolveInstallConfirmation(confirmed: boolean): void {
    const resolve = this.installConfirmationResolver;
    if (!resolve) return;
    this.installConfirmationResolver = undefined;
    this.pendingInstallConfirmation.set(null);
    resolve(confirmed);
  }
}
