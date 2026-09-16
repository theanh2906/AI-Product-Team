import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NEVER, of } from 'rxjs';

import { AppUpdateService } from '../../core/app-update.service';
import { DesktopWindowService } from '../../core/desktop-window.service';
import { NotificationService } from '../../core/notification.service';
import { ObservabilityApiService } from '../../core/observability-api.service';
import { AISessionOverview, ObservabilityOverview, ObservationEvent } from '../../core/observability.models';
import { ProjectApiService } from '../../core/project-api.service';
import { RuntimeHealthService } from '../../core/runtime-health.service';
import { ThemeService } from '../../core/theme.service';
import { ObservabilityPage } from './observability-page';

describe('ObservabilityPage', () => {
  let events: ObservationEvent[];

  const projectApi = {
    listProjects: () =>
      of([
        {
          id: 'project-1',
          name: 'ProductCrew',
          path: 'E:\\Projects\\AI-Product-Team',
          source: 'local',
          importedAt: '2026-09-11T00:00:00Z',
        },
      ]),
  };
  const observabilityApi = {
    overview: () => of(createOverview()),
    aiSessions: () => of(createSessions()),
    events: () => of(events),
    watch: () => NEVER,
  };
  const runtimeHealth = { status: signal(null), initialize: () => {} };
  const notifications = {
    unreadCount: () => 0,
    items: signal([]),
    loading: signal(false),
    toast: signal(null),
    initialize: async () => {},
  };
  const theme = { isDark: () => true, saving: () => false, load: () => {}, toggle: () => {} };
  const updates = {
    shouldShowToast: () => false,
    status: signal(null),
    pendingInstallConfirmation: signal(null),
    installing: signal(false),
    initialize: () => {},
    confirmInstall: () => {},
    cancelInstallConfirmation: () => {},
  };
  const desktop = { isDesktop: false };

  beforeEach(async () => {
    events = createEvents(25);
    await TestBed.configureTestingModule({
      imports: [ObservabilityPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: projectApi },
        { provide: ObservabilityApiService, useValue: observabilityApi },
        { provide: RuntimeHealthService, useValue: runtimeHealth },
        { provide: NotificationService, useValue: notifications },
        { provide: ThemeService, useValue: theme },
        { provide: AppUpdateService, useValue: updates },
        { provide: DesktopWindowService, useValue: desktop },
      ],
    }).compileComponents();
  });

  it('paginates Trace Explorer events at 20 items per page', async () => {
    const fixture = TestBed.createComponent(ObservabilityPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelectorAll('.trace-row')).toHaveLength(20);
    expect(root.textContent).toContain('Trace event 1');
    expect(root.textContent).toContain('Trace event 20');
    expect(root.textContent).not.toContain('Trace event 21');
    expect(root.textContent).toContain('Showing 1-20 of 25');
    expect(root.textContent).toContain('Page 1 / 2');

    root.querySelector<HTMLButtonElement>('[aria-label="Next trace page"]')?.click();
    fixture.detectChanges();

    expect(root.querySelectorAll('.trace-row')).toHaveLength(5);
    expect(root.textContent).toContain('Trace event 21');
    expect(root.textContent).toContain('Trace event 25');
    expect(root.textContent).not.toContain('Trace event 20');
    expect(root.textContent).toContain('Showing 21-25 of 25');
    expect(root.textContent).toContain('Page 2 / 2');
  });

  it('paginates Provider comparison so it does not grow past the session card', async () => {
    const fixture = TestBed.createComponent(ObservabilityPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    component.comparisonPageSize.set(2);
    fixture.detectChanges();

    expect(root.querySelectorAll('.comparison-list article')).toHaveLength(2);
    expect(root.textContent).toContain('Showing 1-2 of 5');
    expect(root.textContent).toContain('Page 1 / 3');
    expect(root.textContent).toContain('Codex');
    expect(root.textContent).toContain('GitHub Copilot');
    expect(root.textContent).not.toContain('Claude Code');

    root.querySelector<HTMLButtonElement>('[aria-label="Next provider comparison page"]')?.click();
    fixture.detectChanges();

    expect(root.querySelectorAll('.comparison-list article')).toHaveLength(2);
    expect(root.textContent).toContain('Showing 3-4 of 5');
    expect(root.textContent).toContain('Page 2 / 3');
    expect(root.textContent).toContain('Claude Code');
    expect(root.textContent).not.toContain('Codex');
  });

});

