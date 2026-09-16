import { DatePipe } from '@angular/common';
import { Component, HostListener, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink, RouterLinkActive } from '@angular/router';

import { AppNotification, NotificationService } from '../../core/notification.service';
import { AppUpdateService } from '../../core/app-update.service';
import { DesktopWindowService } from '../../core/desktop-window.service';
import { RuntimeHealthService } from '../../core/runtime-health.service';
import { ThemeService } from '../../core/theme.service';
import { ProjectContextService } from '../../core/project-context.service';

const SIDEBAR_COLLAPSED_STORAGE_KEY = 'productcrew.sidebar-collapsed';

@Component({
  selector: 'app-shell',
  imports: [DatePipe, FormsModule, RouterLink, RouterLinkActive],
  templateUrl: './app-shell.html',
})
export class AppShell {
  readonly theme = inject(ThemeService);
  readonly notifications = inject(NotificationService);
  readonly desktop = inject(DesktopWindowService);
  readonly updates = inject(AppUpdateService);
  readonly runtimeHealth = inject(RuntimeHealthService);
  readonly projectContext = inject(ProjectContextService);
  private readonly router = inject(Router);
  readonly notificationPanelOpen = signal(false);
  readonly appVersion = signal('Development build');
  readonly sidebarCollapsed = signal(this.readSidebarCollapsed());

  constructor() {
    this.theme.load();
    void this.notifications.initialize();
    this.updates.initialize();
    this.runtimeHealth.initialize();
    void this.projectContext.initialize();
    void this.loadAppVersion();
  }

  selectProject(projectId: string): void {
    if (!this.projectContext.select(projectId)) return;
    void this.router.navigate([], {
      queryParams: { projectId },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }

  @HostListener('document:keydown.escape')
  closeNotifications(): void {
    if (this.updates.pendingInstallConfirmation()) {
      this.updates.cancelInstallConfirmation();
      return;
    }
    this.notificationPanelOpen.set(false);
  }

  @HostListener('document:click')
  closeNotificationsOnOutsideClick(): void {
    this.notificationPanelOpen.set(false);
  }

  toggleNotifications(event: MouseEvent): void {
    event.stopPropagation();
    this.notificationPanelOpen.update((open) => !open);
  }

  toggleSidebar(): void {
    this.sidebarCollapsed.update((collapsed) => {
      const next = !collapsed;
      this.persistSidebarCollapsed(next);
      if (next) this.notificationPanelOpen.set(false);
      return next;
    });
  }

  async openNotification(item: AppNotification): Promise<void> {
    await this.notifications.markRead(item);
    this.notificationPanelOpen.set(false);
    await this.navigateToNotificationTarget(item);
  }

  async openNotificationBoard(event: MouseEvent, item: AppNotification): Promise<void> {
    event.stopPropagation();
    await this.openNotification(item);
  }

  iconFor(item: AppNotification): string {
    return { success: 'check_circle', warning: 'warning', error: 'error', info: 'info' }[item.level];
  }

  providerLabel(provider: string | null | undefined): string {
    if (provider === 'claude-code') return 'Claude Code';
    if (provider === 'github-copilot') return 'GitHub Copilot';
    if (provider === 'codex') return 'Codex';
    return 'None';
  }

  connectedSubtitle(): string {
    const label = this.providerLabel(this.runtimeHealth.status()?.aiProvider);
    return label === 'None' ? 'AI CLI connected' : `${label} connected`;
  }

  selectedProjectTitle(): string {
    return `Active project: ${this.projectContext.selectedProject()?.name || 'No project selected'}`;
  }

  runtimeStatusTitle(): string {
    const status = this.runtimeHealth.status();
    if (status?.state === 'connected') return `Local runtime ready: ${this.connectedSubtitle()}`;
    if (status?.state === 'unavailable') return `Runtime unavailable. Selected provider: ${this.providerLabel(status.aiProvider)}`;
    if (status?.state === 'unreachable') return 'Local service unreachable. Check Settings.';
    return 'Checking runtime';
  }

  private async loadAppVersion(): Promise<void> {
    if (!this.desktop.isDesktop) return;
    try {
      const { getVersion } = await import('@tauri-apps/api/app');
      this.appVersion.set(`v${await getVersion()}`);
    } catch {
      this.appVersion.set('Version unavailable');
    }
  }

  private readSidebarCollapsed(): boolean {
    if (typeof localStorage === 'undefined') return false;
    return localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) === 'true';
  }

  private persistSidebarCollapsed(collapsed: boolean): void {
    if (typeof localStorage === 'undefined') return;
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, String(collapsed));
  }

  private async navigateToNotificationTarget(item: AppNotification): Promise<void> {
    if (item.projectId) {
      await this.router.navigate([item.route || '/work-items'], {
        queryParams: {
          projectId: item.projectId,
          taskId: item.route === '/project-atlas' ? null : item.entityId || null,
        },
      });
      return;
    }
    if (item.route) await this.router.navigateByUrl(item.route);
  }
}
