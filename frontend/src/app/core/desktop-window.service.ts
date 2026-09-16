import { Injectable } from '@angular/core';

function isTauriDesktop(): boolean {
  return typeof window !== 'undefined' && (window.location.hostname === 'tauri.localhost' || '__TAURI_INTERNALS__' in window);
}

@Injectable({ providedIn: 'root' })
export class DesktopWindowService {
  readonly isDesktop = isTauriDesktop();

  async minimize(): Promise<void> {
    await this.invoke('minimize_window');
  }

  async moveToNextMonitor(): Promise<void> {
    await this.invoke('move_to_next_monitor');
  }

  async closeToTray(): Promise<void> {
    await this.invoke('hide_main_window');
  }

  async openProjectFile(projectId: string, relativePath: string): Promise<void> {
    if (!this.isDesktop) {
      throw new Error('Opening local output files is available in the ProductCrew desktop app.');
    }
    await this.invoke('open_project_file', { projectId, relativePath });
  }

  async downloadProjectFile(projectId: string, relativePath: string): Promise<string> {
    if (!this.isDesktop) {
      throw new Error('Downloading local output files is available in the ProductCrew desktop app.');
    }
    return this.invoke<string>('download_project_file', { projectId, relativePath });
  }

  private async invoke<T = void>(command: string, args?: Record<string, unknown>): Promise<T> {
    if (!this.isDesktop) return undefined as T;
    const { invoke } = await import('@tauri-apps/api/core');
    return invoke<T>(command, args);
  }
}