function createEvents(count: number): ObservationEvent[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `event-${index + 1}`,
    timestamp: `2026-09-11T10:${String(index).padStart(2, '0')}:00Z`,
    level: 'info',
    category: 'http',
    name: 'http.request',
    message: `Trace event ${index + 1}`,
    correlationId: `req-${index + 1}`,
    outcome: 'success',
    durationMs: index + 1,
  }));
}

function createOverview(): ObservabilityOverview {
  return {
    generatedAt: '2026-09-11T10:00:00Z',
    rangeDays: 30,
    metrics: {
      totalFeatures: 0,
      totalBugs: 0,
      backlogItems: 0,
      activePlans: 0,
      completedTasks: 0,
      planningApprovalRate: 0,
      averageTaskLeadHours: 0,
      aiJobSuccessRate: 0,
      averageAIJobSeconds: 0,
      reworkCount: 0,
    },
    featureFeasibility: [],
    featureStatus: [],
    bugSeverity: [],
    bugStatus: [],
    deliveryTargets: [],
    agentWorkload: [],
    trend: [],
    recentEvents: [],
  };
}

function createSessions(): AISessionOverview {
  return {
    generatedAt: '2026-09-11T10:00:00Z',
    rangeDays: 30,
    metrics: {
      activeRuns: 0,
      completedRuns: 0,
      successRate: 0,
      medianDurationMs: 0,
      trackedTokens: 0,
      usageTrackedRuns: 0,
    },
    runs: [],
    comparisons: [
      {
        provider: 'Codex',
        model: 'gpt-5.6-sol',
        runs: 12,
        successRate: 75,
        medianDurationMs: 340000,
        trackedTokens: 0,
        usageTrackedRuns: 0,
      },
      {
        provider: 'GitHub Copilot',
        model: 'gpt-5.6-sol',
        runs: 9,
        successRate: 68,
        medianDurationMs: 280000,
        trackedTokens: 0,
        usageTrackedRuns: 0,
      },
      {
        provider: 'Claude Code',
        model: 'sonnet',
        runs: 8,
        successRate: 88,
        medianDurationMs: 420000,
        trackedTokens: 125000,
        usageTrackedRuns: 3,
      },
      {
        provider: 'Unknown',
        model: 'not reported',
        runs: 7,
        successRate: 97,
        medianDurationMs: 210000,
        trackedTokens: 0,
        usageTrackedRuns: 0,
      },
      {
        provider: 'Claude Code',
        model: 'not reported',
        runs: 5,
        successRate: 84,
        medianDurationMs: 260000,
        trackedTokens: 0,
        usageTrackedRuns: 0,
      },
    ],
    providerUsage: [
      {
        provider: 'GitHub Copilot',
        primaryModel: 'gpt-5.6-sol',
        status: 'unavailable',
        confidence: 'exact',
        runs: 4,
        activeRuns: 1,
        completedRuns: 3,
        failedRuns: 0,
        successRate: 100,
        medianDurationMs: 64000,
        trackedTokens: 123456,
        usageTrackedRuns: 3,
        localPressurePercent: 100,
      },
      {
        provider: 'Claude Code',
        primaryModel: 'sonnet',
        status: 'unavailable',
        confidence: 'unavailable',
        runs: 1,
        activeRuns: 0,
        completedRuns: 1,
        failedRuns: 0,
        successRate: 100,
        medianDurationMs: 32000,
        trackedTokens: 0,
        usageTrackedRuns: 0,
        localPressurePercent: 0,
      },
    ],
  };
}
