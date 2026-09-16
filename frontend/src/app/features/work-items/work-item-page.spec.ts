import { provideHttpClient } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NEVER, Observable, of, Subject } from 'rxjs';
import { vi } from 'vitest';

import { ProjectApiService } from '../../core/project-api.service';
import { BuildProfile, BuildRun, DeployProfile, DeployRun, ProductWorkspace } from '../../core/project.models';
import { GitStatusApiService } from '../../core/git-status-api.service';
import { WorkItemApiService } from '../../core/work-item-api.service';
import { CreateBacklogRequest, ProjectBoard, ReviewPlanRequest } from '../../core/work-item.models';
import { WorkItemPage } from './work-item-page';

describe('WorkItemPage', () => {
  let plannedBacklogItemId = '';
  let removedBacklogItemId = '';
  let boardEvents: Subject<ProjectBoard>;
  let buildRunEvents: Subject<BuildRun>;
  let deployRunEvents: Subject<DeployRun>;
  let deployLogListener: ((level: string, message: string) => void) | undefined;
  let startedBuildActionId = '';
  let startedDeployActionId = '';
  let deployProfileOverride: DeployProfile | null = null;
  let createdBacklogAttachments: File[] = [];
  let createdBacklogRequest: unknown = null;
  let generatedIntakeRequest: unknown = null;
  let reviewedPlanRequest: ReviewPlanRequest | null = null;
  let retriedPlanId = '';
  let restartedTaskId = '';
  let approvedDesignTaskId = '';
  let approvedDesignBoard: ProjectBoard | null = null;
  let submittedDesignFeedbackTaskId = '';
  let submittedDesignFeedbackRequest: { feedback: string; reviewer: string } | null = null;
  let submittedDesignFeedbackBoard: ProjectBoard | null = null;
  let submittedDesignFeedbackResponse: Observable<ProjectBoard> | null = null;
  let ignoredTaskId = '';
  let safeStoppedProjectId = '';
  let continuedProjectId = '';
  let loadedBoardProjectId = '';

  const board: ProjectBoard = {
    id: 'board-1',
    projectId: 'project-1',
    projectName: 'local-app',
    backlog: [
      {
        id: 'backlog-1',
        requestId: 'request-backlog-1',
        key: 'BUG-001',
        type: 'bug',
        source: 'source-scan',
        title: 'Frame discovery picks the wrong game frame',
        description: 'The automation can target a non-game iframe when the page has nested frames.',
        deliveryTarget: 'fullstack',
        requiresUI: false,
        acceptanceCriteria: ['Team Lead produces an implementation-ready plan'],
        attachments: [],
        status: 'backlog',
        createdAt: '2026-08-11T00:00:00Z',
        updatedAt: '2026-08-11T00:00:00Z',
      },
    ],
    plans: [
      {
        id: 'plan-1',
        request: {
          id: 'request-1',
          title: 'Saved search filters',
          description: 'Persist the selected search filters between sessions.',
          workType: 'feature',
          deliveryTarget: 'fullstack',
          requiresUI: true,
          acceptanceCriteria: ['core-flow'],
          attachments: [],
          createdAt: '2026-08-11T00:00:00Z',
        },
        status: 'approved',
        revision: 1,
        summary: 'Implement persistence and a visible restore interaction.',
        documents: [],
        reviews: [],
        createdAt: '2026-08-11T00:00:00Z',
        updatedAt: '2026-08-11T00:00:00Z',
      },
    ],
    tasks: [
      {
        id: 'task-1',
        planId: 'plan-1',
        key: 'DEV-001',
        title: '[Developer] Implement saved search filters',
        role: 'developer',
        column: 'planning',
        status: 'planned',
        priority: 'high',
        description: 'Implement the approved behavior and automated tests.',
        acceptanceCriteria: ['Saved filters are restored'],
        dependencyIds: [],
        documentIds: [],
        createdAt: '2026-08-11T00:00:00Z',
        updatedAt: '2026-08-11T00:00:00Z',
      },
    ],
    createdAt: '2026-08-11T00:00:00Z',
    updatedAt: '2026-08-11T00:00:00Z',
  };

  const projectApi = {
    listWorkspaces: () => of([] as ProductWorkspace[]),
    createWorkspace: () => of({} as ProductWorkspace),
    updateWorkspace: () => of({} as ProductWorkspace),
    getSettings: () =>
      of({
        clonePath: 'C:\\Workspaces',
        gitProvider: 'github',
        gitUsername: '',
        githubAccount: '',
        theme: 'light',
        patConfigured: true,
        credentialType: 'OS keyring',
        configPath: 'C:\\Users\\test\\.productcrew\\settings.json',
      }),
    updateTheme: () =>
      of({
        clonePath: 'C:\\Workspaces',
        gitProvider: 'github',
        gitUsername: '',
        githubAccount: '',
        theme: 'dark',
        patConfigured: true,
        credentialType: 'OS keyring',
        configPath: 'C:\\Users\\test\\.productcrew\\settings.json',
      }),
    listProjects: () =>
      of([
        {
          id: 'project-1',
          name: 'local-app',
          path: 'C:\\Projects\\local-app',
          source: 'local',
          branch: 'develop',
          importedAt: '2026-08-11T00:00:00Z',
        },
      ]),
    getBuildProfile: () =>
      of({
        projectId: 'project-1',
        projectName: 'local-app',
        status: 'ready',
        summary: 'Detected 1 local build action.',
        configFiles: ['package.json'],
        selectedActionId: 'check',
        actions: [
          {
            id: 'check',
            label: 'npm run check',
            description: 'Run the full check script.',
            executable: 'npm',
            arguments: ['run', 'check'],
            workingDir: '',
            source: 'package.json',
            recommended: true,
            confidence: 92,
          },
        ],
      } satisfies BuildProfile),
    startBuildRun: (_projectId: string, actionId: string) => {
      startedBuildActionId = actionId;
      return of({
        id: 'build-1',
        projectId: 'project-1',
        actionId,
        actionLabel: 'npm run check',
        command: 'npm run check',
        status: 'queued',
        exitCode: -1,
        logPath: '',
        startedAt: '2026-08-11T00:05:00Z',
        durationMs: 0,
      } satisfies BuildRun);
    },
    watchBuildRun: () => buildRunEvents ?? NEVER,
    getDeployProfile: () =>
      of(
        deployProfileOverride ?? {
          projectId: 'project-1',
          projectName: 'local-app',
          status: 'not_configured',
          configFiles: [],
          actions: [],
        } satisfies DeployProfile,
      ),
    startDeployRun: (_projectId: string, actionId: string) => {
      startedDeployActionId = actionId;
      return of({
        id: 'deploy-1',
        projectId: 'project-1',
        actionId,
        actionLabel: 'npm run deploy:prod',
        command: 'npm run deploy:prod',
        status: 'queued',
        exitCode: -1,
        logPath: '',
        startedAt: '2026-08-11T00:05:00Z',
        durationMs: 0,
      } satisfies DeployRun);
    },
    watchDeployRun: (_projectId: string, _runId: string, onLog?: (level: string, message: string) => void) => {
      deployLogListener = onLog;
      return deployRunEvents ?? NEVER;
    },
  };
  const boardApi = {
    getBoard: (projectId: string) => {
      loadedBoardProjectId = projectId;
      return of({ ...board, projectId });
    },
    watchBoard: () => boardEvents ?? NEVER,
    createPlan: () => of(board),
    createBacklog: (_projectId: string, request: unknown, attachments: File[] = []) => {
      createdBacklogRequest = request;
      createdBacklogAttachments = attachments;
      return of(board);
    },
    requestAttachmentUrl: (_projectId: string, requestId: string, attachmentId: string, download = false) =>
      `/api/requests/${requestId}/attachments/${attachmentId}${download ? '?download=1' : ''}`,
    taskArtifactUrl: (_projectId: string, taskId: string, artifactId: string) =>
      `/api/tasks/${taskId}/artifacts/${artifactId}`,
    generateIntakeQuestions: (_projectId: string, request: unknown) => {
      generatedIntakeRequest = request;
      return of({
        schemaVersion: 1,
        heading: 'Confirm delivery decisions',
        summary: 'Answer the decisions Team Lead needs before planning this request.',
        workType: 'feature' as const,
        source: 'codex',
        questions: [
          {
            id: 'user-experience',
            label: 'Does this change the user experience?',
            helpText: 'Controls whether Designer work is allowed.',
            type: 'toggle' as const,
            binding: 'requiresUI' as const,
            required: true,
            options: [
              { value: 'true', label: 'Yes', description: 'Include Designer work.' },
              { value: 'false', label: 'No', description: 'Engineering only.' },
            ],
            defaultValues: ['true'],
            layout: { span: 12 as const },
          },
          {
            id: 'delivery-surface',
            label: 'Which surface is affected?',
            helpText: 'Choose the implementation boundary.',
            type: 'dropdown' as const,
            binding: 'deliveryTarget' as const,
            required: true,
            options: [
              { value: 'frontend', label: 'Frontend', description: 'Client work.' },
              { value: 'backend', label: 'Backend', description: 'Service work.' },
              { value: 'fullstack', label: 'Full-stack', description: 'Both.' },
            ],
            defaultValues: ['fullstack'],
            layout: { span: 6 as const },
          },
        ],
      });
    },
    planBacklog: (_projectId: string, itemId: string) => {
      plannedBacklogItemId = itemId;
      return of({
        ...board,
        backlog: board.backlog.map((item) =>
          item.id === itemId ? { ...item, status: 'planning' as const, planId: 'plan-2' } : item,
        ),
      });
    },
    removeBacklogItem: (_projectId: string, itemId: string) => {
      removedBacklogItemId = itemId;
      return of({
        ...board,
        backlog: board.backlog.filter((item) => item.id !== itemId),
      });
    },
    reviewPlan: (_projectId: string, _planId: string, request: ReviewPlanRequest) => {
      reviewedPlanRequest = request;
      return of(board);
    },
    retryPlan: (_projectId: string, planId: string) => {
      retriedPlanId = planId;
      return of(board);
    },
    moveTask: () => of(board),
    restartTask: (_projectId: string, taskId: string) => {
      restartedTaskId = taskId;
      return of({
        ...board,
        tasks: board.tasks.map((task) =>
          task.id === taskId
            ? { ...task, column: task.role, status: 'queued' as const, threadId: undefined, blockedReason: undefined, execution: undefined }
            : task,
        ),
      });
    },
    approveDesignTask: (_projectId: string, taskId: string) => {
      approvedDesignTaskId = taskId;
      return of(approvedDesignBoard ?? board);
    },
    submitDesignFeedback: (
      _projectId: string,
      taskId: string,
      request: { feedback: string; reviewer: string },
    ) => {
      submittedDesignFeedbackTaskId = taskId;
      submittedDesignFeedbackRequest = request;
      return submittedDesignFeedbackResponse ?? of(submittedDesignFeedbackBoard ?? board);
    },
    ignoreTask: (_projectId: string, taskId: string) => {
      ignoredTaskId = taskId;
      return of({
        ...board,
        tasks: board.tasks.map((task) =>
          task.id === taskId
            ? {
                ...task,
                column: 'done' as const,
                status: 'completed' as const,
                blockedReason: undefined,
                execution: {
                  verdict: 'ignored' as const,
                  summary: 'PM ignored this blocked task and accepted the workflow risk.',
                  changedFiles: [],
                  verification: [],
                  remainingRisks: ['Codex usage limit reached'],
                  completedAt: '2026-08-24T03:00:00Z',
                },
              }
            : task,
        ),
      });
    },
    safeStopQueue: (projectId: string) => {
      safeStoppedProjectId = projectId;
      return of({
        ...board,
        activeTaskId: 'task-1',
        tasks: board.tasks.map((task) => task.id === 'task-1' ? { ...task, column: 'developer' as const, status: 'in_progress' as const } : task),
        queueControl: { status: 'stopping' as const, requestedAt: '2026-08-11T00:10:00Z', updatedAt: '2026-08-11T00:10:00Z' },
      });
    },
    continueQueue: (projectId: string) => {
      continuedProjectId = projectId;
      return of({
        ...board,
        queueControl: undefined,
      });
    },
    watchPlanningSession: () => NEVER,
    watchTaskSession: () => NEVER,
  };

  const gitApi = {
    getStatus: () => of({ status: 'ok' as const, branch: 'main', dirty: [] }),
    getCommits: () => of({ status: 'ok' as const, commits: [] }),
    linkTaskGitReference: () => of(board),
    clearTaskGitReference: () => of(board),
    proposeTaskGitBranch: () => of({ status: 'proposed' as const, branchName: 'task/DEV-001-example' }),
    createTaskGitBranch: () => of({ status: 'created' as const, branchName: 'task/DEV-001-example' }),
  };

  beforeEach(async () => {
    localStorage.clear();
    plannedBacklogItemId = '';
    removedBacklogItemId = '';
    startedBuildActionId = '';
    startedDeployActionId = '';
    deployProfileOverride = null;
    deployLogListener = undefined;
    createdBacklogAttachments = [];
    createdBacklogRequest = null;
    generatedIntakeRequest = null;
    reviewedPlanRequest = null;
    retriedPlanId = '';
    restartedTaskId = '';
    approvedDesignTaskId = '';
    approvedDesignBoard = null;
    submittedDesignFeedbackTaskId = '';
    submittedDesignFeedbackRequest = null;
    submittedDesignFeedbackBoard = null;
    submittedDesignFeedbackResponse = null;
    ignoredTaskId = '';
    safeStoppedProjectId = '';
    continuedProjectId = '';
    loadedBoardProjectId = '';
    boardEvents = new Subject<ProjectBoard>();
    buildRunEvents = new Subject<BuildRun>();
    deployRunEvents = new Subject<DeployRun>();
    await TestBed.configureTestingModule({
      imports: [WorkItemPage],
      providers: [
        provideHttpClient(),
        provideRouter([]),
        { provide: ProjectApiService, useValue: projectApi },
        { provide: WorkItemApiService, useValue: boardApi },
        { provide: GitStatusApiService, useValue: gitApi },
      ],
    }).compileComponents();
  });

  it('loads the independent board for the selected project', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;

    await component.ngOnInit();

    expect(component.selectedProject()?.name).toBe('local-app');
    expect(component.board()?.projectId).toBe('project-1');
  });

  it('opens Git status and delivery in a separate modal without inserting a board panel', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;

    await component.ngOnInit();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.board-git-panel')).toBeNull();
    expect(fixture.nativeElement.querySelector('.kanban-board')).not.toBeNull();

    const launcher = fixture.nativeElement.querySelector('.git-delivery-launcher') as HTMLButtonElement;
    expect(launcher).not.toBeNull();
    launcher.click();
    fixture.detectChanges();

    expect(component.gitDeliveryOpen()).toBe(true);
    expect(fixture.nativeElement.querySelector('app-git-delivery-modal')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.board-git-panel')).toBeNull();
  });

  it('loads a workspace board without changing the global project selection', async () => {
    const workspace: ProductWorkspace = {
      schemaVersion: 1,
      id: 'workspace-platform',
      name: 'Platform workspace',
      folders: [
        { projectId: 'project-1', name: 'local-app', path: 'C:\\Projects\\local-app', access: 'read-write', primary: true },
        { projectId: 'project-2', name: 'service', path: 'C:\\Projects\\service', access: 'read-write' },
      ],
      settings: { queueMode: 'serial' },
      filePath: 'C:\\Users\\test\\.productcrew\\workspaces\\workspace-platform\\platform.workspace',
      createdAt: '2026-09-10T00:00:00Z',
      updatedAt: '2026-09-10T00:00:00Z',
    };
    vi.spyOn(projectApi, 'listWorkspaces').mockReturnValueOnce(of([workspace]));
    localStorage.setItem('productcrew.work-board-mode', 'workspace');
    localStorage.setItem('productcrew.selected-workspace-id', workspace.id);
    const component = TestBed.createComponent(WorkItemPage).componentInstance;

    await component.ngOnInit();

    expect(component.boardMode()).toBe('workspace');
    expect(component.selectedWorkspace()?.name).toBe('Platform workspace');
    expect(loadedBoardProjectId).toBe(workspace.id);
    expect(component.projects()[0].id).toBe('project-1');
  });

  it('blocks a Developer task from entering the Designer queue', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const task = component.board()!.tasks[0];

    expect(component.canDrop(task, 'designer')).toBe(false);
    expect(component.canDrop(task, 'developer')).toBe(true);
  });

  it('keeps a build-verifying task active and non-draggable', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const task = { ...component.board()!.tasks[0], column: 'developer' as const, status: 'verifying' as const };

    expect(component.statusLabel(task.status)).toBe('Build verifying');
    expect(component.canDrag(task)).toBe(false);
    expect(component.canDrop(task, 'planning')).toBe(false);
  });

  it('keeps task card quick actions icon-only with custom tooltips', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const baseTask = component.board()!.tasks[0];
    const verifyingTask = {
      ...baseTask,
      id: 'task-build',
      key: 'DEV-101',
      column: 'developer' as const,
      status: 'verifying' as const,
      sequenceOrder: 1,
      documentIds: ['doc-1', 'doc-2'],
    };
    const queuedTask = {
      ...baseTask,
      id: 'task-queued',
      key: 'DEV-102',
      column: 'developer' as const,
      status: 'queued' as const,
    };
    const lockedPlan = { ...component.board()!.plans[0], id: 'plan-locked', status: 'awaiting_approval' as const };
    const lockedTask = {
      ...baseTask,
      id: 'task-locked',
      planId: lockedPlan.id,
      key: 'DEV-103',
      column: 'planning' as const,
      status: 'planned' as const,
    };
    component.board.set({
      ...component.board()!,
      plans: [...component.board()!.plans, lockedPlan],
      tasks: [verifyingTask, queuedTask, lockedTask],
      backlog: component.board()!.backlog.map((item) => ({ ...item, repeatReports: 1 })),
    });
    fixture.detectChanges();

    const buildLogHint = fixture.nativeElement.querySelector(
      '#task-card-task-build .live-log-hint',
    ) as HTMLElement;
    expect(buildLogHint.getAttribute('data-tooltip')).toBe('Open task to view the build verification log');
    expect(buildLogHint.getAttribute('aria-label')).toBe('Open build verification log');
    expect(buildLogHint.hasAttribute('title')).toBe(false);
    expect(buildLogHint.textContent).not.toContain('Build log');

    const sequenceOrder = fixture.nativeElement.querySelector(
      '#task-card-task-build .task-card-tooltip.task-key',
    ) as HTMLElement;
    expect(sequenceOrder.getAttribute('data-tooltip')).toBe('Plan sequence order');
    expect(sequenceOrder.hasAttribute('title')).toBe(false);

    const documentCount = fixture.nativeElement.querySelector(
      '#task-card-task-build .attachment-count',
    ) as HTMLElement;
    expect(documentCount.getAttribute('data-tooltip')).toBe('2 linked documents');
    expect(documentCount.hasAttribute('title')).toBe(false);

    const queueButton = fixture.nativeElement.querySelector(
      '#task-card-task-queued .queue-task-button',
    ) as HTMLButtonElement;
    expect(queueButton.getAttribute('data-tooltip')).toBe('Re-queue this task');
    expect(queueButton.getAttribute('aria-label')).toBe('Re-queue this task');
    expect(queueButton.hasAttribute('title')).toBe(false);
    expect(queueButton.textContent).not.toContain('Re-queue');
    expect(queueButton.textContent).not.toContain('Queue');

    const priorityDot = fixture.nativeElement.querySelector(
      '#task-card-task-queued .priority-dot',
    ) as HTMLElement;
    expect(priorityDot.getAttribute('data-tooltip')).toBe('high priority');
    expect(priorityDot.hasAttribute('title')).toBe(false);

    const taskDragHandle = fixture.nativeElement.querySelector(
      '#task-card-task-queued .drag-handle',
    ) as HTMLElement;
    expect(taskDragHandle.getAttribute('data-tooltip')).toBe('Drag task');
    expect(taskDragHandle.hasAttribute('title')).toBe(false);

    const lockedIcon = fixture.nativeElement.querySelector(
      '#task-card-task-locked .task-icon-action[aria-label="Planning approval required"]',
    ) as HTMLElement;
    expect(lockedIcon.getAttribute('data-tooltip')).toBe('Planning approval required');
    expect(lockedIcon.hasAttribute('title')).toBe(false);

    const backlogCard = fixture.nativeElement.querySelector(
      '#backlog-card-backlog-1',
    ) as HTMLElement;
    expect(backlogCard.querySelector('.task-status')).toBeNull();
    expect(backlogCard.textContent).not.toContain('Ready for planning');

    const repeatChip = backlogCard.querySelector('.repeat-report-chip') as HTMLElement;
    expect(repeatChip.getAttribute('data-tooltip')).toBe('QA reproduced this existing bug again');
    expect(repeatChip.hasAttribute('title')).toBe(false);

    const backlogPlanButton = backlogCard.querySelector('.backlog-plan-button') as HTMLButtonElement;
    expect(backlogPlanButton.getAttribute('data-tooltip')).toBe('Send this item to Team Lead planning');
    expect(backlogPlanButton.getAttribute('aria-label')).toBe('Plan item BUG-001');
    expect(backlogPlanButton.hasAttribute('title')).toBe(false);
    expect(backlogPlanButton.textContent).not.toContain('Plan');

    const backlogRemoveButton = backlogCard.querySelector('.backlog-remove-button') as HTMLButtonElement;
    expect(backlogRemoveButton.getAttribute('data-tooltip')).toBe('Remove this backlog item');
    expect(backlogRemoveButton.getAttribute('aria-label')).toBe('Remove item BUG-001');
    expect(backlogRemoveButton.hasAttribute('title')).toBe(false);
    expect(backlogRemoveButton.textContent).not.toContain('Remove');

    const backlogDragHandle = backlogCard.querySelector('.drag-handle') as HTMLElement;
    expect(backlogDragHandle.getAttribute('data-tooltip')).toBe('Drag to Team Lead');
    expect(backlogDragHandle.hasAttribute('title')).toBe(false);

    vi.spyOn(backlogPlanButton, 'getBoundingClientRect').mockReturnValue({
      x: 4,
      y: 280,
      width: 26,
      height: 24,
      top: 280,
      right: 30,
      bottom: 304,
      left: 4,
      toJSON: () => ({}),
    } as DOMRect);
    component.showCardTooltip({ target: backlogPlanButton } as unknown as MouseEvent);
    fixture.detectChanges();
    const tooltip = fixture.nativeElement.querySelector('.card-tooltip-overlay') as HTMLElement;
    expect(tooltip.textContent).toBe('Send this item to Team Lead planning');
    expect(Number.parseFloat(tooltip.style.left)).toBeGreaterThan(100);
    expect(tooltip.style.top).toBe('270px');
    expect(tooltip.classList.contains('below')).toBe(false);
  });

  it('approves and schedules a valid ordered plan sequence', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const plan = { ...component.board()!.plans[0], status: 'awaiting_approval' as const };
    const baseTask = component.board()!.tasks[0];
    const tasks = [
      { ...baseTask, id: 'design', key: 'DSN-001', role: 'designer' as const },
      { ...baseTask, id: 'develop', key: 'DEV-001', role: 'developer' as const },
      { ...baseTask, id: 'qa', key: 'QA-001', role: 'qa' as const },
    ];
    component.board.set({
      ...component.board()!,
      plans: [plan],
      tasks,
      backlog: component.board()!.backlog.map((item) => ({
        ...item,
        source: 'qa',
        planId: plan.id,
        status: 'planning' as const,
      })),
    });

    expect(component.canRunSequence(plan)).toBe(true);
    await component.approveAndRunSequence(plan);

    expect(reviewedPlanRequest).toMatchObject({ decision: 'approve', runSequence: true });
  });

  it('allows a UI-flagged bug plan to run without a Designer task when Team Lead scoped it to Developer and QA', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const plan = {
      ...component.board()!.plans[0],
      status: 'awaiting_approval' as const,
      request: { ...component.board()!.plans[0].request, requiresUI: true },
    };
    const baseTask = component.board()!.tasks[0];
    component.board.set({
      ...component.board()!,
      plans: [plan],
      tasks: [
        { ...baseTask, id: 'develop-fix', key: 'DEV-001', role: 'developer' as const },
        { ...baseTask, id: 'develop-test', key: 'DEV-002', role: 'developer' as const },
        { ...baseTask, id: 'qa', key: 'QA-001', role: 'qa' as const },
      ],
    });

    expect(component.canRunSequence(plan)).toBe(true);
  });

  it('runs the configured project build action from the board header', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();

    await component.runProjectBuild();

    expect(startedBuildActionId).toBe('check');
    expect(component.activeBuildRun()?.actionLabel).toBe('npm run check');
  });

  it('keeps board header actions compact while preserving details in tooltips', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();
    const nativeElement = fixture.nativeElement as HTMLElement;

    const cards = Array.from(nativeElement.querySelectorAll<HTMLElement>('.board-actions .board-action-card'));
    expect(cards).toHaveLength(4);

    const queueAction = nativeElement.querySelector<HTMLElement>('.board-queue-control .board-icon-action')!;
    expect(queueAction.textContent).not.toContain('Safe stop');
    expect(queueAction.getAttribute('data-tooltip')).toContain('Pause this project queue');

    const buildCard = nativeElement.querySelector<HTMLElement>('.board-build-control')!;
    expect(buildCard.textContent).not.toContain('npm run check');
    expect(buildCard.getAttribute('data-tooltip')).toContain('npm run check');
    expect(nativeElement.querySelector<HTMLElement>('.board-build-control strong')?.textContent?.trim()).toBe('Configured');

    const deployConfigureAction = nativeElement.querySelector<HTMLElement>('.board-deploy-control .board-icon-action')!;
    expect(deployConfigureAction.textContent).not.toContain('Configure');
    expect(deployConfigureAction.getAttribute('data-tooltip')).toContain('Configure a deploy action');

    buildCard.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    fixture.detectChanges();

    expect(nativeElement.querySelector<HTMLElement>('.card-tooltip-overlay')?.textContent).toContain('npm run check');
  });

  it('shows the build result only after completion and dismisses it after 5 seconds', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    await component.runProjectBuild();

    expect(component.activeBuildRun()?.status).toBe('queued');
    expect(component.visibleBuildRun()).toBeNull();

    vi.useFakeTimers();
    buildRunEvents.next({
      id: 'build-1',
      projectId: 'project-1',
      actionId: 'check',
      actionLabel: 'npm run check',
      command: 'npm run check',
      status: 'passed',
      exitCode: 0,
      reason: 'npm run check completed successfully.',
      logPath: '',
      startedAt: '2026-08-11T00:05:00Z',
      completedAt: '2026-08-11T00:05:10Z',
      durationMs: 10_000,
    });

    expect(component.visibleBuildRun()?.status).toBe('passed');

    vi.advanceTimersByTime(4_999);
    expect(component.visibleBuildRun()?.status).toBe('passed');

    vi.advanceTimersByTime(1);

    expect(component.visibleBuildRun()).toBeNull();
    vi.useRealTimers();
  });

  it('keeps the deploy control disabled and links to Settings when no deploy action is configured', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();

    expect(component.deployControlState()).toBe('unconfigured');
    expect(component.selectedDeployAction()).toBeNull();
    component.openDeployConfirm();
    expect(component.deployConfirmOpen()).toBe(false);
  });

  it('requires acknowledgment before confirming a deploy, then starts the run and streams live logs', async () => {
    deployProfileOverride = {
      projectId: 'project-1',
      projectName: 'local-app',
      status: 'ready',
      configFiles: ['package.json'],
      selectedActionId: 'deploy-prod',
      actions: [
        {
          id: 'deploy-prod',
          label: 'npm run deploy:prod',
          description: 'Deploy to production.',
          executable: 'npm',
          arguments: ['run', 'deploy:prod'],
          workingDir: '',
          source: 'package.json',
          recommended: true,
          confidence: 90,
        },
      ],
    };
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();

    expect(component.deployControlState()).toBe('configured');

    component.openDeployConfirm();
    expect(component.deployConfirmOpen()).toBe(true);

    await component.confirmDeploy();
    expect(startedDeployActionId).toBe('');
    expect(component.activeDeployRun()).toBeNull();

    component.deployAcknowledged.set(true);
    await component.confirmDeploy();

    expect(startedDeployActionId).toBe('deploy-prod');
    expect(component.deployConfirmOpen()).toBe(false);
    expect(component.deployControlState()).toBe('running');
    expect(component.deployLogViewerOpen()).toBe(true);

    deployLogListener?.('stdout', 'Building production bundle...');
    expect(component.deployLogs()).toEqual([{ level: 'stdout', message: 'Building production bundle...', at: expect.any(Number) }]);

    deployRunEvents.next({
      id: 'deploy-1',
      projectId: 'project-1',
      actionId: 'deploy-prod',
      actionLabel: 'npm run deploy:prod',
      command: 'npm run deploy:prod',
      status: 'passed',
      exitCode: 0,
      reason: 'npm run deploy:prod completed successfully.',
      logPath: 'C:\\logs\\deploy-1.log',
      startedAt: '2026-08-11T00:05:00Z',
      completedAt: '2026-08-11T00:05:12Z',
      durationMs: 12_000,
    });

    expect(component.deployControlState()).toBe('passed');
    expect(component.activeDeployRun()?.exitCode).toBe(0);
    expect(component.activeDeployRun()?.durationMs).toBe(12_000);
  });

  it('queues dependent tasks and exposes their waiting dependency', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const prerequisite = {
      ...component.board()!.tasks[0],
      id: 'task-prerequisite',
      key: 'DEV-007',
      column: 'developer' as const,
      status: 'in_progress' as const,
    };
    const dependent = {
      ...component.board()!.tasks[0],
      id: 'task-dependent',
      key: 'DEV-008',
      dependencyIds: [prerequisite.id],
    };
    component.board.set({ ...component.board()!, tasks: [prerequisite, dependent] });

    expect(component.canDrop(dependent, 'developer')).toBe(true);
    component.board.set({
      ...component.board()!,
      tasks: [prerequisite, { ...dependent, column: 'developer', status: 'queued' }],
    });
    expect(component.waitingDependencyKeys(component.board()!.tasks[1])).toEqual(['DEV-007']);
  });

  it('keeps completed manual Designer handoffs in PM review until approved from the drawer', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const designReview = {
      ...component.board()!.tasks[0],
      id: 'design-review',
      key: 'DSN-001',
      title: '[Designer] Design saved search filters',
      role: 'designer' as const,
      column: 'designer' as const,
      status: 'design_review' as const,
      execution: {
        summary: 'Mockup and handoff are ready for PM review.',
        changedFiles: ['.productcrew/design-artifacts/design-review/handoff.md'],
        verification: ['Rendered overview.png'],
        remainingRisks: [],
        artifacts: [
          {
            id: 'overview',
            title: 'Overview',
            kind: 'png',
            relativePath: '.productcrew/design-artifacts/design-review/overview.png',
            mediaType: 'image/png',
            width: 1440,
            height: 900,
          },
        ],
        completedAt: '2026-08-11T00:05:00Z',
      },
    };
    component.board.set({ ...component.board()!, tasks: [designReview] });
    fixture.detectChanges();

    expect(component.statusLabel(designReview.status)).toBe('Design review');
    expect(component.canDrag(designReview)).toBe(false);
    expect(component.canQueue(designReview)).toBe(false);
    expect(fixture.nativeElement.querySelector('.task-card .approve-design-button')).toBeNull();

    component.openTask(designReview);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.design-approval-gate')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.task-header-actions .approve-design-button')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.design-approval-gate .approve-design-button')).toBeNull();
    approvedDesignBoard = {
      ...component.board()!,
      tasks: [{ ...designReview, column: 'done' as const, status: 'completed' as const }],
    };
    await component.approveDesignTask(new Event('click'), designReview);

    expect(approvedDesignTaskId).toBe(designReview.id);
    expect(component.taskById(designReview.id)?.status).toBe('completed');
    expect(component.selectedTask()).toBeNull();
  });

  it('shows Designer feedback controls only for manual design review tasks', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const baseTask = component.board()!.tasks[0];
    const manualReview = {
      ...baseTask,
      id: 'design-review-manual',
      key: 'DSN-002',
      role: 'designer' as const,
      column: 'designer' as const,
      status: 'design_review' as const,
      execution: {
        summary: 'Manual review is ready.',
        changedFiles: ['.productcrew/design-artifacts/design-review-manual/handoff.md'],
        verification: ['Rendered overview.png'],
        remainingRisks: [],
        completedAt: '2026-08-11T00:06:00Z',
      },
    };
    const sequenceReview = {
      ...manualReview,
      id: 'design-review-sequence',
      key: 'DSN-003',
      sequenceOrder: 1,
    };

    component.board.set({ ...component.board()!, tasks: [manualReview, sequenceReview] });
    component.openTask(manualReview);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.task-header-actions .approve-design-button')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.task-header-actions .feedback-design-button')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.design-approval-gate .feedback-design-button')).toBeNull();

    component.openTask(sequenceReview);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.design-approval-gate')).toBeNull();
    expect(fixture.nativeElement.querySelector('.design-revision-context')).toBeNull();
  });

  it('requires PM feedback text, shows revision context, and requeues the same Designer task', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const designReview = {
      ...component.board()!.tasks[0],
      id: 'design-review-feedback',
      key: 'DSN-004',
      title: '[Designer] Revise saved search filters',
      role: 'designer' as const,
      column: 'designer' as const,
      status: 'design_review' as const,
      execution: {
        summary: 'Updated mockups and handoff are ready for another PM decision.',
        changedFiles: ['.productcrew/design-artifacts/design-review-feedback/handoff.md'],
        verification: ['Rendered overview.png'],
        remainingRisks: [],
        artifacts: [
          {
            id: 'overview',
            title: 'Overview',
            kind: 'png',
            relativePath: '.productcrew/design-artifacts/design-review-feedback/overview.png',
            mediaType: 'image/png',
            width: 1440,
            height: 900,
          },
        ],
        completedAt: '2026-08-11T00:10:00Z',
      },
      revisionHistory: [
        {
          revision: 1,
          feedback: 'Tighten the filter chip spacing and keep the existing HTML file.',
          reviewer: 'PM',
          requestedAt: '2026-08-11T00:08:00Z',
          execution: {
            summary: 'Initial mockup established the review structure.',
            changedFiles: ['.productcrew/design-artifacts/design-review-feedback/overview.html'],
            verification: ['Rendered overview.png'],
            remainingRisks: [],
            completedAt: '2026-08-11T00:05:00Z',
          },
        },
      ],
    };
    component.board.set({ ...component.board()!, tasks: [designReview] });
    component.openTask(designReview);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.delivery-report')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.design-revision-context')?.textContent).toContain('Revision context');
    expect(fixture.nativeElement.querySelector('.design-revision-context')?.textContent).toContain(
      'Tighten the filter chip spacing and keep the existing HTML file.',
    );

    fixture.nativeElement.querySelector('.feedback-design-button').click();
    fixture.detectChanges();

    const approveButton = fixture.nativeElement.querySelector('.approve-design-button') as HTMLButtonElement;
    const confirmButton = fixture.nativeElement.querySelector('.feedback-confirm') as HTMLButtonElement;
    const textarea = fixture.nativeElement.querySelector('#design-feedback-textarea') as HTMLTextAreaElement;

    expect(approveButton.disabled).toBe(true);
    expect(confirmButton.disabled).toBe(true);

    textarea.value = '  no ';
    textarea.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    await component.submitDesignFeedback(new Event('click'), designReview);

    expect(component.errorMessage()).toBe('Explain what the Designer should revise before sending feedback.');
    expect(component.designFeedbackError()).toBe('Explain what the Designer should revise before sending feedback.');
    expect(submittedDesignFeedbackTaskId).toBe('');

    component.errorMessage.set(null);
    textarea.value = 'Tighten the filter chip spacing and preserve the current artifact files in place.';
    textarea.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    const response = new Subject<ProjectBoard>();
    submittedDesignFeedbackResponse = response;
    submittedDesignFeedbackBoard = {
      ...component.board()!,
      tasks: [
        {
          ...designReview,
          status: 'queued' as const,
          execution: undefined,
        },
      ],
    };

    const pending = component.submitDesignFeedback(new Event('click'), designReview);
    fixture.detectChanges();

    expect(component.saving()).toBe(true);
    expect((fixture.nativeElement.querySelector('.feedback-confirm') as HTMLButtonElement).disabled).toBe(true);
    expect((fixture.nativeElement.querySelector('.feedback-design-button') as HTMLButtonElement).disabled).toBe(true);

    response.next(submittedDesignFeedbackBoard);
    response.complete();
    await pending;
    fixture.detectChanges();

    expect(submittedDesignFeedbackTaskId).toBe(designReview.id);
    expect(submittedDesignFeedbackRequest).toEqual({
      feedback: 'Tighten the filter chip spacing and preserve the current artifact files in place.',
      reviewer: 'PM',
    });
    expect(component.successMessage()).toBe('DSN-004 sent back to Designer for revision.');
    expect(component.selectedTask()).toBeNull();
  });

  it('restarts a blocked task with a new AI session', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const blocked = {
      ...component.board()!.tasks[0],
      column: 'developer' as const,
      status: 'blocked' as const,
      threadId: 'stale-codex-thread',
      blockedReason: 'Codex usage limit reached',
    };
    component.board.set({ ...component.board()!, tasks: [blocked] });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.task-card .restart-task-button')).toBeNull();
    component.openTask(blocked);
    fixture.detectChanges();

    const restartButton = fixture.nativeElement.querySelector(
      '.task-header-actions .restart-task-button',
    ) as HTMLButtonElement;
    expect(restartButton).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.execution-alert .restart-task-button')).toBeNull();
    await component.restartTask(new Event('click'), blocked);

    expect(restartedTaskId).toBe(blocked.id);
    expect(component.taskById(blocked.id)?.status).toBe('queued');
    expect(component.taskById(blocked.id)?.threadId).toBeUndefined();
  });

  it('renders long blocked reasons inside the compact card text wrapper', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const reason = 'Developer stopped: GitHub Copilot failed to read C:\\Users\\anh.tang\\.productcrew\\copilot-prompts\\copilot-prompt-1872610923456789.md because the path is long.';
    const blocked = {
      ...component.board()!.tasks[0],
      column: 'developer' as const,
      status: 'blocked' as const,
      blockedReason: reason,
    };
    component.board.set({ ...component.board()!, tasks: [blocked] });
    fixture.detectChanges();

    const row = fixture.nativeElement.querySelector('.task-card .blocked-row') as HTMLElement;
    expect(row.querySelector('.material-symbols-rounded')?.textContent?.trim()).toBe('block');
    expect(row.querySelector('.blocked-text')?.textContent).toContain('copilot-prompts');
  });

  it('shows a verify-again quick action only for manual blocked QA tasks', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const baseTask = component.board()!.tasks[0];
    const manualQA = {
      ...baseTask,
      key: 'QA-001',
      role: 'qa' as const,
      column: 'qa' as const,
      status: 'blocked' as const,
      blockedReason: 'QA found 1 unresolved reproducible bug.',
    };
    const sequenceQA = {
      ...manualQA,
      id: 'task-sequence-qa',
      key: 'QA-002',
      sequenceOrder: 3,
    };
    const waitingQA = {
      ...manualQA,
      id: 'task-waiting-qa',
      key: 'QA-003',
      blockedReason: 'QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.',
    };
    component.board.set({ ...component.board()!, tasks: [manualQA, sequenceQA, waitingQA] });
    fixture.detectChanges();

    const verifyButtons = fixture.nativeElement.querySelectorAll(
      '.task-card .manual-qa-verify-button',
    ) as NodeListOf<HTMLButtonElement>;
    expect(verifyButtons.length).toBe(1);
    expect(verifyButtons[0].getAttribute('aria-label')).toBe('Verify this QA bug again');
    expect(verifyButtons[0].getAttribute('data-tooltip')).toBe('Queue QA to verify this bug again');
    expect(verifyButtons[0].hasAttribute('title')).toBe(false);
    verifyButtons[0].click();
    await fixture.whenStable();

    expect(restartedTaskId).toBe(manualQA.id);
  });

  it('presents QA waiting on a planned bug as waiting instead of an error', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const waitingQA = {
      ...component.board()!.tasks[0],
      key: 'QA-001',
      role: 'qa' as const,
      column: 'qa' as const,
      status: 'blocked' as const,
      blockedReason: 'QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.',
    };
    component.board.set({ ...component.board()!, tasks: [waitingQA] });
    fixture.detectChanges();

    const card = fixture.nativeElement.querySelector(
      '.kanban-column[data-column="qa"] .task-card',
    ) as HTMLElement;
    expect(card.classList.contains('waiting-on-planning-bug')).toBe(true);
    expect(card.classList.contains('blocked')).toBe(false);
    expect(card.querySelector('.blocked-row')?.classList.contains('waiting-on-planning-bug')).toBe(true);
    expect(card.querySelector('.task-status')?.getAttribute('data-status')).toBe('waiting');
    expect(card.querySelector('.task-status')?.textContent).toContain('Waiting');
    expect(card.querySelector('.manual-qa-verify-button')).toBeNull();
  });

  it('renders QA findings as linked bug tickets and opens the ticket detail', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const linkedBug = { ...component.board()!.backlog[0], source: 'qa' };
    const qaTask = {
      ...component.board()!.tasks[0],
      id: 'qa-linked',
      key: 'QA-002',
      role: 'qa' as const,
      column: 'qa' as const,
      status: 'blocked' as const,
      blockedReason: 'QA found one reproducible bug.',
      execution: {
        verdict: 'failed' as const,
        summary: 'One bug remains.',
        changedFiles: [],
        verification: ['source inspection passed'],
        remainingRisks: ['Original finding wording'],
        completedAt: '2026-08-11T00:30:00Z',
        findings: [{
          severity: 'high',
          title: 'Original finding wording',
          description: 'QA found the same regression.',
          evidence: 'Reproduced in focused checks.',
          steps: [],
          expected: 'Works',
          actual: 'Fails',
          affectedFiles: [],
          linkedBacklogId: linkedBug.id,
          linkedBacklogKey: linkedBug.key,
          linkedBacklogTitle: linkedBug.title,
          linkedBacklogStatus: linkedBug.status,
        }],
      },
    };
    component.board.set({ ...component.board()!, backlog: [linkedBug], tasks: [qaTask] });
    component.openTask(qaTask);
    fixture.detectChanges();

    const findingSummary = fixture.nativeElement.querySelector('.qa-findings summary strong') as HTMLElement;
    expect(findingSummary.textContent).toContain('BUG-001 · Frame discovery picks the wrong game frame');
    expect(findingSummary.textContent).not.toContain('Original finding wording');
    expect(component.linkedFindingBacklog({
      severity: 'high',
      title: 'Frame discovery picks the wrong game frame',
      description: '',
      evidence: '',
      steps: [],
      expected: '',
      actual: '',
      affectedFiles: [],
    })?.id).toBe(linkedBug.id);

    const openTicket = fixture.nativeElement.querySelector('.qa-finding-ticket-action') as HTMLButtonElement;
    openTicket.click();
    fixture.detectChanges();

    expect(component.selectedBacklog()?.id).toBe(linkedBug.id);
    expect(component.selectedTask()).toBeNull();
  });

  it('offers the right Team Lead follow-up for linked QA bug tickets', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const finding = {
      severity: 'high',
      title: 'Original finding wording',
      description: 'QA found the same regression.',
      evidence: 'Reproduced in focused checks.',
      steps: [],
      expected: 'Works',
      actual: 'Fails',
      affectedFiles: [],
      linkedBacklogId: 'qa-bug-open',
      linkedBacklogKey: 'BUG-005',
      linkedBacklogTitle: '[QA] Existing contract bug',
      linkedBacklogStatus: 'backlog' as const,
    };
    const openBug = { ...component.board()!.backlog[0], id: 'qa-bug-open', key: 'BUG-005', source: 'qa', status: 'backlog' as const, title: '[QA] Existing contract bug' };
    component.board.set({ ...component.board()!, backlog: [openBug] });

    expect(component.qaFindingFollowUpActionLabel(finding)).toBe('Move to Team Lead');
    await component.handleQAFindingFollowUpAction(new Event('click'), finding);
    expect(plannedBacklogItemId).toBe(openBug.id);

    const failedPlan = { ...component.board()!.plans[0], id: 'plan-failed', status: 'failed' as const };
    const planningBug = { ...openBug, status: 'planning' as const, planId: failedPlan.id };
    component.board.set({ ...component.board()!, backlog: [planningBug], plans: [failedPlan] });

    expect(component.qaFindingFollowUpActionLabel(finding)).toBe('Retry Team Lead');
    await component.handleQAFindingFollowUpAction(new Event('click'), finding);
    expect(retriedPlanId).toBe(failedPlan.id);
  });

  it('ignores a blocked task and marks it done', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const blocked = {
      ...component.board()!.tasks[0],
      column: 'developer' as const,
      status: 'blocked' as const,
      blockedReason: 'Codex usage limit reached',
    };
    component.board.set({ ...component.board()!, tasks: [blocked] });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.task-card .ignore-task-button')).toBeNull();
    component.openTask(blocked);
    fixture.detectChanges();

    const ignoreButton = fixture.nativeElement.querySelector(
      '.task-header-actions .ignore-task-button',
    ) as HTMLButtonElement;
    expect(ignoreButton).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.execution-alert .ignore-task-button')).toBeNull();
    await component.ignoreTask(new Event('click'), blocked);

    expect(ignoredTaskId).toBe(blocked.id);
    expect(component.taskById(blocked.id)?.status).toBe('completed');
    expect(component.taskById(blocked.id)?.execution?.verdict).toBe('ignored');
  });

  it('opens backlog details in the right drawer', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();

    component.openBacklog(component.backlogItems()[0]);
    fixture.detectChanges();

    expect(component.selectedBacklog()?.key).toBe('BUG-001');
    expect(component.selectedTask()).toBeNull();
    expect(fixture.nativeElement.querySelector('.backlog-drawer .drawer-actions')).toBeNull();
    expect(fixture.nativeElement.querySelector('.backlog-action-primary')?.textContent).toContain('Send to Team Lead');

    const toggle = fixture.nativeElement.querySelector('.backlog-action-toggle') as HTMLButtonElement;
    toggle.click();
    fixture.detectChanges();

    expect(component.backlogActionMenuOpen()).toBe(true);
    expect(fixture.nativeElement.querySelector('.backlog-action-menu-panel')?.textContent).toContain('Remove');
  });

  it('moves a repeatedly reported QA bug to the top and highlights its existing card', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    const existing = component.board()!.backlog[0];
    component.board.set({
      ...component.board()!,
      backlog: [
        existing,
        {
          ...existing,
          id: 'backlog-reported-again',
          key: 'BUG-002',
          source: 'qa',
          title: '[QA] Existing regression',
          repeatReports: 2,
          lastReportedAt: '2026-08-22T02:30:00Z',
        },
      ],
    });
    fixture.detectChanges();

    expect(component.backlogItems()[0].id).toBe('backlog-reported-again');
    const highlighted = fixture.nativeElement.querySelector(
      '#backlog-card-backlog-reported-again',
    ) as HTMLElement;
    expect(highlighted.classList.contains('rediscovered')).toBe(true);
    expect(highlighted.textContent).toContain('Found again · 2');
  });

  it('keeps a readable title and detailed description in Team Lead intake', () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;

    component.requestForm.controls.title.setValue('Saved search filters');
    component.requestForm.controls.description.setValue(
      'Keep the current search filters after the user closes and reopens the app.',
    );

    expect(component.requestForm.valid).toBe(true);
    expect(component.requestForm.valid).toBe(true);
  });

  it('generates request-specific controls and applies their defaults', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.requestForm.setValue({
      title: 'Saved search filters',
      description: 'Keep search filters after the user closes and reopens the application.',
    });

    await component.prepareSmartIntake();

    expect(component.requestStep()).toBe('clarify');
    expect(component.intakeQuestionnaire()?.questions.length).toBe(2);
    expect(component.answerValue('delivery-surface')).toBe('fullstack');
    expect(component.intakeReady()).toBe(true);
  });

  it('defaults PM requests to feature and does not let Smart Intake reclassify them as bugs', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.openNewRequest();
    component.requestForm.setValue({
      title: 'Add Remove button to remove Feature/Bug/Task from Backlog',
      description: 'Add a remove action for unnecessary backlog cards, including cards created from QA bug reports.',
    });

    expect(component.requestType()).toBe('feature');
    await component.prepareSmartIntake();
    expect(generatedIntakeRequest).toMatchObject({ workType: 'feature' });

    const questionnaire = component.intakeQuestionnaire();
    expect(questionnaire).not.toBeNull();
    component.intakeQuestionnaire.set({ ...questionnaire!, workType: 'bug' });
    await component.submitRequest();

    expect(createdBacklogRequest).toMatchObject({
      type: 'feature',
      workType: 'feature',
    } satisfies Partial<CreateBacklogRequest>);
  });

  it('saves a canceled request as a project draft and lets the user discard it', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.openNewRequest();
    component.requestForm.setValue({
      title: 'Saved search filters',
      description: 'Keep search filters after the user closes and reopens the application.',
    });

    component.closeRequestModal();

    expect(component.requestModalOpen()).toBe(false);
    expect(component.requestDraft()?.title).toBe('Saved search filters');
    expect(localStorage.getItem('productcrew.requestDraft.project-1')).toContain('Saved search filters');

    const restored = TestBed.createComponent(WorkItemPage).componentInstance;
    await restored.ngOnInit();
    restored.continueRequestDraft();

    expect(restored.requestModalOpen()).toBe(true);
    expect(restored.requestForm.controls.title.value).toBe('Saved search filters');

    restored.discardRequestDraft();

    expect(restored.requestDraft()).toBeNull();
    expect(localStorage.getItem('productcrew.requestDraft.project-1')).toBeNull();
  });

  it('keeps the request modal open when the backdrop is clicked', async () => {
    const fixture = TestBed.createComponent(WorkItemPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    component.openNewRequest();
    fixture.detectChanges();

    (fixture.nativeElement.querySelector('.modal-backdrop') as HTMLElement).click();

    expect(component.requestModalOpen()).toBe(true);
  });

  it('clears the saved request draft after adding the backlog item', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.openNewRequest();
    component.requestForm.setValue({
      title: 'Saved search filters',
      description: 'Keep search filters after the user closes and reopens the application.',
    });
    component.closeRequestModal();
    component.continueRequestDraft();

    await component.prepareSmartIntake();
    await component.submitRequest();

    expect(component.requestDraft()).toBeNull();
    expect(localStorage.getItem('productcrew.requestDraft.project-1')).toBeNull();
  });

  it('keeps selected reference files and uploads them with the backlog request', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.requestForm.setValue({
      title: 'Settings import preview',
      description: 'Use the attached Markdown example while planning settings import behavior.',
    });
    const attachment = new File(['# Expected behavior'], 'expected.md', { type: 'text/markdown' });
    const input = { files: [attachment], value: 'selected' } as unknown as HTMLInputElement;

    component.selectRequestAttachments({ target: input } as unknown as Event);
    await component.prepareSmartIntake();
    await component.submitRequest();

    expect(createdBacklogAttachments.map((file) => file.name)).toEqual(['expected.md']);
    expect(component.requestAttachments()).toEqual([]);
  });

  it('keeps Backlog to Team Lead drag-drop working even if drag state is reset', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const item = component.backlogItems()[0];
    const dataTransfer = fakeDataTransfer();

    component.dragBacklogStart(fakeDragEvent(dataTransfer), item);
    component.dragEnd();
    component.dragOver(fakeDragEvent(dataTransfer), 'planning');
    await component.drop(fakeDragEvent(dataTransfer), 'planning');

    expect(plannedBacklogItemId).toBe(item.id);
    expect(component.successMessage()).toContain('Team Lead started planning');
  });

  it('opens and closes the live planning session for the selected plan', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const plan = { ...board.plans[0], status: 'analyzing' as const };

    component.togglePlanningLog(plan);
    expect(component.expandedPlanningLogId()).toBe(plan.id);

    component.togglePlanningLog(plan);
    expect(component.expandedPlanningLogId()).toBeNull();
  });

  it('calls removeBacklogItem on service and updates the board on backlog removal', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    const item = component.backlogItems()[0];

    await component.removeBacklogItem(item);

    expect(removedBacklogItemId).toBe(item.id);
    expect(component.board()?.backlog.length).toBe(0);
    expect(component.successMessage()).toContain(`Backlog item ${item.key} removed.`);
  });

  it('safe-stops the current project queue without clearing queued tasks', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();

    await component.safeStopQueue();

    expect(safeStoppedProjectId).toBe('project-1');
    expect(component.queueControlStatus()).toBe('stopping');
    expect(component.queueControlLabel()).toBe('Stopping after current');
    expect(component.board()?.tasks.length).toBe(1);
    expect(component.successMessage()).toContain('pause after the current task');
  });

  it('continues a paused project queue manually', async () => {
    const component = TestBed.createComponent(WorkItemPage).componentInstance;
    await component.ngOnInit();
    component.board.set({
      ...component.board()!,
      queueControl: {
        status: 'paused',
        requestedAt: '2026-08-11T00:10:00Z',
        pausedAt: '2026-08-11T00:20:00Z',
        updatedAt: '2026-08-11T00:20:00Z',
      },
    });

    await component.continueQueue();

    expect(continuedProjectId).toBe('project-1');
    expect(component.queueControlStatus()).toBe('running');
    expect(component.successMessage()).toContain('Queue continued');
  });

});

function fakeDataTransfer(): DataTransfer {
  const data = new Map<string, string>();
  return {
    get types() {
      return Array.from(data.keys()) as unknown as DOMStringList;
    },
    setData: (type: string, value: string) => data.set(type, value),
    getData: (type: string) => data.get(type) ?? '',
    effectAllowed: 'uninitialized',
    dropEffect: 'none',
  } as unknown as DataTransfer;
}

function fakeDragEvent(dataTransfer: DataTransfer): DragEvent {
  return {
    dataTransfer,
    preventDefault: () => undefined,
  } as unknown as DragEvent;
}
