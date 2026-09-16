import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NEVER, Subject, of } from 'rxjs';

import { AppUpdateService } from '../../core/app-update.service';
import { DesktopWindowService } from '../../core/desktop-window.service';
import { NotificationService } from '../../core/notification.service';
import { ProjectApiService } from '../../core/project-api.service';
import { RuntimeHealthService } from '../../core/runtime-health.service';
import { ThemeService } from '../../core/theme.service';
import { BoardStreamEvent, WorkItemApiService } from '../../core/work-item-api.service';
import { ProjectBoard } from '../../core/work-item.models';
import { FeatureLibraryPage } from './feature-library-page';

describe('FeatureLibraryPage', () => {
  let boardEvents: Subject<ProjectBoard>;
  let boardStreamEvents: Subject<BoardStreamEvent>;
  let board: ProjectBoard;

  const projectApi = {
    listProjects: () =>
      of([
        {
          id: 'project-1',
          name: 'ProductCrew',
          path: 'E:\\Projects\\AI-Product-Team',
          source: 'local',
          importedAt: '2026-08-20T00:00:00Z',
        },
      ]),
  };
  const boardApi = {
    getBoard: () => of(board),
    watchBoard: () => boardEvents ?? NEVER,
    watchBoardStream: () => boardStreamEvents ?? NEVER,
    taskArtifactUrl: (_projectId: string, taskId: string, artifactId: string) =>
      `/api/tasks/${taskId}/artifacts/${artifactId}`,
    taskDesignSourceUrl: (_projectId: string, taskId: string, relativePath: string) =>
      `/api/tasks/${taskId}/design-source?path=${encodeURIComponent(relativePath)}`,
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
    boardEvents = new Subject<ProjectBoard>();
    boardStreamEvents = new Subject<BoardStreamEvent>();
    board = createBoard();
    await TestBed.configureTestingModule({
      imports: [FeatureLibraryPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: projectApi },
        { provide: WorkItemApiService, useValue: boardApi },
        { provide: RuntimeHealthService, useValue: runtimeHealth },
        { provide: NotificationService, useValue: notifications },
        { provide: ThemeService, useValue: theme },
        { provide: AppUpdateService, useValue: updates },
        { provide: DesktopWindowService, useValue: desktop },
      ],
    }).compileComponents();
  });

  it('builds a feature-centric view from backlog, plan, tasks, artifacts, and history', async () => {
    const fixture = TestBed.createComponent(FeatureLibraryPage);
    await fixture.componentInstance.ngOnInit();
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelectorAll('.feature-index-row')).toHaveLength(2);
    expect(root.textContent).toContain('Feature Library');
    expect(root.textContent).toContain('FEAT-001');
    expect(root.textContent).not.toContain('BUG-001');
    expect(root.querySelector('.feature-dossier-copy h2')?.textContent).toContain('Feature Library');
    expect(root.textContent).toContain('DSN-001');
    expect(root.textContent).toContain('DEV-001');
    expect(root.textContent).toContain('QA-001');
    expect(root.textContent).toContain('Plan approved');
    expect(root.querySelector('.feature-artifacts img')?.getAttribute('src')).toBe(
      '/api/tasks/task-design/artifacts/artifact-overview',
    );
    expect(root.querySelector('.feature-design-source-thumbnail iframe')).toBeNull();
    expect(root.querySelector<HTMLAnchorElement>('.feature-design-source-link')?.getAttribute('href')).toContain(
      '/api/tasks/task-design/design-source',
    );
  });

  it('searches and filters the feature index without including non-feature tickets', async () => {
    const fixture = TestBed.createComponent(FeatureLibraryPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();

    component.search.set('webhooks');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelectorAll('.feature-index-row')).toHaveLength(1);
    expect(fixture.nativeElement.textContent).toContain('Configurable webhooks');

    component.search.set('');
    component.setFilter('shipped');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelectorAll('.feature-index-row')).toHaveLength(1);
    expect(fixture.nativeElement.textContent).toContain('Feature Library');
  });

  it('changes the dossier when another feature is selected', async () => {
    const fixture = TestBed.createComponent(FeatureLibraryPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    component.selectFeature(component.features()[1]);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.feature-dossier-copy h2')?.textContent).toContain(
      'Configurable webhooks',
    );
    expect(fixture.nativeElement.textContent).toContain('No delivery tickets yet');
  });

  it('exposes full text tooltips for truncated Feature Library content', async () => {
    const fixture = TestBed.createComponent(FeatureLibraryPage);
    await fixture.componentInstance.ngOnInit();
    fixture.detectChanges();

    const root = fixture.nativeElement as HTMLElement;
    const rowTitles = Array.from(root.querySelectorAll('.feature-index-row')).map((row) => row.getAttribute('title'));
    expect(rowTitles).toContain('Feature Library');
    expect(root.querySelector('.feature-dossier-copy h2')?.getAttribute('title')).toBe('Feature Library');
    expect(root.querySelector('.feature-dossier-copy p')?.getAttribute('title')).toBe(
      'Create a readable home for product feature history and delivery work.',
    );
    expect(root.querySelector('.feature-ticket summary strong')?.getAttribute('title')).toBe(
      '[Designer] Design Feature Library',
    );
  });

  it('keeps the library synchronized with live Workboard events', async () => {
    const fixture = TestBed.createComponent(FeatureLibraryPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    boardStreamEvents.next({
      state: 'connected',
      board: {
      ...board,
      backlog: [
        ...board.backlog,
        {
          id: 'backlog-feature-3',
          requestId: 'request-feature-3',
          key: 'FEAT-003',
          type: 'feature',
          source: 'PM request',
          title: 'Git integration',
          description: 'Connect feature delivery with repository changes.',
          deliveryTarget: 'fullstack',
          requiresUI: true,
          acceptanceCriteria: [],
          attachments: [],
          status: 'backlog',
          createdAt: '2026-08-28T03:00:00Z',
          updatedAt: '2026-08-28T03:00:00Z',
        },
      ],
      },
    });
    fixture.detectChanges();

    expect(component.features()).toHaveLength(3);
    expect(fixture.nativeElement.textContent).toContain('FEAT-003');
    expect(component.selectedFeature()?.item.key).toBe('FEAT-001');
    expect(component.streamState()).toBe('live');
    expect(fixture.nativeElement.textContent).toContain('Live via SSE');

    boardStreamEvents.next({ state: 'reconnecting' });
    fixture.detectChanges();

    expect(component.features()).toHaveLength(3);
    expect(component.streamState()).toBe('reconnecting');
    expect(fixture.nativeElement.textContent).toContain('Reconnecting');
  });
});

