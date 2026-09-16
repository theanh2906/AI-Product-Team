import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { AppUpdateService } from '../../core/app-update.service';
import { DesktopWindowService } from '../../core/desktop-window.service';
import { NotificationService } from '../../core/notification.service';
import { RuntimeHealthService, RuntimeHealthStatus } from '../../core/runtime-health.service';
import { ThemeService } from '../../core/theme.service';
import { AppShell } from './app-shell';

describe('AppShell runtime badge', () => {
  const runtimeHealth = { status: signal<RuntimeHealthStatus | null>(null), initialize: () => {} };
  const notifications = {
    unreadCount: () => 0,
    items: signal([]),
    loading: signal(false),
    toast: signal(null),
    initialize: async () => {},
  };
  const theme = { isDark: () => false, saving: () => false, load: () => {}, toggle: () => {} };
  const updates = {
    shouldShowToast: () => false,
    status: signal(null),
    pendingInstallConfirmation: signal<{ key: string; title: string } | null>(null),
    installing: signal(false),
    initialize: () => {},
    confirmInstall: () => {},
    cancelInstallConfirmation: () => {},
  };
  const desktop = { isDesktop: false };

  beforeEach(async () => {
    runtimeHealth.status.set(null);
    await TestBed.configureTestingModule({
      imports: [AppShell],
      providers: [
        provideRouter([]),
        { provide: RuntimeHealthService, useValue: runtimeHealth },
        { provide: NotificationService, useValue: notifications },
        { provide: ThemeService, useValue: theme },
        { provide: AppUpdateService, useValue: updates },
        { provide: DesktopWindowService, useValue: desktop },
      ],
    }).compileComponents();
  });

  function render() {
    const fixture = TestBed.createComponent(AppShell);
    fixture.detectChanges();
    return fixture;
  }

  it('shows a neutral checking state, with no subtitle, before the first health response resolves', () => {
    const fixture = render();
    const badge = fixture.nativeElement.querySelector('.local-status');

    expect(badge.textContent).toContain('Checking runtime…');
    expect(badge.textContent).not.toContain('AI CLI connected');
    expect(badge.textContent).not.toContain('Local runtime ready');
    expect(badge.classList.contains('checking')).toBe(true);
    expect(badge.classList.contains('unavailable')).toBe(false);
    expect(badge.classList.contains('unreachable')).toBe(false);
    expect(badge.querySelector('small')).toBeNull();
  });

  it('transitions from the checking state to connected in place, clearing the checking class', () => {
    const fixture = render();
    let badge = fixture.nativeElement.querySelector('.local-status');
    expect(badge.classList.contains('checking')).toBe(true);

    runtimeHealth.status.set({ state: 'connected', aiProvider: 'codex' });
    fixture.detectChanges();

    badge = fixture.nativeElement.querySelector('.local-status');
    expect(badge.textContent).toContain('Local runtime ready');
    expect(badge.textContent).toContain('Codex connected');
    expect(badge.classList.contains('checking')).toBe(false);
  });

  it('shows the selected provider as connected when the runtime is healthy', () => {
    runtimeHealth.status.set({ state: 'connected', aiProvider: 'claude-code' });
    const fixture = render();
    const badge = fixture.nativeElement.querySelector('.local-status');

    expect(badge.textContent).toContain('Local runtime ready');
    expect(badge.textContent).toContain('Claude Code connected');
    expect(badge.textContent).not.toContain('AI CLI connected');
    expect(badge.classList.contains('unavailable')).toBe(false);
    expect(badge.classList.contains('unreachable')).toBe(false);
  });

  it('labels GitHub Copilot provider health clearly', () => {
    runtimeHealth.status.set({ state: 'connected', aiProvider: 'github-copilot' });
    const fixture = render();
    const badge = fixture.nativeElement.querySelector('.local-status');

    expect(badge.textContent).toContain('GitHub Copilot connected');
  });

  it('does not claim "AI CLI connected" and names the selected provider when the app-server is unavailable', () => {
    runtimeHealth.status.set({ state: 'unavailable', aiProvider: 'codex' });
    const fixture = render();
    const badge = fixture.nativeElement.querySelector('.local-status');

    expect(badge.textContent).toContain('Runtime unavailable');
    expect(badge.textContent).toContain('Selected provider: Codex');
    expect(badge.textContent).not.toContain('AI CLI connected');
    expect(badge.classList.contains('unavailable')).toBe(true);
  });

  it('shows a degraded state with a keyboard-reachable Settings link when the health request fails', () => {
    runtimeHealth.status.set({ state: 'unreachable', aiProvider: null });
    const fixture = render();
    const badge = fixture.nativeElement.querySelector('.local-status');
    const link = badge.querySelector('a');

    expect(badge.textContent).toContain('Local service unreachable');
    expect(badge.classList.contains('unreachable')).toBe(true);
    expect(link).not.toBeNull();
    expect(link.getAttribute('href')).toBe('/settings');
    expect(link.getAttribute('tabindex')).not.toBe('-1');
  });

  it('updates the badge in place when health state changes, without recreating the component', () => {
    runtimeHealth.status.set({ state: 'connected', aiProvider: 'codex' });
    const fixture = render();
    expect(fixture.nativeElement.querySelector('.local-status').textContent).toContain('Local runtime ready');

    runtimeHealth.status.set({ state: 'unreachable', aiProvider: null });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.local-status').textContent).toContain('Local service unreachable');
  });

  it('leaves the notification toggle, theme toggle, and nav links unaffected by the badge change', () => {
    runtimeHealth.status.set({ state: 'connected', aiProvider: 'codex' });
    const fixture = render();

    expect(fixture.nativeElement.querySelector('.notification-toggle')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.theme-toggle')).not.toBeNull();
    expect(fixture.nativeElement.querySelectorAll('.nav-list a').length).toBeGreaterThan(0);
  });

  it('renders an in-app update confirmation dialog for active AI sessions', () => {
    updates.pendingInstallConfirmation.set({
      key: 'QA-010',
      title: '[QA] Validate smoke-test tracking end to end',
    });
    const fixture = render();
    const dialog = fixture.nativeElement.querySelector('.app-confirm-dialog');

    expect(dialog).not.toBeNull();
    expect(dialog.getAttribute('role')).toBe('dialog');
    expect(dialog.textContent).toContain('Pause before updating?');
    expect(dialog.textContent).toContain('QA-010');
    expect(dialog.textContent).toContain('[QA] Validate smoke-test tracking end to end');
    expect(fixture.nativeElement.querySelector('[aria-label="ProductCrew home"]')).not.toBeNull();
  });
});