function createBoard(): ProjectBoard {
  return {
    id: 'board-1',
    projectId: 'project-1',
    projectName: 'ProductCrew',
    backlog: [
      {
        id: 'backlog-feature-1',
        requestId: 'request-feature-1',
        key: 'FEAT-001',
        type: 'feature',
        source: 'PM request',
        title: 'Feature Library',
        description: 'Create a readable home for product feature history and delivery work.',
        deliveryTarget: 'fullstack',
        requiresUI: true,
        acceptanceCriteria: ['Features remain linked to their delivery tickets'],
        attachments: [],
        status: 'done',
        planId: 'plan-feature-1',
        createdAt: '2026-08-20T00:00:00Z',
        updatedAt: '2026-08-27T08:00:00Z',
      },
      {
        id: 'backlog-feature-2',
        requestId: 'request-feature-2',
        key: 'FEAT-002',
        type: 'feature',
        source: 'PM request',
        title: 'Configurable webhooks',
        description: 'Send board events to external automation.',
        deliveryTarget: 'backend',
        requiresUI: false,
        acceptanceCriteria: [],
        attachments: [],
        status: 'backlog',
        createdAt: '2026-08-21T00:00:00Z',
        updatedAt: '2026-08-21T00:00:00Z',
      },
      {
        id: 'backlog-bug-1',
        requestId: 'request-bug-1',
        key: 'BUG-001',
        type: 'bug',
        source: 'qa',
        title: 'Feature view clips a title',
        description: 'A QA bug should not become a top-level feature.',
        deliveryTarget: 'frontend',
        requiresUI: true,
        acceptanceCriteria: [],
        attachments: [],
        status: 'backlog',
        createdAt: '2026-08-22T00:00:00Z',
        updatedAt: '2026-08-22T00:00:00Z',
      },
    ],
    plans: [
      {
        id: 'plan-feature-1',
        request: {
          id: 'request-feature-1',
          title: 'Feature Library',
          description: 'Create a readable home for product feature history and delivery work.',
          workType: 'feature',
          deliveryTarget: 'fullstack',
          requiresUI: true,
          acceptanceCriteria: ['Features remain linked to their delivery tickets'],
          attachments: [],
          createdAt: '2026-08-20T00:00:00Z',
        },
        status: 'completed',
        revision: 1,
        summary: 'Build a master-detail feature library.',
        documents: [
          {
            id: 'document-1',
            planId: 'plan-feature-1',
            title: 'Feature Library specification',
            kind: 'product-spec',
            audience: ['designer', 'developer', 'qa'],
            content: '# Feature Library\nKeep the Workboard focused on execution.',
            version: 1,
            createdAt: '2026-08-20T02:00:00Z',
          },
        ],
        reviews: [
          {
            decision: 'approve',
            reviewer: 'PM',
            revision: 1,
            createdAt: '2026-08-20T03:00:00Z',
          },
        ],
        createdAt: '2026-08-20T01:00:00Z',
        updatedAt: '2026-08-20T03:00:00Z',
      },
    ],
    tasks: [
      {
        id: 'task-design',
        planId: 'plan-feature-1',
        key: 'DSN-001',
        title: '[Designer] Design Feature Library',
        role: 'designer',
        column: 'done',
        status: 'completed',
        priority: 'high',
        description: 'Create the approved master-detail design.',
        acceptanceCriteria: ['Mockup is approved'],
        dependencyIds: [],
        documentIds: ['document-1'],
        sequenceOrder: 1,
        execution: {
          verdict: 'passed',
          summary: 'Approved Feature Library design.',
          changedFiles: ['.productcrew/design-artifacts/task-design/overview.html'],
          verification: ['Rendered overview.png'],
          remainingRisks: [],
          artifacts: [
            {
              id: 'artifact-overview',
              title: 'Overview',
              kind: 'mockup',
              relativePath: '.productcrew/design-artifacts/task-design/overview.png',
              mediaType: 'image/png',
              width: 1440,
              height: 900,
            },
          ],
          completedAt: '2026-08-21T00:00:00Z',
        },
        createdAt: '2026-08-20T04:00:00Z',
        updatedAt: '2026-08-21T00:00:00Z',
      },
      {
        id: 'task-dev',
        planId: 'plan-feature-1',
        key: 'DEV-001',
        title: '[Developer] Implement Feature Library',
        role: 'developer',
        column: 'done',
        status: 'completed',
        priority: 'high',
        description: 'Implement the feature view.',
        acceptanceCriteria: ['Feature data stays synchronized'],
        dependencyIds: ['task-design'],
        documentIds: ['document-1'],
        sequenceOrder: 2,
        execution: {
          verdict: 'passed',
          summary: 'Feature view implemented.',
          changedFiles: ['frontend/src/app/features/feature-library/feature-library-page.ts'],
          verification: ['Frontend tests pass'],
          remainingRisks: [],
          completedAt: '2026-08-25T00:00:00Z',
        },
        createdAt: '2026-08-20T04:00:00Z',
        updatedAt: '2026-08-25T00:00:00Z',
      },
      {
        id: 'task-qa',
        planId: 'plan-feature-1',
        key: 'QA-001',
        title: '[QA] Verify Feature Library',
        role: 'qa',
        column: 'done',
        status: 'completed',
        priority: 'high',
        description: 'Verify feature grouping and artifacts.',
        acceptanceCriteria: ['Feature view passes QA'],
        dependencyIds: ['task-dev'],
        documentIds: ['document-1'],
        sequenceOrder: 3,
        execution: {
          verdict: 'passed',
          summary: 'Feature Library passed QA.',
          changedFiles: [],
          verification: ['All acceptance criteria pass'],
          remainingRisks: [],
          completedAt: '2026-08-27T08:00:00Z',
        },
        createdAt: '2026-08-20T04:00:00Z',
        updatedAt: '2026-08-27T08:00:00Z',
      },
    ],
    createdAt: '2026-08-20T00:00:00Z',
    updatedAt: '2026-08-27T08:00:00Z',
  };
}
