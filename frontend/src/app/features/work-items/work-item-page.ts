import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import {
  Component,
  computed,
  HostListener,
  inject,
  OnDestroy,
  OnInit,
  signal,
} from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { ActivatedRoute } from '@angular/router';
import { firstValueFrom, Subscription } from 'rxjs';

import { ProjectContextService } from '../../core/project-context.service';
import { DesktopWindowService } from '../../core/desktop-window.service';
import { ProjectApiService } from '../../core/project-api.service';
import { BuildProfile, BuildRun, DeployProfile, DeployRun, ProductWorkspace } from '../../core/project.models';
import { GitStatusApiService } from '../../core/git-status-api.service';
import { GitStatus, TaskGitBranchResponse, TaskGitReferenceRequest } from '../../core/git-status.models';
import { WorkItemApiService } from '../../core/work-item-api.service';
import {
  AgentRole,
  BacklogItem,
  BoardColumn,
  BoardPlan,
  BoardTask,
  CreateBacklogRequest,
  DesignArtifact,
  IntakeQuestion,
  IntakeQuestionnaire,
  PlanDocument,
  ProjectBoard,
  RequestAttachment,
  TaskRevision,
  TaskFinding,
  TaskSessionLog,
} from '../../core/work-item.models';
import { AppShell } from '../../shared/app-shell/app-shell';
import { GitDeliveryModal } from '../../shared/git-delivery-modal/git-delivery-modal';

interface BoardColumnDefinition {
  id: BoardColumn;
  title: string;
  description: string;
  icon: string;
}

interface PendingRequestAttachment {
  id: string;
  file: File;
  previewUrl?: string;
}

type RequestWorkType = CreateBacklogRequest['type'];
type BoardMode = 'project' | 'workspace';

interface RequestDraft {
  schemaVersion: 1;
  projectId: string;
  title: string;
  description: string;
  requestType?: RequestWorkType;
  step: 'describe' | 'clarify';
  questionnaire: IntakeQuestionnaire | null;
  answers: Record<string, string[]>;
  attachmentNames: string[];
  savedAt: string;
}

interface CardTooltip {
  text: string;
  left: number;
  top: number;
  placement: 'above' | 'below';
}

interface DeployLogLine {
  level: string;
  message: string;
  at: number;
}

const BACKLOG_DRAG_TYPE = 'application/x-productcrew-backlog-id';
const TASK_DRAG_TYPE = 'application/x-productcrew-task-id';
const TOAST_DURATION_MS = 5_000;
const MAX_REQUEST_ATTACHMENTS = 12;
const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024;
const MAX_REQUEST_ATTACHMENT_BYTES = 50 * 1024 * 1024;
const MIN_REVIEW_FEEDBACK_LENGTH = 5;
const MAX_REVIEW_FEEDBACK_LENGTH = 2000;
const REQUEST_ATTACHMENT_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'webp', 'gif', 'pdf', 'txt', 'md', 'json']);
const REQUEST_DRAFT_STORAGE_PREFIX = 'productcrew.requestDraft.';
const WORK_BOARD_MODE_STORAGE_KEY = 'productcrew.work-board-mode';
const SELECTED_WORKSPACE_STORAGE_KEY = 'productcrew.selected-workspace-id';

@Component({
  selector: 'app-work-item-page',
  imports: [ReactiveFormsModule, DatePipe, AppShell, GitDeliveryModal],
  templateUrl: './work-item-page.html',
  styleUrl: './work-item-page.css',
})
export class WorkItemPage implements OnInit, OnDestroy {
  private readonly formBuilder = inject(FormBuilder);
  private readonly projectContext = inject(ProjectContextService);
  readonly desktop = inject(DesktopWindowService);
  private readonly projectApi = inject(ProjectApiService);
  private readonly boardApi = inject(WorkItemApiService);
  private readonly gitApi = inject(GitStatusApiService);
  private readonly title = inject(Title);
  private readonly route = inject(ActivatedRoute);
  private boardSubscription?: Subscription;
  private taskSessionSubscription?: Subscription;
  private planningSessionSubscription?: Subscription;
  private buildRunSubscription?: Subscription;
  private deployRunSubscription?: Subscription;
  private routeSubscription?: Subscription;
  private projectSelectionSubscription?: Subscription;
  private highlightTimer?: ReturnType<typeof setTimeout>;
  private buildResultTimer?: ReturnType<typeof setTimeout>;
  private loadSequence = 0;
  private pendingTaskFocusId: string | null = null;
  private activeProjectId = '';

  readonly columns: BoardColumnDefinition[] = [
    { id: 'backlog', title: 'Backlog', description: 'PM-owned ideas and verified bugs', icon: 'inventory_2' },
    { id: 'planning', title: 'Team Lead', description: 'Analysis and PM approval', icon: 'account_tree' },
    { id: 'designer', title: 'Designer', description: 'UI/UX agent queue', icon: 'palette' },
    { id: 'developer', title: 'Developer', description: 'Engineering agent queue', icon: 'terminal' },
    { id: 'qa', title: 'QA', description: 'Design match and bug verification', icon: 'verified' },
    { id: 'done', title: 'Done', description: 'Completed by the assigned agent', icon: 'task_alt' },
  ];

  readonly projects = this.projectContext.projects;
  readonly boardMode = signal<BoardMode>(this.readBoardMode());
  readonly workspaces = signal<ProductWorkspace[]>([]);
  readonly selectedWorkspaceId = signal(this.readSelectedWorkspaceId());
  readonly workspaceManagerOpen = signal(false);
  readonly workspaceLoading = signal(false);
  readonly editingWorkspaceId = signal<string | null>(null);
  readonly workspaceProjectIds = signal<string[]>([]);
  readonly workspaceProjectAccess = signal<Record<string, 'read-only' | 'read-write'>>({});
  readonly workspaceForm = this.formBuilder.nonNullable.group({
    name: ['', [Validators.required, Validators.minLength(3), Validators.maxLength(80)]],
  });
  readonly selectedWorkspace = computed(
    () => this.workspaces().find((workspace) => workspace.id === this.selectedWorkspaceId()) ?? null,
  );
  readonly selectedProjectId = computed(() =>
    this.boardMode() === 'workspace' ? this.selectedWorkspaceId() : this.projectContext.selectedProjectId(),
  );
  readonly board = signal<ProjectBoard | null>(null);
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly errorMessage = signal<string | null>(null);
  readonly successMessage = signal<string | null>(null);
  readonly requestModalOpen = signal(false);
  readonly requestStep = signal<'describe' | 'clarify'>('describe');
  readonly generatingIntake = signal(false);
  readonly intakeQuestionnaire = signal<IntakeQuestionnaire | null>(null);
  readonly intakeAnswers = signal<Record<string, string[]>>({});
  readonly requestAttachments = signal<PendingRequestAttachment[]>([]);
  readonly requestAttachmentDragging = signal(false);
  readonly requestDraft = signal<RequestDraft | null>(null);
  readonly reviewPlanId = signal<string | null>(null);
  readonly focusedPlanningId = signal<string | null>(null);
  readonly denyMode = signal(false);
  readonly denyReason = signal('');
  readonly designFeedbackMode = signal(false);
  readonly designFeedbackText = signal('');
  readonly designFeedbackError = signal<string | null>(null);
  readonly selectedTaskId = signal<string | null>(null);
  readonly selectedBacklogId = signal<string | null>(null);
  readonly backlogActionMenuOpen = signal(false);
  readonly taskSession = signal<TaskSessionLog | null>(null);
  readonly planningSession = signal<TaskSessionLog | null>(null);
  readonly buildProfile = signal<BuildProfile | null>(null);
  readonly buildProfileLoading = signal(false);
  readonly activeBuildRun = signal<BuildRun | null>(null);
  readonly buildRunning = signal(false);
  readonly visibleBuildRun = computed(() => {
    const run = this.activeBuildRun();
    return run && !this.isBuildRunning(run) ? run : null;
  });
  readonly projectGitStatus = signal<GitStatus | null>(null);
  readonly gitStatusLoading = signal(false);
  readonly gitDeliveryOpen = signal(false);
  readonly taskGitMode = signal<'view' | 'link' | 'create-confirm'>('view');
  readonly taskGitLinkKind = signal<'branch' | 'commit'>('branch');
  readonly taskGitLinkValue = signal('');
  readonly taskGitProposedBranch = signal<string | null>(null);
  readonly taskGitBranchConflict = signal<string | null>(null);
  readonly taskGitSaving = signal(false);
  readonly taskGitError = signal<string | null>(null);
  readonly deployProfile = signal<DeployProfile | null>(null);
  readonly deployProfileLoading = signal(false);
  readonly activeDeployRun = signal<DeployRun | null>(null);
  readonly deployRunning = signal(false);
  readonly deployLogs = signal<DeployLogLine[]>([]);
  readonly deployConfirmOpen = signal(false);
  readonly deployAcknowledged = signal(false);
  readonly deployLogViewerOpen = signal(false);
  readonly selectedDeployAction = computed(() => {
    const profile = this.deployProfile();
    return profile?.actions.find((action) => action.id === profile.selectedActionId) ?? null;
  });
  readonly deployControlState = computed<'unconfigured' | 'configured' | 'running' | 'passed' | 'failed'>(() => {
    if (!this.selectedDeployAction()) return 'unconfigured';
    const run = this.activeDeployRun();
    if (run && this.isDeployRunActive(run)) return 'running';
    if (run?.status === 'passed') return 'passed';
    if (run?.status === 'failed' || run?.status === 'timeout') return 'failed';
    return 'configured';
  });
  readonly expandedPlanningLogId = signal<string | null>(null);
  readonly selectedArtifact = signal<DesignArtifact | null>(null);
  readonly openingChangedFile = signal<string | null>(null);
  readonly downloadingChangedFile = signal<string | null>(null);
  readonly visibleTaskSession = computed(() => {
    const session = this.taskSession();
    return session && session.status !== 'idle' ? session : null;
  });
  readonly visiblePlanningSession = computed(() => {
    const session = this.planningSession();
    return session && session.status !== 'idle' ? session : null;
  });
  readonly highlightedTaskId = signal<string | null>(null);
  readonly draggedTaskId = signal<string | null>(null);
  readonly draggedBacklogId = signal<string | null>(null);
  readonly dropColumn = signal<BoardColumn | null>(null);
  readonly cardTooltip = signal<CardTooltip | null>(null);
  private cardTooltipTarget: HTMLElement | null = null;

  readonly requestForm = this.formBuilder.nonNullable.group({
    title: ['', [Validators.required, Validators.minLength(4), Validators.maxLength(120)]],
    description: ['', [Validators.required, Validators.minLength(10), Validators.maxLength(6000)]],
  });
  readonly requestType = signal<RequestWorkType>('feature');
  readonly requestTypeOptions: { value: RequestWorkType; label: string; icon: string; description: string }[] = [
    { value: 'feature', label: 'Feature', icon: 'auto_awesome', description: 'New capability or workflow improvement.' },
    { value: 'bug', label: 'Bug', icon: 'bug_report', description: 'Broken or incorrect behavior to fix.' },
    { value: 'todo', label: 'Task', icon: 'checklist', description: 'Maintenance, cleanup, or follow-up work.' },
  ];
  readonly intakeReady = computed(() => {
    const questionnaire = this.intakeQuestionnaire();
    if (!questionnaire) return false;
    return questionnaire.questions.every((question) => {
      if (!question.required || question.type === 'info') return true;
      return (this.intakeAnswers()[question.id] ?? []).some((value) => value.trim().length > 0);
    });
  });
  readonly answeredQuestionCount = computed(() => {
    const questionnaire = this.intakeQuestionnaire();
    if (!questionnaire) return 0;
    return questionnaire.questions.filter(
      (question) => question.type === 'info' || (this.intakeAnswers()[question.id] ?? []).some((value) => value.trim().length > 0),
    ).length;
  });

  readonly selectedProject = computed(() => {
    if (this.boardMode() === 'project') return this.projectContext.selectedProject();
    const workspace = this.selectedWorkspace();
    const primary = workspace?.folders.find((folder) => folder.primary) ?? workspace?.folders[0];
    return workspace && primary ? { id: workspace.id, name: workspace.name, path: primary.path, source: 'workspace' as const } : null;
  });
  readonly workspaceWritableCount = computed(
    () => this.selectedWorkspace()?.folders.filter((folder) => folder.access === 'read-write').length ?? 0,
  );
  readonly workspaceReadOnlyCount = computed(
    () => this.selectedWorkspace()?.folders.filter((folder) => folder.access === 'read-only').length ?? 0,
  );
  readonly canCreateWorkspace = computed(
    () => this.workspaceForm.valid && this.workspaceProjectIds().length >= 2 && !this.saving(),
  );
  readonly activeTask = computed(
    () => this.board()?.tasks.find((task) => task.id === this.board()?.activeTaskId) ?? null,
  );
  readonly queueControlStatus = computed(() => this.board()?.queueControl?.status ?? 'running');
  readonly queuedAgentTaskCount = computed(
    () => this.board()?.tasks.filter((task) => task.status === 'queued' && task.column === task.role).length ?? 0,
  );
  readonly selectedBuildAction = computed(() => {
    const profile = this.buildProfile();
    return profile?.actions.find((action) => action.id === profile.selectedActionId) ?? null;
  });
  readonly selectedTask = computed(
    () => this.board()?.tasks.find((task) => task.id === this.selectedTaskId()) ?? null,
  );
  readonly selectedBacklog = computed(
    () => this.board()?.backlog.find((item) => item.id === this.selectedBacklogId()) ?? null,
  );
  readonly selectedTaskPlan = computed(() => {
    const task = this.selectedTask();
    return task ? this.board()?.plans.find((plan) => plan.id === task.planId) ?? null : null;
  });
  readonly selectedTaskDocuments = computed(() => {
    const task = this.selectedTask();
    const plan = this.selectedTaskPlan();
    if (!task || !plan) return [];
    return plan.documents.filter((document) => task.documentIds.includes(document.id));
  });
  readonly selectedTaskDependencies = computed(() => {
    const task = this.selectedTask();
    return task
      ? this.board()?.tasks.filter((candidate) => task.dependencyIds.includes(candidate.id)) ?? []
      : [];
  });
  readonly reviewPlan = computed(() => {
    const id = this.reviewPlanId();
    return id ? this.board()?.plans.find((plan) => plan.id === id) ?? null : null;
  });
  readonly planningItems = computed(() =>
    [...(this.board()?.plans ?? [])]
      .filter((plan) => plan.status !== 'approved' && plan.status !== 'completed' && plan.status !== 'not_feasible')
      .sort((left, right) => right.createdAt.localeCompare(left.createdAt)),
  );
  readonly latestPlanningItem = computed(() => {
    const items = this.planningItems();
    return items.find((plan) => plan.id === this.focusedPlanningId()) ?? items[0] ?? null;
  });
  readonly approvedPlanCount = computed(
    () => this.board()?.plans.filter((plan) => plan.status === 'approved').length ?? 0,
  );
  readonly gitDirtyCount = computed(() => this.projectGitStatus()?.dirty.length ?? 0);

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Work board · ProductCrew');
    try {
      const requestedProjectId = this.route.snapshot.queryParamMap.get('projectId') ?? undefined;
      this.pendingTaskFocusId = this.route.snapshot.queryParamMap.get('taskId');
      await this.projectContext.initialize(requestedProjectId);
      await this.loadWorkspaces();
      const projectId = this.selectedProjectId();
      if (projectId) await this.activateProject(projectId);
      this.projectSelectionSubscription = this.projectContext.selectionChanges.subscribe((nextProjectId) => {
        if (this.boardMode() === 'project' && nextProjectId && nextProjectId !== this.activeProjectId) void this.activateProject(nextProjectId);
      });
      this.watchNavigationTarget();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load connected projects.'));
    } finally {
      this.loading.set(false);
    }
  }

  async switchBoardMode(mode: BoardMode): Promise<void> {
    if (mode === this.boardMode()) return;
    this.boardMode.set(mode);
    if (typeof localStorage !== 'undefined') localStorage.setItem(WORK_BOARD_MODE_STORAGE_KEY, mode);
    const scopeId = mode === 'workspace' ? this.selectedWorkspaceId() : this.projectContext.selectedProjectId();
    if (!scopeId) {
      this.board.set(null);
      this.buildProfile.set(null);
      this.deployProfile.set(null);
      if (mode === 'workspace') this.openWorkspaceManager();
      return;
    }
    await this.activateProject(scopeId);
  }

  async selectWorkspace(workspaceId: string): Promise<void> {
    if (!this.workspaces().some((workspace) => workspace.id === workspaceId)) return;
    this.selectedWorkspaceId.set(workspaceId);
    if (typeof localStorage !== 'undefined') localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, workspaceId);
    if (this.boardMode() === 'workspace' && workspaceId !== this.activeProjectId) await this.activateProject(workspaceId);
  }

  openWorkspaceManager(): void {
    const workspace = this.selectedWorkspace();
    this.editingWorkspaceId.set(workspace?.id ?? null);
    this.workspaceForm.reset({ name: workspace?.name ?? '' });
    this.workspaceProjectIds.set(workspace?.folders.map((folder) => folder.projectId) ?? []);
    this.workspaceProjectAccess.set(
      Object.fromEntries(workspace?.folders.map((folder) => [folder.projectId, folder.access]) ?? []),
    );
    this.workspaceManagerOpen.set(true);
  }

  openNewWorkspace(): void {
    this.editingWorkspaceId.set(null);
    this.workspaceForm.reset({ name: '' });
    this.workspaceProjectIds.set([]);
    this.workspaceProjectAccess.set({});
    this.workspaceManagerOpen.set(true);
  }

  closeWorkspaceManager(): void {
    if (!this.saving()) this.workspaceManagerOpen.set(false);
  }

  toggleWorkspaceProject(projectId: string): void {
    this.workspaceProjectIds.update((current) => {
      if (current.includes(projectId)) {
        this.workspaceProjectAccess.update((access) => {
          const next = { ...access };
          delete next[projectId];
          return next;
        });
        return current.filter((id) => id !== projectId);
      }
      this.workspaceProjectAccess.update((access) => ({ ...access, [projectId]: 'read-write' }));
      return [...current, projectId];
    });
  }

  workspaceProjectSelected(projectId: string): boolean {
    return this.workspaceProjectIds().includes(projectId);
  }

  toggleWorkspaceProjectAccess(event: Event, projectId: string): void {
    event.stopPropagation();
    if (this.workspaceProjectIds()[0] === projectId) return;
    this.workspaceProjectAccess.update((access) => ({
      ...access,
      [projectId]: access[projectId] === 'read-only' ? 'read-write' : 'read-only',
    }));
  }

  async createWorkspace(): Promise<void> {
    if (!this.canCreateWorkspace()) return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      const projectIds = this.workspaceProjectIds();
      const request = {
        name: this.workspaceForm.controls.name.value.trim(),
        folders: projectIds.map((projectId, index) => ({
          projectId,
          access: index === 0 ? 'read-write' as const : this.workspaceProjectAccess()[projectId] ?? 'read-write',
          primary: index === 0,
        })),
      };
      const editingID = this.editingWorkspaceId();
      const workspace = await firstValueFrom(
        editingID ? this.projectApi.updateWorkspace(editingID, request) : this.projectApi.createWorkspace(request),
      );
      this.workspaces.update((current) =>
        editingID ? current.map((item) => item.id === workspace.id ? workspace : item) : [workspace, ...current],
      );
      this.selectedWorkspaceId.set(workspace.id);
      this.boardMode.set('workspace');
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, workspace.id);
        localStorage.setItem(WORK_BOARD_MODE_STORAGE_KEY, 'workspace');
      }
      this.workspaceManagerOpen.set(false);
      await this.activateProject(workspace.id);
      this.successMessage.set(`${workspace.name} workspace ${editingID ? 'updated' : 'created'}.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not create workspace.'));
    } finally {
      this.saving.set(false);
    }
  }

  private async loadWorkspaces(): Promise<void> {
    this.workspaceLoading.set(true);
    try {
      const workspaces = await firstValueFrom(this.projectApi.listWorkspaces());
      this.workspaces.set(workspaces);
      const selected = workspaces.find((workspace) => workspace.id === this.selectedWorkspaceId()) ?? workspaces[0] ?? null;
      this.selectedWorkspaceId.set(selected?.id ?? '');
      if (selected && typeof localStorage !== 'undefined') localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, selected.id);
      if (!selected && this.boardMode() === 'workspace') this.board.set(null);
    } finally {
      this.workspaceLoading.set(false);
    }
  }

  private readBoardMode(): BoardMode {
    if (typeof localStorage === 'undefined') return 'project';
    return localStorage.getItem(WORK_BOARD_MODE_STORAGE_KEY) === 'workspace' ? 'workspace' : 'project';
  }

  private readSelectedWorkspaceId(): string {
    if (typeof localStorage === 'undefined') return '';
    return localStorage.getItem(SELECTED_WORKSPACE_STORAGE_KEY) ?? '';
  }

  ngOnDestroy(): void {
    this.boardSubscription?.unsubscribe();
    this.taskSessionSubscription?.unsubscribe();
    this.planningSessionSubscription?.unsubscribe();
    this.buildRunSubscription?.unsubscribe();
    this.deployRunSubscription?.unsubscribe();
    this.routeSubscription?.unsubscribe();
    this.projectSelectionSubscription?.unsubscribe();
    if (this.highlightTimer) clearTimeout(this.highlightTimer);
    if (this.buildResultTimer) clearTimeout(this.buildResultTimer);
    this.clearRequestAttachments();
  }

  @HostListener('document:keydown.escape')
  closeTopLayer(): void {
    if (this.saving()) return;
    if (this.deployLogViewerOpen()) {
      this.closeDeployLogViewer();
    } else if (this.deployConfirmOpen()) {
      this.cancelDeployConfirm();
    } else if (this.workspaceManagerOpen()) {
      this.closeWorkspaceManager();
    } else if (this.backlogActionMenuOpen()) {
      this.closeBacklogActionMenu();
    } else if (this.selectedArtifact()) {
      this.selectedArtifact.set(null);
    } else if (this.reviewPlanId()) {
      this.closePlanReview();
    } else if (this.selectedTaskId()) {
      this.closeTask();
    } else if (this.selectedBacklogId()) {
      this.closeBacklog();
    } else if (this.requestModalOpen()) {
      this.closeRequestModal();
    } else if (this.expandedPlanningLogId()) {
      this.closePlanningLog();
    }
  }

  @HostListener('window:resize')
  hideCardTooltip(): void {
    this.cardTooltipTarget = null;
    this.cardTooltip.set(null);
  }

  showCardTooltip(event: Event): void {
    const target = this.cardTooltipElement(event.target);
    if (!target) {
      this.hideCardTooltip();
      return;
    }
    const text = target.getAttribute('data-tooltip')?.trim();
    if (!text) return;
    const rect = target.getBoundingClientRect();
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 320;
    const gutter = 12;
    const maxTooltipWidth = Math.min(220, Math.max(120, viewportWidth - gutter * 2));
    const halfTooltipWidth = maxTooltipWidth / 2;
    const left = Math.min(
      viewportWidth - gutter - halfTooltipWidth,
      Math.max(gutter + halfTooltipWidth, rect.left + rect.width / 2),
    );
    const placement: CardTooltip['placement'] = rect.top > 72 ? 'above' : 'below';
    this.cardTooltipTarget = target;
    this.cardTooltip.set({
      text,
      left,
      top: placement === 'above' ? rect.top - 10 : rect.bottom + 10,
      placement,
    });
  }

  hideCardTooltipOnPointerOut(event: MouseEvent): void {
    const target = this.cardTooltipTarget;
    if (!target) return;
    if (event.relatedTarget instanceof Node && target.contains(event.relatedTarget)) return;
    this.hideCardTooltip();
  }

  private cardTooltipElement(target: EventTarget | null): HTMLElement | null {
    if (!(target instanceof Element)) return null;
    return target.closest<HTMLElement>('.task-card [data-tooltip], .board-actions [data-tooltip]');
  }

  private async activateProject(projectId: string): Promise<void> {
    this.activeProjectId = projectId;
    this.closeTask();
    this.closeBacklog();
    this.highlightedTaskId.set(null);
    this.pendingTaskFocusId = null;
    this.reviewPlanId.set(null);
    this.closePlanningLog();
    if (!this.requestModalOpen()) this.resetRequestComposer();
    this.loadRequestDraft(projectId);
    await this.loadBoard(projectId);
    await this.loadBuildProfile(projectId);
    await this.loadDeployProfile(projectId);
    await this.loadGitStatus(projectId);
  }

  openNewRequest(): void {
    if (!this.selectedProject()) {
      this.errorMessage.set('Import a project before creating work.');
      return;
    }
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.restoreRequestDraft(this.requestDraft());
    this.requestModalOpen.set(true);
    this.resetRequestScroll();
  }

  closeRequestModal(): void {
    if (this.saving() || this.generatingIntake()) return;
    if (this.hasDraftableRequestContent()) {
      this.saveRequestDraft();
      this.requestModalOpen.set(false);
      this.successMessage.set('Request draft saved. Continue it or discard it when you are ready.');
      return;
    }
    this.resetRequestComposer();
    this.requestModalOpen.set(false);
  }

  continueRequestDraft(): void {
    this.openNewRequest();
  }

  discardRequestDraft(): void {
    if (this.saving() || this.generatingIntake()) return;
    this.clearStoredRequestDraft();
    this.resetRequestComposer();
    this.requestModalOpen.set(false);
    this.successMessage.set('Request draft discarded.');
  }

  draftTitle(draft: RequestDraft): string {
    return draft.title.trim() || 'Untitled request';
  }

  draftSubtitle(draft: RequestDraft): string {
    const decisions = Object.values(draft.answers).filter((values) => values.some((value) => value.trim().length > 0)).length;
    const liveAttachments = this.requestAttachments().length;
    const details = [
      draft.step === 'clarify' ? `${decisions} decision${decisions === 1 ? '' : 's'} captured` : 'Description in progress',
      liveAttachments > 0
        ? `${liveAttachments} reference file${liveAttachments === 1 ? '' : 's'} kept`
        : draft.attachmentNames.length > 0
          ? 'Reference files need reattaching'
          : '',
    ].filter(Boolean);
    return details.join(' · ');
  }

  requestHasDraftableContent(): boolean {
    return this.hasDraftableRequestContent();
  }

  selectRequestAttachments(event: Event): void {
    const input = event.target as HTMLInputElement;
    if (input.files) this.addRequestAttachments(Array.from(input.files));
    input.value = '';
  }

  capturePastedAttachments(event: ClipboardEvent): void {
    const files = Array.from(event.clipboardData?.items ?? [])
      .filter((item) => item.kind === 'file')
      .map((item) => item.getAsFile())
      .filter((file): file is File => file !== null);
    if (files.length === 0) return;
    event.preventDefault();
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    this.addRequestAttachments(
      files.map((file, index) =>
        file.name && file.name !== 'image.png'
          ? file
          : new File([file], `pasted-${timestamp}-${index + 1}.${this.extensionForMediaType(file.type)}`, { type: file.type }),
      ),
    );
  }

  dragRequestAttachments(event: DragEvent): void {
    event.preventDefault();
    if (event.type === 'dragleave' && event.currentTarget === event.target) {
      this.requestAttachmentDragging.set(false);
      return;
    }
    this.requestAttachmentDragging.set(true);
  }

  dropRequestAttachments(event: DragEvent): void {
    event.preventDefault();
    this.requestAttachmentDragging.set(false);
    if (event.dataTransfer?.files) this.addRequestAttachments(Array.from(event.dataTransfer.files));
  }

  removeRequestAttachment(id: string): void {
    this.requestAttachments.update((attachments) => {
      const removed = attachments.find((attachment) => attachment.id === id);
      if (removed?.previewUrl) URL.revokeObjectURL(removed.previewUrl);
      return attachments.filter((attachment) => attachment.id !== id);
    });
  }

  requestAttachmentSize(size: number): string {
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} KB`;
    return `${(size / (1024 * 1024)).toFixed(1)} MB`;
  }

  isImageAttachment(mediaType: string): boolean {
    return mediaType.startsWith('image/');
  }

  requestAttachmentIcon(mediaType: string): string {
    return this.isImageAttachment(mediaType) ? 'image' : mediaType === 'application/pdf' ? 'picture_as_pdf' : 'description';
  }

  requestAttachmentUrl(item: BacklogItem, attachment: RequestAttachment, download = false): string {
    const project = this.selectedProject();
    if (!project) return '';
    return this.boardApi.requestAttachmentUrl(project.id, item.requestId, attachment.id, download);
  }

  private addRequestAttachments(files: File[]): void {
    this.errorMessage.set(null);
    const current = this.requestAttachments();
    if (current.length + files.length > MAX_REQUEST_ATTACHMENTS) {
      this.errorMessage.set(`A request can contain at most ${MAX_REQUEST_ATTACHMENTS} attachments.`);
      return;
    }
    const accepted: PendingRequestAttachment[] = [];
    for (const file of files) {
      const extension = file.name.split('.').pop()?.toLowerCase() ?? '';
      if (!REQUEST_ATTACHMENT_EXTENSIONS.has(extension)) {
        this.errorMessage.set(`${file.name} is not a supported attachment type.`);
        this.releasePendingAttachments(accepted);
        return;
      }
      if (file.size > MAX_ATTACHMENT_BYTES) {
        this.errorMessage.set(`${file.name} exceeds the 10 MB attachment limit.`);
        this.releasePendingAttachments(accepted);
        return;
      }
      const duplicate = [...current, ...accepted].some(
        (attachment) => attachment.file.name === file.name && attachment.file.size === file.size && attachment.file.lastModified === file.lastModified,
      );
      if (duplicate) continue;
      accepted.push({
        id: `${Date.now()}-${crypto.getRandomValues(new Uint32Array(1))[0]}`,
        file,
        previewUrl: this.isImageAttachment(file.type) ? URL.createObjectURL(file) : undefined,
      });
    }
    const total = [...current, ...accepted].reduce((sum, attachment) => sum + attachment.file.size, 0);
    if (total > MAX_REQUEST_ATTACHMENT_BYTES) {
      this.errorMessage.set('Request attachments exceed the 50 MB total limit.');
      this.releasePendingAttachments(accepted);
      return;
    }
    this.requestAttachments.set([...current, ...accepted]);
  }

  private extensionForMediaType(mediaType: string): string {
    return ({ 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/gif': 'gif' } as Record<string, string>)[mediaType] ?? 'png';
  }

  private clearRequestAttachments(): void {
    this.releasePendingAttachments(this.requestAttachments());
    this.requestAttachments.set([]);
    this.requestAttachmentDragging.set(false);
  }

  private releasePendingAttachments(attachments: PendingRequestAttachment[]): void {
    for (const attachment of attachments) if (attachment.previewUrl) URL.revokeObjectURL(attachment.previewUrl);
  }

  async prepareSmartIntake(): Promise<void> {
    const project = this.selectedProject();
    if (!project) return;
    if (this.requestForm.invalid) {
      this.requestForm.markAllAsTouched();
      this.errorMessage.set('Add a readable name and enough request context before continuing.');
      return;
    }
    this.generatingIntake.set(true);
    this.errorMessage.set(null);
    const value = this.requestForm.getRawValue();
    try {
      const questionnaire = await firstValueFrom(
        this.boardApi.generateIntakeQuestions(project.id, {
          title: value.title.trim(),
          description: value.description.trim(),
          workType: this.requestType(),
        }),
      );
      this.intakeQuestionnaire.set(questionnaire);
      this.intakeAnswers.set(
        Object.fromEntries(
          questionnaire.questions.map((question) => [question.id, [...(question.defaultValues ?? [])]]),
        ),
      );
      this.requestStep.set('clarify');
      this.resetRequestScroll();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not prepare smart questions. Try again.'));
    } finally {
      this.generatingIntake.set(false);
    }
  }

  backToRequestDescription(): void {
    if (this.saving()) return;
    this.requestStep.set('describe');
    this.resetRequestScroll();
  }

  private resetRequestScroll(): void {
    requestAnimationFrame(() => {
      document.querySelector<HTMLElement>('.request-modal .modal-scroll')?.scrollTo({ top: 0 });
    });
  }

  setSingleAnswer(questionId: string, value: string): void {
    this.intakeAnswers.update((answers) => ({ ...answers, [questionId]: value ? [value] : [] }));
  }

  setTextAnswer(questionId: string, event: Event): void {
    this.setSingleAnswer(questionId, (event.target as HTMLTextAreaElement).value);
  }

  toggleIntakeAnswer(questionId: string, value: string, checked: boolean): void {
    this.intakeAnswers.update((answers) => {
      const next = new Set(answers[questionId] ?? []);
      checked ? next.add(value) : next.delete(value);
      return { ...answers, [questionId]: [...next] };
    });
  }

  answerSelected(questionId: string, value: string): boolean {
    return (this.intakeAnswers()[questionId] ?? []).includes(value);
  }

  answerValue(questionId: string): string {
    return this.intakeAnswers()[questionId]?.[0] ?? '';
  }

  setRequestType(type: RequestWorkType): void {
    if (this.requestStep() === 'clarify') return;
    this.requestType.set(type);
  }

  intakeControlIcon(type: IntakeQuestion['type']): string {
    return ({ toggle: 'toggle_on', radio: 'radio_button_checked', dropdown: 'unfold_more', checkbox: 'checklist', scale: 'linear_scale', short_text: 'notes', info: 'lightbulb' } as const)[type];
  }

  async submitRequest(): Promise<void> {
    const project = this.selectedProject();
    if (!project) return;
    const questionnaire = this.intakeQuestionnaire();
    if (this.requestForm.invalid || !questionnaire || !this.intakeReady()) {
      this.errorMessage.set('Answer every required clarification before adding this request.');
      return;
    }
    this.saving.set(true);
    this.errorMessage.set(null);
    const value = this.requestForm.getRawValue();
    const requestType = this.requestType();
    const request: CreateBacklogRequest = {
      type: requestType,
      source: 'manual',
      title: value.title.trim(),
      description: value.description.trim(),
      workType: requestType,
      deliveryTarget: 'fullstack',
      requiresUI: false,
      acceptanceCriteria: [],
      intake: { questionnaire, answers: this.intakeAnswers() },
    };
    try {
      const board = await firstValueFrom(
        this.boardApi.createBacklog(project.id, request, this.requestAttachments().map((attachment) => attachment.file)),
      );
      this.board.set(board);
      this.requestModalOpen.set(false);
      this.clearStoredRequestDraft();
      this.resetRequestComposer();
      this.successMessage.set('Request added to Backlog. Drag it to Team Lead when you are ready to plan.');
      this.startBoardWatch(project.id);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not create the planning request.'));
    } finally {
      this.saving.set(false);
    }
  }

  private restoreRequestDraft(draft: RequestDraft | null): void {
    this.resetRequestComposer({ preserveAttachments: !!draft && this.requestAttachments().length > 0 });
    if (!draft || draft.projectId !== this.selectedProjectId()) return;
    this.requestForm.reset({
      title: draft.title,
      description: draft.description,
    });
    this.requestType.set(draft.requestType ?? draft.questionnaire?.workType ?? 'feature');
    this.intakeQuestionnaire.set(draft.questionnaire);
    this.intakeAnswers.set(draft.answers);
    this.requestStep.set(draft.step === 'clarify' && draft.questionnaire ? 'clarify' : 'describe');
  }

  private saveRequestDraft(): void {
    const projectId = this.selectedProjectId();
    if (!projectId) return;
    const value = this.requestForm.getRawValue();
    const draft: RequestDraft = {
      schemaVersion: 1,
      projectId,
      title: value.title,
      description: value.description,
      requestType: this.requestType(),
      step: this.requestStep(),
      questionnaire: this.intakeQuestionnaire(),
      answers: this.intakeAnswers(),
      attachmentNames: this.requestAttachments().map((attachment) => attachment.file.name),
      savedAt: new Date().toISOString(),
    };
    this.requestDraft.set(draft);
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(this.requestDraftStorageKey(projectId), JSON.stringify(draft));
    }
  }

  private loadRequestDraft(projectId: string): void {
    if (typeof localStorage === 'undefined') {
      this.requestDraft.set(null);
      return;
    }
    const raw = localStorage.getItem(this.requestDraftStorageKey(projectId));
    if (!raw) {
      this.requestDraft.set(null);
      return;
    }
    try {
      const draft = JSON.parse(raw) as RequestDraft;
      this.requestDraft.set(draft.schemaVersion === 1 && draft.projectId === projectId ? draft : null);
    } catch {
      localStorage.removeItem(this.requestDraftStorageKey(projectId));
      this.requestDraft.set(null);
    }
  }

  private clearStoredRequestDraft(): void {
    const projectId = this.selectedProjectId();
    if (projectId && typeof localStorage !== 'undefined') {
      localStorage.removeItem(this.requestDraftStorageKey(projectId));
    }
    this.requestDraft.set(null);
  }

  private requestDraftStorageKey(projectId: string): string {
    return `${REQUEST_DRAFT_STORAGE_PREFIX}${projectId}`;
  }

  private resetRequestComposer(options: { preserveAttachments?: boolean } = {}): void {
    this.requestForm.reset({
      title: '',
      description: '',
    });
    this.requestType.set('feature');
    this.requestStep.set('describe');
    this.intakeQuestionnaire.set(null);
    this.intakeAnswers.set({});
    if (options.preserveAttachments) {
      this.requestAttachmentDragging.set(false);
    } else {
      this.clearRequestAttachments();
    }
  }

  private hasDraftableRequestContent(): boolean {
    const value = this.requestForm.getRawValue();
    if (value.title.trim() || value.description.trim() || this.requestAttachments().length > 0 || this.intakeQuestionnaire()) {
      return true;
    }
    return Object.values(this.intakeAnswers()).some((answers) => answers.some((answer) => answer.trim().length > 0));
  }

  openPlanReview(plan: BoardPlan): void {
    this.reviewPlanId.set(plan.id);
    this.denyMode.set(false);
    this.denyReason.set('');
  }

  focusPlanning(event: Event): void {
    this.closePlanningLog();
    this.focusedPlanningId.set((event.target as HTMLSelectElement).value);
  }

  togglePlanningLog(plan: BoardPlan): void {
    if (this.expandedPlanningLogId() === plan.id) {
      this.closePlanningLog();
      return;
    }
    this.expandedPlanningLogId.set(plan.id);
    this.planningSession.set(null);
    this.planningSessionSubscription?.unsubscribe();
    this.planningSessionSubscription = this.boardApi
      .watchPlanningSession(this.selectedProjectId(), plan.id)
      .subscribe({
        next: (session) => {
          this.planningSession.set(session);
          requestAnimationFrame(() => {
            const log = document.getElementById('planning-session-log');
            if (log) log.scrollTop = log.scrollHeight;
          });
        },
      });
  }

  closePlanningLog(): void {
    this.expandedPlanningLogId.set(null);
    this.planningSession.set(null);
    this.planningSessionSubscription?.unsubscribe();
    this.planningSessionSubscription = undefined;
  }

  closePlanReview(): void {
    this.reviewPlanId.set(null);
    this.denyMode.set(false);
    this.denyReason.set('');
  }

  setDenyReason(event: Event): void {
    this.denyReason.set((event.target as HTMLTextAreaElement).value);
  }

  async approvePlan(plan: BoardPlan): Promise<void> {
    await this.reviewPlanning(plan, 'approve', '');
  }

  async approveAndRunSequence(plan: BoardPlan): Promise<void> {
    await this.reviewPlanning(plan, 'approve', '', true);
  }

  canRunSequence(plan: BoardPlan): boolean {
    const tasks = this.tasksForPlan(plan.id);
    if (tasks.length === 0) return false;
    if (!tasks.some((task) => task.role === 'developer') || tasks.at(-1)?.role !== 'qa') return false;
    const phases = { designer: 0, developer: 1, qa: 2 } as const;
    return tasks.every((task, index) => {
      if (task.role === 'qa' && index !== tasks.length - 1) return false;
      return index === 0 || phases[task.role] >= phases[tasks[index - 1].role];
    });
  }

  async denyPlan(plan: BoardPlan): Promise<void> {
    const reason = this.denyReason().trim();
    if (reason.length < 5) {
      this.errorMessage.set('Explain what Team Lead must change before sending the plan back.');
      return;
    }
    await this.reviewPlanning(plan, 'deny', reason);
  }

  async retryPlan(plan: BoardPlan): Promise<void> {
    const projectId = this.selectedProjectId();
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(await firstValueFrom(this.boardApi.retryPlan(projectId, plan.id)));
      this.closePlanReview();
      this.successMessage.set('Team Lead is retrying this plan in the background.');
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not retry Team Lead planning.'));
    } finally {
      this.saving.set(false);
    }
  }

  tasksIn(column: BoardColumn): BoardTask[] {
    if (column === 'backlog') return [];
    return (this.board()?.tasks ?? [])
      .filter((task) => task.column === column)
      .sort((left, right) => {
        const leftSequenced = !!left.sequenceOrder;
        const rightSequenced = !!right.sequenceOrder;
        if (leftSequenced !== rightSequenced) return leftSequenced ? -1 : 1;
        if (leftSequenced && rightSequenced) {
          const scheduledOrder = (this.planFor(left)?.sequence?.scheduledAt ?? '').localeCompare(
            this.planFor(right)?.sequence?.scheduledAt ?? '',
          );
          if (scheduledOrder !== 0) return scheduledOrder;
          return (left.sequenceOrder ?? 0) - (right.sequenceOrder ?? 0);
        }
        return this.priorityRank(left.priority) - this.priorityRank(right.priority);
      });
  }

  backlogItems(): BacklogItem[] {
    return [...(this.board()?.backlog ?? [])]
      .filter((item) => item.status === 'backlog')
      .sort((left, right) =>
        (right.lastReportedAt ?? right.createdAt).localeCompare(left.lastReportedAt ?? left.createdAt),
      );
  }

  dragBacklogStart(event: DragEvent, item: BacklogItem): void {
    this.draggedBacklogId.set(item.id);
    event.dataTransfer?.setData('text/plain', item.id);
    event.dataTransfer?.setData(BACKLOG_DRAG_TYPE, item.id);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
  }

  planFor(task: BoardTask): BoardPlan | null {
    return this.board()?.plans.find((plan) => plan.id === task.planId) ?? null;
  }

  tasksForPlan(planId: string): BoardTask[] {
    return (this.board()?.tasks ?? []).filter((task) => task.planId === planId);
  }

  openTask(task: BoardTask): void {
    this.closeBacklog();
    this.selectedTaskId.set(task.id);
    this.designFeedbackMode.set(false);
    this.designFeedbackText.set('');
    this.designFeedbackError.set(null);
    this.resetTaskGitEvidence();
    this.taskSession.set(null);
    this.taskSessionSubscription?.unsubscribe();
    this.taskSessionSubscription = this.boardApi
      .watchTaskSession(this.selectedProjectId(), task.id)
      .subscribe({
        next: (session) => {
          this.taskSession.set(session);
          requestAnimationFrame(() => {
            const log = document.getElementById('task-session-log');
            if (log) log.scrollTop = log.scrollHeight;
          });
        },
      });
  }

  closeTask(): void {
    this.selectedTaskId.set(null);
    this.designFeedbackMode.set(false);
    this.designFeedbackText.set('');
    this.designFeedbackError.set(null);
    this.resetTaskGitEvidence();
    this.taskSession.set(null);
    this.taskSessionSubscription?.unsubscribe();
    this.taskSessionSubscription = undefined;
    this.selectedArtifact.set(null);
  }

  artifactUrl(task: BoardTask, artifact: DesignArtifact): string {
    return this.boardApi.taskArtifactUrl(this.selectedProjectId(), task.id, artifact.id);
  }

  openArtifact(event: Event, artifact: DesignArtifact): void {
    event.stopPropagation();
    this.selectedArtifact.set(artifact);
  }

  async openChangedFile(event: Event, relativePath: string): Promise<void> {
    event.stopPropagation();
    const project = this.selectedProject();
    if (!project || this.openingChangedFile()) return;

    this.openingChangedFile.set(relativePath);
    this.errorMessage.set(null);
    try {
      await this.desktop.openProjectFile(project.id, relativePath);
    } catch (error) {
      this.errorMessage.set(
        error instanceof Error
          ? error.message
          : typeof error === 'string'
            ? error
            : 'Could not open this output file.',
      );
    } finally {
      this.openingChangedFile.set(null);
    }
  }

  async downloadChangedFile(event: Event, relativePath: string): Promise<void> {
    event.stopPropagation();
    const project = this.selectedProject();
    if (!project || this.downloadingChangedFile()) return;

    this.downloadingChangedFile.set(relativePath);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      const destination = await this.desktop.downloadProjectFile(project.id, relativePath);
      const fileName = destination.split(/[\\/]/).pop() || relativePath;
      this.successMessage.set(`${fileName} downloaded to Downloads.`);
    } catch (error) {
      this.errorMessage.set(
        error instanceof Error
          ? error.message
          : typeof error === 'string'
            ? error
            : 'Could not download this output file.',
      );
    } finally {
      this.downloadingChangedFile.set(null);
    }
  }

  openBacklog(item: BacklogItem): void {
    this.closeTask();
    this.closeBacklogActionMenu();
    this.selectedBacklogId.set(item.id);
  }

  linkedFindingBacklog(finding: TaskFinding): BacklogItem | null {
    const backlog = this.board()?.backlog ?? [];
    const id = finding.linkedBacklogId?.trim();
    const key = finding.linkedBacklogKey?.trim().toLowerCase();
    const linked = backlog.find((item) => (id && item.id === id) || (key && item.key.toLowerCase() === key));
    if (linked) return linked;
    const normalizedTitle = this.normalizedQAFindingTitle(finding.title);
    if (!normalizedTitle) return null;
    return backlog.find(
      (item) =>
        item.source === 'qa' &&
        item.type === 'bug' &&
        item.status !== 'done' &&
        this.normalizedQAFindingTitle(item.title) === normalizedTitle,
    ) ?? null;
  }

  qaFindingTicketLabel(finding: TaskFinding): string {
    const item = this.linkedFindingBacklog(finding);
    const key = item?.key ?? finding.linkedBacklogKey?.trim();
    const title = item?.title ?? finding.linkedBacklogTitle?.trim() ?? finding.title;
    return key ? `${key} · ${title}` : title;
  }

  qaFindingPlan(finding: TaskFinding): BoardPlan | null {
    const item = this.linkedFindingBacklog(finding);
    const planId = item?.planId || finding.linkedBacklogPlanId?.trim();
    return planId ? this.board()?.plans.find((plan) => plan.id === planId) ?? null : null;
  }

  qaFindingFollowUpActionLabel(finding: TaskFinding): string {
    const item = this.linkedFindingBacklog(finding);
    if (!item) return '';
    if (item.status === 'backlog') return 'Move to Team Lead';
    const plan = this.qaFindingPlan(finding);
    if (plan?.status === 'failed' || plan?.status === 'changes_requested') return 'Retry Team Lead';
    return '';
  }

  qaFindingFollowUpActionIcon(finding: TaskFinding): string {
    const item = this.linkedFindingBacklog(finding);
    if (!item) return 'open_in_new';
    if (item.status === 'backlog') return 'account_tree';
    const plan = this.qaFindingPlan(finding);
    if (plan?.status === 'failed' || plan?.status === 'changes_requested') return 'refresh';
    return 'open_in_new';
  }

  openQAFindingTicket(event: Event, finding: TaskFinding): void {
    event.preventDefault();
    event.stopPropagation();
    const item = this.linkedFindingBacklog(finding);
    if (!item) return;
    this.openBacklog(item);
  }

  async handleQAFindingFollowUpAction(event: Event, finding: TaskFinding): Promise<void> {
    event.preventDefault();
    event.stopPropagation();
    const item = this.linkedFindingBacklog(finding);
    if (!item || this.saving()) return;
    if (item.status === 'backlog') {
      await this.planBacklogItem(item, event);
      return;
    }
    const plan = this.qaFindingPlan(finding);
    if (plan?.status === 'failed' || plan?.status === 'changes_requested') {
      await this.retryPlan(plan);
      return;
    }
    this.openBacklog(item);
  }

  private normalizedQAFindingTitle(value: string): string {
    return value
      .trim()
      .toLowerCase()
      .replace(/^\[qa\]\s*/, '')
      .replace(/[^\p{L}\p{N}]+/gu, ' ')
      .trim()
      .replace(/\s+/g, ' ');
  }

  closeBacklog(): void {
    this.closeBacklogActionMenu();
    this.selectedBacklogId.set(null);
  }

  toggleBacklogActionMenu(event: Event): void {
    event.stopPropagation();
    if (this.saving()) return;
    this.backlogActionMenuOpen.update((open) => !open);
  }

  closeBacklogActionMenu(): void {
    this.backlogActionMenuOpen.set(false);
  }

  backlogSourceLabel(item: BacklogItem): string {
    return item.source === 'feature-radar'
      ? 'Feature Radar'
      : item.source === 'source-scan'
        ? 'Bug Scanner'
        : 'PM request';
  }

  backlogSourceIcon(item: BacklogItem): string {
    return item.source === 'feature-radar'
      ? 'radar'
      : item.source === 'source-scan'
        ? 'policy'
        : 'person';
  }

  taskById(taskId: string): BoardTask | null {
    return this.board()?.tasks.find((task) => task.id === taskId) ?? null;
  }

  canDrag(task: BoardTask): boolean {
    if (task.sequenceOrder && task.status !== 'blocked') return false;
    return task.status !== 'in_progress' && task.status !== 'verifying' && task.status !== 'design_review' && task.status !== 'completed';
  }

  dragStart(event: DragEvent, task: BoardTask): void {
    if (!this.canDrag(task)) {
      event.preventDefault();
      return;
    }
    this.draggedTaskId.set(task.id);
    event.dataTransfer?.setData('text/plain', task.id);
    event.dataTransfer?.setData(TASK_DRAG_TYPE, task.id);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
  }

  dragOver(event: DragEvent, column: BoardColumn): void {
    const backlogDrag = this.isBacklogDrag(event);
    const taskDrag = this.isTaskDrag(event);
    if (!taskDrag && !backlogDrag) return;
    event.preventDefault();
    this.dropColumn.set(column);
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = backlogDrag
        ? (column === 'planning' ? 'move' : 'none')
        : (this.canDrop(this.draggedTaskFromEvent(event), column) ? 'move' : 'none');
    }
  }

  dragLeave(event: DragEvent, column: BoardColumn): void {
    if (event.currentTarget === event.target && this.dropColumn() === column) {
      this.dropColumn.set(null);
    }
  }

  async drop(event: DragEvent, column: BoardColumn): Promise<void> {
    event.preventDefault();
    const backlogId = this.backlogDragId(event);
    this.draggedBacklogId.set(null);
    if (backlogId) {
      this.dropColumn.set(null);
      if (column !== 'planning') {
        this.errorMessage.set('Backlog items can only be moved to Team Lead for planning.');
        return;
      }
      this.saving.set(true);
      try {
        await this.startBacklogPlanning(backlogId);
      } catch (error) {
        this.errorMessage.set(this.errorText(error, 'Could not start Team Lead planning.'));
      } finally {
        this.saving.set(false);
      }
      return;
    }
    const task = this.draggedTaskFromEvent(event);
    this.draggedTaskId.set(null);
    this.dropColumn.set(null);
    if (!task || task.column === column) return;
    if (!this.canDrop(task, column)) {
      this.errorMessage.set(this.dropError(task, column));
      return;
    }
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(
        await firstValueFrom(this.boardApi.moveTask(this.selectedProjectId(), task.id, column)),
      );
      this.successMessage.set(`${task.key} moved to ${this.columnLabel(column)}.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not move this task.'));
    } finally {
      this.saving.set(false);
    }
  }

  async planBacklogItem(item: BacklogItem, event?: Event): Promise<void> {
    event?.stopPropagation();
    if (this.saving()) return;
    this.closeBacklogActionMenu();
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      await this.startBacklogPlanning(item.id);
      this.closeBacklog();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not start Team Lead planning.'));
    } finally {
      this.saving.set(false);
    }
  }

  async removeBacklogItem(item: BacklogItem, event?: Event): Promise<void> {
    event?.stopPropagation();
    if (this.saving()) return;
    this.closeBacklogActionMenu();
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(
        await firstValueFrom(this.boardApi.removeBacklogItem(this.selectedProjectId(), item.id)),
      );
      this.successMessage.set(`Backlog item ${item.key} removed.`);
      if (this.selectedBacklogId() === item.id) {
        this.closeBacklog();
      }
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not remove this backlog item.'));
    } finally {
      this.saving.set(false);
    }
  }

  async queueTask(event: Event, task: BoardTask): Promise<void> {
    event.stopPropagation();
    if (this.saving() || !this.canQueue(task)) return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      const target = task.role as BoardColumn;
      this.board.set(await firstValueFrom(this.boardApi.moveTask(this.selectedProjectId(), task.id, target)));
      this.successMessage.set(`${task.key} queued for ${this.roleLabel(task.role)}.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not queue this task.'));
    } finally {
      this.saving.set(false);
    }
  }

  async restartTask(event: Event, task: BoardTask): Promise<void> {
    event.stopPropagation();
    if (this.saving() || task.status !== 'blocked') return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(
        await firstValueFrom(this.boardApi.restartTask(this.selectedProjectId(), task.id)),
      );
      this.successMessage.set(`${task.key} restarted with a new AI session.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not restart this task.'));
    } finally {
      this.saving.set(false);
    }
  }

  async verifyManualQABug(event: Event, task: BoardTask): Promise<void> {
    await this.restartTask(event, task);
  }

  async approveDesignTask(event: Event, task: BoardTask): Promise<void> {
    event.stopPropagation();
    if (this.saving() || !this.canApproveDesign(task)) return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(
        await firstValueFrom(this.boardApi.approveDesignTask(this.selectedProjectId(), task.id)),
      );
      this.successMessage.set(`${task.key} design handoff approved. Dependent tasks can start now.`);
      this.closeTask();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not approve this design handoff.'));
    } finally {
      this.saving.set(false);
    }
  }

  openDesignFeedback(event: Event, task: BoardTask): void {
    event.stopPropagation();
    if (this.saving() || !this.canApproveDesign(task)) return;
    this.designFeedbackMode.set(true);
    this.designFeedbackError.set(null);
    this.errorMessage.set(null);
    requestAnimationFrame(() => {
      (document.getElementById('design-feedback-composer') as HTMLElement | null)?.scrollIntoView?.({ block: 'nearest' });
      (document.getElementById('design-feedback-textarea') as HTMLTextAreaElement | null)?.focus();
    });
  }

  cancelDesignFeedback(event?: Event): void {
    event?.stopPropagation();
    if (this.saving()) return;
    this.designFeedbackMode.set(false);
    this.designFeedbackError.set(null);
  }

  setDesignFeedback(event: Event): void {
    this.designFeedbackText.set((event.target as HTMLTextAreaElement).value);
    this.designFeedbackError.set(null);
    this.errorMessage.set(null);
  }

  async submitDesignFeedback(event: Event, task: BoardTask): Promise<void> {
    event.stopPropagation();
    if (this.saving() || !this.canApproveDesign(task)) return;
    const feedback = this.designFeedbackText().trim();
    if (feedback.length < MIN_REVIEW_FEEDBACK_LENGTH) {
      const message = 'Explain what the Designer should revise before sending feedback.';
      this.designFeedbackError.set(message);
      this.errorMessage.set(message);
      return;
    }
    this.saving.set(true);
    this.errorMessage.set(null);
    this.designFeedbackError.set(null);
    try {
      this.board.set(
        await firstValueFrom(
          this.boardApi.submitDesignFeedback(this.selectedProjectId(), task.id, {
            feedback,
            reviewer: 'PM',
          }),
        ),
      );
      this.successMessage.set(`${task.key} sent back to Designer for revision.`);
      this.closeTask();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not send Designer feedback.'));
    } finally {
      this.saving.set(false);
    }
  }

  async ignoreTask(event: Event, task: BoardTask): Promise<void> {
    event.stopPropagation();
    if (this.saving() || task.status !== 'blocked') return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(
        await firstValueFrom(this.boardApi.ignoreTask(this.selectedProjectId(), task.id)),
      );
      this.successMessage.set(`${task.key} ignored and marked done.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not ignore this task.'));
    } finally {
      this.saving.set(false);
    }
  }

  async safeStopQueue(): Promise<void> {
    const projectId = this.selectedProjectId();
    if (!projectId || this.saving() || this.queueControlStatus() !== 'running') return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      const board = await firstValueFrom(this.boardApi.safeStopQueue(projectId));
      this.board.set(board);
      this.successMessage.set(board.queueControl?.status === 'stopping'
        ? 'Queue will pause after the current task finishes.'
        : 'Queue paused. Queued tasks will wait until you continue.');
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not safe stop this queue.'));
    } finally {
      this.saving.set(false);
    }
  }

  async continueQueue(): Promise<void> {
    const projectId = this.selectedProjectId();
    if (!projectId || this.saving() || this.queueControlStatus() === 'running') return;
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      this.board.set(await firstValueFrom(this.boardApi.continueQueue(projectId)));
      this.successMessage.set('Queue continued. ProductCrew will start the next ready task.');
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not continue this queue.'));
    } finally {
      this.saving.set(false);
    }
  }

  async runProjectBuild(): Promise<void> {
    const projectId = this.selectedProjectId();
    const action = this.selectedBuildAction();
    if (!projectId || !action || this.buildRunning()) return;
    this.buildRunning.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      const run = await firstValueFrom(this.projectApi.startBuildRun(projectId, action.id));
      this.clearBuildResult();
      this.activeBuildRun.set(run);
      this.watchBuildRun(projectId, run);
    } catch (error) {
      this.buildRunning.set(false);
      this.errorMessage.set(this.errorText(error, 'Could not start project build verification.'));
    }
  }

  buildButtonTitle(): string {
    if (this.buildProfileLoading()) return 'Loading build action configuration.';
    if (!this.buildProfile() || this.buildProfile()?.status === 'not_detected') return 'Configure a build action in Settings first.';
    if (!this.selectedBuildAction()) return 'No build action selected for this project.';
    return 'Run the configured build action for this project.';
  }

  buildControlLabel(): string {
    if (this.buildProfileLoading()) return 'Loading';
    if (this.buildRunning()) return 'Running';
    const run = this.activeBuildRun();
    if (run?.status === 'passed') return 'Passed';
    if (run?.status === 'failed' || run?.status === 'timeout') return 'Failed';
    return this.selectedBuildAction() ? 'Configured' : 'Not configured';
  }

  buildControlTooltip(): string {
    const action = this.selectedBuildAction();
    if (!action) return this.buildButtonTitle();
    const run = this.activeBuildRun();
    const status = run ? `Last run: ${run.status}${run.exitCode !== 0 ? `, exit ${run.exitCode}` : ''}. ` : '';
    return `Build verify: ${action.label}. ${status}${this.buildButtonTitle()}`;
  }

  deployButtonTitle(): string {
    if (this.deployProfileLoading()) return 'Loading deploy action configuration.';
    if (!this.selectedDeployAction()) return 'Configure a deploy action in Settings first.';
    if (this.deployControlState() === 'running') return 'A deploy is already running.';
    return 'Run the configured production deploy command for this project.';
  }

  deployControlLabel(): string {
    switch (this.deployControlState()) {
      case 'unconfigured':
        return 'Not configured';
      case 'running':
        return 'Running';
      case 'passed':
        return 'Passed';
      case 'failed':
        return 'Failed';
      default:
        return 'Configured';
    }
  }

  deployControlTooltip(): string {
    const action = this.selectedDeployAction();
    if (!action) return this.deployButtonTitle();
    const run = this.activeDeployRun();
    const status = run ? `Last run: ${run.status}${run.exitCode !== 0 ? `, exit ${run.exitCode}` : ''}. ` : '';
    return `Production deploy: ${action.label}. ${status}${this.deployButtonTitle()}`;
  }

  openDeployConfirm(): void {
    if (!this.selectedProject() || !this.selectedDeployAction() || this.deployControlState() === 'running') return;
    this.deployAcknowledged.set(false);
    this.deployConfirmOpen.set(true);
  }

  cancelDeployConfirm(): void {
    this.deployConfirmOpen.set(false);
    this.deployAcknowledged.set(false);
  }

  async confirmDeploy(): Promise<void> {
    const projectId = this.selectedProjectId();
    const action = this.selectedDeployAction();
    if (!projectId || !action || !this.deployAcknowledged() || this.deployControlState() === 'running') return;
    this.deployConfirmOpen.set(false);
    this.deployRunning.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      this.deployLogs.set([]);
      const run = await firstValueFrom(this.projectApi.startDeployRun(projectId, action.id));
      this.activeDeployRun.set(run);
      this.deployLogViewerOpen.set(true);
      this.watchDeployRun(projectId, run);
    } catch (error) {
      this.deployRunning.set(false);
      this.errorMessage.set(this.errorText(error, 'Could not start the production deploy.'));
    }
  }

  openDeployLogViewer(): void {
    if (!this.activeDeployRun()) return;
    this.deployLogViewerOpen.set(true);
  }

  closeDeployLogViewer(): void {
    this.deployLogViewerOpen.set(false);
  }

  deployDurationSeconds(run: DeployRun): number {
    return Math.round(run.durationMs / 1000);
  }

  deployElapsedSeconds(): number {
    const run = this.activeDeployRun();
    if (!run) return 0;
    const startedAt = new Date(run.startedAt).getTime();
    return Math.max(0, Math.round((Date.now() - startedAt) / 1000));
  }

  queueControlLabel(): string {
    switch (this.queueControlStatus()) {
      case 'stopping':
        return 'Stopping after current';
      case 'paused':
        return 'Queue paused';
      default:
        return 'Queue running';
    }
  }

  queueControlSubtitle(): string {
    const queued = this.queuedAgentTaskCount();
    switch (this.queueControlStatus()) {
      case 'stopping':
        return 'Will pause after active task';
      case 'paused':
        return queued > 0 ? `${queued} waiting for continue` : 'Manual continue required';
      default:
        return queued > 0 ? `${queued} waiting in queue` : 'Ready for next task';
    }
  }

  queueControlStatusLabel(): string {
    switch (this.queueControlStatus()) {
      case 'stopping':
        return 'Safe stopping';
      case 'paused':
        return 'Paused';
      default:
        return this.queuedAgentTaskCount() > 0 ? `${this.queuedAgentTaskCount()} queued` : 'Running';
    }
  }

  queueControlTooltip(): string {
    return `${this.queueControlLabel()}: ${this.queueControlSubtitle()}.`;
  }

  queueControlIcon(): string {
    switch (this.queueControlStatus()) {
      case 'stopping':
        return 'motion_photos_pause';
      case 'paused':
        return 'pause_circle';
      default:
        return 'queue_play_next';
    }
  }

  dragEnd(): void {
    this.draggedTaskId.set(null);
    this.draggedBacklogId.set(null);
    this.dropColumn.set(null);
  }

  canDrop(task: BoardTask | null, column: BoardColumn): boolean {
    if (!task || task.status === 'in_progress' || task.status === 'verifying' || task.status === 'design_review' || task.status === 'completed' || column === 'done') {
      return false;
    }
    const plan = this.planFor(task);
    if (plan?.status !== 'approved') return false;
    if (task.sequenceOrder && plan.sequence?.status !== 'completed') {
      return task.status === 'blocked' && column === task.role;
    }
    if (column !== 'planning' && column !== task.role) return false;
    return true;
  }

  waitingDependencyKeys(task: BoardTask): string[] {
    if (task.status !== 'queued') return [];
    return task.dependencyIds
      .map((dependencyId) => this.taskById(dependencyId))
      .filter((dependency): dependency is BoardTask => !!dependency && dependency.status !== 'completed')
      .map((dependency) => dependency.key);
  }

  canQueue(task: BoardTask): boolean {
    return this.canDrop(task, task.role as BoardColumn);
  }

  canApproveDesign(task: BoardTask): boolean {
    return task.role === 'designer' && task.status === 'design_review' && !task.sequenceOrder && !!task.execution;
  }

  hasDesignerRevisionContext(task: BoardTask): boolean {
    return task.role === 'designer' && !task.sequenceOrder && (!!task.execution || (task.revisionHistory?.length ?? 0) > 0);
  }

  designerRevisionHistory(task: BoardTask): TaskRevision[] {
    return [...(task.revisionHistory ?? [])].sort((left, right) => right.revision - left.revision);
  }

  currentDesignerRevision(task: BoardTask): number {
    const latestArchivedRevision = Math.max(0, ...(task.revisionHistory ?? []).map((revision) => revision.revision));
    return task.execution ? Math.max(1, latestArchivedRevision + 1) : latestArchivedRevision;
  }

  reviewFeedbackMinLength(): number {
    return MIN_REVIEW_FEEDBACK_LENGTH;
  }

  reviewFeedbackMaxLength(): number {
    return MAX_REVIEW_FEEDBACK_LENGTH;
  }

  canVerifyManualQABug(task: BoardTask): boolean {
    return task.role === 'qa' && task.status === 'blocked' && !task.sequenceOrder && !this.isWaitingOnPlanningBug(task);
  }

  isWaitingOnPlanningBug(task: BoardTask): boolean {
    const reason = (task.blockedReason ?? '').toLowerCase();
    return task.role === 'qa'
      && task.status === 'blocked'
      && reason.includes('team lead planning')
      && (reason.includes('waiting on') || reason.includes('matched'));
  }

  taskStatusTone(task: BoardTask): BoardTask['status'] | 'waiting' {
    return this.isWaitingOnPlanningBug(task) ? 'waiting' : task.status;
  }

  taskStatusLabel(task: BoardTask): string {
    return this.isWaitingOnPlanningBug(task) ? 'Waiting' : this.statusLabel(task.status);
  }

  queueBlockReason(task: BoardTask): string {
    return this.dropError(task, task.role as BoardColumn);
  }

  roleLabel(role: AgentRole): string {
    return { designer: 'Designer', developer: 'Developer', qa: 'QA' }[role];
  }

  statusLabel(status: BoardTask['status']): string {
    return {
      planned: 'Planned',
      queued: 'Queued',
      in_progress: 'In progress',
      verifying: 'Build verifying',
      design_review: 'Design review',
      blocked: 'Blocked',
      completed: 'Completed',
    }[status];
  }

  planningStatusLabel(plan: BoardPlan): string {
    return {
      analyzing: 'Team Lead analyzing',
      awaiting_approval: 'PM review required',
      approved: 'Approved',
      changes_requested: 'Changes requested',
      failed: 'Planning needs attention',
      completed: 'Already implemented',
      not_feasible: 'Not feasible',
    }[plan.status];
  }

  documentKindLabel(document: PlanDocument): string {
    return document.kind.replaceAll('-', ' ');
  }

  columnLabel(column: BoardColumn): string {
    return this.columns.find((candidate) => candidate.id === column)?.title ?? column;
  }

  dropState(column: BoardColumn): 'valid' | 'invalid' | null {
    if (this.draggedBacklogId() && this.dropColumn() === column) return column === 'planning' ? 'valid' : 'invalid';
    const task = this.draggedTask();
    if (!task || this.dropColumn() !== column) return null;
    return this.canDrop(task, column) ? 'valid' : 'invalid';
  }

  dropMessage(column: BoardColumn): string {
    const backlogId = this.draggedBacklogId();
    if (backlogId) {
      return column === 'planning'
        ? 'Release to send this backlog item to Team Lead'
        : 'Backlog items can only enter Team Lead planning';
    }
    const task = this.draggedTask();
    if (!task) return '';
    return this.canDrop(task, column) ? `Release to queue ${task.key}` : this.dropError(task, column);
  }

  private async loadBoard(projectId: string): Promise<void> {
    const sequence = ++this.loadSequence;
    this.loading.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.boardSubscription?.unsubscribe();
    this.boardSubscription = undefined;
    try {
      const board = await firstValueFrom(this.boardApi.getBoard(projectId));
      if (sequence !== this.loadSequence) return;
      this.board.set(board);
      this.applyPendingTaskFocus(board);
      this.startBoardWatch(projectId);
    } catch (error) {
      if (sequence !== this.loadSequence) return;
      const response = error as HttpErrorResponse;
      if (response.status === 404) {
        this.board.set(null);
      } else {
        this.errorMessage.set(this.errorText(error, 'Could not load this project board.'));
      }
    } finally {
      if (sequence === this.loadSequence) this.loading.set(false);
    }
  }

  private async loadBuildProfile(projectId: string, preserveBuildResult = false): Promise<void> {
    this.buildRunSubscription?.unsubscribe();
    if (!preserveBuildResult) this.clearBuildResult();
    this.buildRunning.set(false);
    this.buildProfileLoading.set(true);
    try {
      const profile = await firstValueFrom(this.projectApi.getBuildProfile(projectId));
      this.buildProfile.set(profile);
      if (profile.lastRun && ['queued', 'running'].includes(profile.lastRun.status)) {
        this.clearBuildResult();
        this.activeBuildRun.set(profile.lastRun);
        this.watchBuildRun(projectId, profile.lastRun);
      } else if (!preserveBuildResult) {
        this.activeBuildRun.set(null);
      }
    } catch {
      this.buildProfile.set(null);
    } finally {
      this.buildProfileLoading.set(false);
    }
  }

  private async loadGitStatus(projectId: string): Promise<void> {
    this.gitStatusLoading.set(true);
    try {
      const status = await firstValueFrom(this.gitApi.getStatus(projectId));
      this.projectGitStatus.set(status);
    } catch {
      this.projectGitStatus.set(null);
    } finally {
      this.gitStatusLoading.set(false);
    }
  }

  refreshGitStatus(): void {
    const projectId = this.selectedProjectId();
    if (projectId) void this.loadGitStatus(projectId);
  }

  private resetTaskGitEvidence(): void {
    this.taskGitMode.set('view');
    this.taskGitLinkKind.set('branch');
    this.taskGitLinkValue.set('');
    this.taskGitProposedBranch.set(null);
    this.taskGitBranchConflict.set(null);
    this.taskGitError.set(null);
  }

  openTaskGitLink(task: BoardTask): void {
    this.taskGitError.set(null);
    this.taskGitLinkKind.set(task.gitReference?.commitSha && !task.gitReference?.branch ? 'commit' : 'branch');
    this.taskGitLinkValue.set(task.gitReference?.branch || task.gitReference?.commitSha || '');
    this.taskGitMode.set('link');
  }

  setTaskGitLinkKind(kind: 'branch' | 'commit'): void {
    this.taskGitLinkKind.set(kind);
  }

  useCurrentBranchForGitLink(): void {
    const branch = this.projectGitStatus()?.branch;
    if (!branch) return;
    this.taskGitLinkKind.set('branch');
    this.taskGitLinkValue.set(branch);
  }

  cancelTaskGitLink(): void {
    this.taskGitMode.set('view');
    this.taskGitError.set(null);
  }

  async submitTaskGitLink(task: BoardTask): Promise<void> {
    const value = this.taskGitLinkValue().trim();
    if (!value || this.taskGitSaving()) return;
    this.taskGitSaving.set(true);
    this.taskGitError.set(null);
    try {
      const request: TaskGitReferenceRequest =
        this.taskGitLinkKind() === 'branch' ? { branch: value } : { commitSha: value };
      const board = await firstValueFrom(
        this.gitApi.linkTaskGitReference(this.selectedProjectId(), task.id, request),
      );
      this.applyGitReferenceFromBoard(board, task.id);
      this.taskGitMode.set('view');
    } catch (error) {
      this.taskGitError.set(this.errorText(error, 'Could not link this branch or commit.'));
    } finally {
      this.taskGitSaving.set(false);
    }
  }

  async unlinkTaskGitReference(task: BoardTask): Promise<void> {
    if (this.taskGitSaving()) return;
    this.taskGitSaving.set(true);
    this.taskGitError.set(null);
    try {
      const board = await firstValueFrom(this.gitApi.clearTaskGitReference(this.selectedProjectId(), task.id));
      this.applyGitReferenceFromBoard(board, task.id);
      this.taskGitMode.set('view');
    } catch (error) {
      this.taskGitError.set(this.errorText(error, 'Could not clear the linked evidence.'));
    } finally {
      this.taskGitSaving.set(false);
    }
  }

  async openTaskGitBranchConfirm(task: BoardTask): Promise<void> {
    if (this.taskGitSaving()) return;
    this.taskGitError.set(null);
    this.taskGitBranchConflict.set(null);
    this.taskGitSaving.set(true);
    try {
      const proposal = await firstValueFrom(this.gitApi.proposeTaskGitBranch(this.selectedProjectId(), task.id));
      this.taskGitProposedBranch.set(proposal.branchName);
      this.taskGitMode.set('create-confirm');
    } catch (error) {
      this.taskGitError.set(this.errorText(error, 'Could not propose a branch name.'));
    } finally {
      this.taskGitSaving.set(false);
    }
  }

  cancelTaskGitBranchConfirm(): void {
    this.taskGitMode.set('view');
    this.taskGitProposedBranch.set(null);
    this.taskGitBranchConflict.set(null);
    this.taskGitError.set(null);
  }

  async confirmCreateTaskGitBranch(task: BoardTask): Promise<void> {
    const branchName = this.taskGitProposedBranch();
    if (!branchName || this.taskGitSaving()) return;
    this.taskGitSaving.set(true);
    this.taskGitBranchConflict.set(null);
    this.taskGitError.set(null);
    try {
      const result = await firstValueFrom(
        this.gitApi.createTaskGitBranch(this.selectedProjectId(), task.id, branchName),
      );
      this.applyCreatedGitBranch(task.id, result);
      this.taskGitMode.set('view');
      this.taskGitProposedBranch.set(null);
      this.refreshGitStatus();
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 409) {
        const conflict = error.error as TaskGitBranchResponse | undefined;
        this.taskGitBranchConflict.set(conflict?.branchName ?? branchName);
      } else {
        this.taskGitError.set(this.errorText(error, 'Could not create the branch.'));
      }
    } finally {
      this.taskGitSaving.set(false);
    }
  }

  private applyGitReferenceFromBoard(board: ProjectBoard, taskId: string): void {
    const updated = board.tasks.find((candidate) => candidate.id === taskId);
    this.board.update((current) =>
      current
        ? {
            ...current,
            tasks: current.tasks.map((task) =>
              task.id === taskId ? { ...task, gitReference: updated?.gitReference } : task,
            ),
          }
        : current,
    );
  }

  private applyCreatedGitBranch(taskId: string, result: TaskGitBranchResponse): void {
    this.board.update((current) =>
      current
        ? {
            ...current,
            tasks: current.tasks.map((task) =>
              task.id === taskId
                ? {
                    ...task,
                    gitReference: {
                      branch: result.branchName,
                      commitSha: task.gitReference?.commitSha,
                      linkedAt: new Date().toISOString(),
                      linkedBy: task.gitReference?.linkedBy,
                    },
                  }
                : task,
            ),
          }
        : current,
    );
  }

  private watchBuildRun(projectId: string, run: BuildRun): void {
    this.buildRunning.set(this.isBuildRunning(run));
    this.buildRunSubscription?.unsubscribe();
    this.buildRunSubscription = this.projectApi.watchBuildRun(projectId, run.id).subscribe({
      next: (update) => {
        this.activeBuildRun.set(update);
        this.buildRunning.set(this.isBuildRunning(update));
        if (!this.isBuildRunning(update)) this.scheduleBuildResultDismiss(update.id);
      },
      complete: () => {
        const finished = this.activeBuildRun();
        this.buildRunning.set(false);
        if (finished && !this.isBuildRunning(finished)) this.scheduleBuildResultDismiss(finished.id);
        void this.loadBuildProfile(projectId, true);
      },
    });
  }

  private isBuildRunning(run: BuildRun): boolean {
    return run.status === 'queued' || run.status === 'running';
  }

  private clearBuildResult(): void {
    if (this.buildResultTimer) clearTimeout(this.buildResultTimer);
    this.buildResultTimer = undefined;
    this.activeBuildRun.set(null);
  }

  private scheduleBuildResultDismiss(runId: string): void {
    if (this.buildResultTimer) clearTimeout(this.buildResultTimer);
    this.buildResultTimer = setTimeout(() => {
      if (this.activeBuildRun()?.id === runId) this.activeBuildRun.set(null);
      this.buildResultTimer = undefined;
    }, TOAST_DURATION_MS);
  }

  private async loadDeployProfile(projectId: string, preserveDeployResult = false): Promise<void> {
    this.deployRunSubscription?.unsubscribe();
    if (!preserveDeployResult) {
      this.activeDeployRun.set(null);
      this.deployLogs.set([]);
    }
    this.deployRunning.set(false);
    this.deployProfileLoading.set(true);
    try {
      const profile = await firstValueFrom(this.projectApi.getDeployProfile(projectId));
      this.deployProfile.set(profile);
      if (profile.lastRun && this.isDeployRunActive(profile.lastRun)) {
        this.activeDeployRun.set(profile.lastRun);
        this.watchDeployRun(projectId, profile.lastRun);
      } else if (!preserveDeployResult) {
        this.activeDeployRun.set(profile.lastRun ?? null);
      }
    } catch {
      this.deployProfile.set(null);
    } finally {
      this.deployProfileLoading.set(false);
    }
  }

  private watchDeployRun(projectId: string, run: DeployRun): void {
    this.deployRunning.set(this.isDeployRunActive(run));
    this.deployRunSubscription?.unsubscribe();
    this.deployRunSubscription = this.projectApi
      .watchDeployRun(projectId, run.id, (level, message) => {
        this.deployLogs.update((lines) => [...lines, { level, message, at: Date.now() }]);
      })
      .subscribe({
        next: (update) => {
          this.activeDeployRun.set(update);
          this.deployRunning.set(this.isDeployRunActive(update));
        },
        complete: () => {
          this.deployRunning.set(false);
          void this.loadDeployProfile(projectId, true);
        },
      });
  }

  private isDeployRunActive(run: DeployRun): boolean {
    return run.status === 'queued' || run.status === 'running';
  }

  private startBoardWatch(projectId: string): void {
    this.boardSubscription?.unsubscribe();
    this.boardSubscription = this.boardApi.watchBoard(projectId).subscribe({
      next: (board) => {
        if (board.projectId === this.selectedProjectId()) {
          this.board.set(board);
          this.applyPendingTaskFocus(board);
        }
      },
    });
  }

  private watchNavigationTarget(): void {
    this.routeSubscription?.unsubscribe();
    this.routeSubscription = this.route.queryParamMap.subscribe((params) => {
      const projectId = params.get('projectId');
      if (!projectId || !this.projects().some((project) => project.id === projectId)) return;

      this.pendingTaskFocusId = params.get('taskId');
      if (this.boardMode() !== 'project') {
        this.boardMode.set('project');
        if (typeof localStorage !== 'undefined') localStorage.setItem(WORK_BOARD_MODE_STORAGE_KEY, 'project');
      }
      if (projectId === this.selectedProjectId()) {
        this.applyPendingTaskFocus(this.board());
        return;
      }

      this.projectContext.select(projectId);
    });
  }

  private applyPendingTaskFocus(board: ProjectBoard | null): void {
    const taskId = this.pendingTaskFocusId;
    if (!board || !taskId) return;

    const task = board.tasks.find((candidate) => candidate.id === taskId);
    this.pendingTaskFocusId = null;
    if (!task) return;

    this.focusTask(task, false);
  }

  private focusTask(task: BoardTask, openDrawer: boolean): void {
    if (openDrawer) this.openTask(task);
    this.highlightedTaskId.set(task.id);
    if (this.highlightTimer) clearTimeout(this.highlightTimer);
    this.highlightTimer = setTimeout(() => this.highlightedTaskId.set(null), 3600);

    setTimeout(() => {
      document
        .getElementById(`task-card-${task.id}`)
        ?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'center' });
    }, 80);
  }

  private async reviewPlanning(
    plan: BoardPlan,
    decision: 'approve' | 'deny',
    reason: string,
    runSequence = false,
  ): Promise<void> {
    this.saving.set(true);
    this.errorMessage.set(null);
    try {
      const updatedBoard = await firstValueFrom(
        this.boardApi.reviewPlan(this.selectedProjectId(), plan.id, {
          decision,
          reason,
          reviewer: 'Ong Ben',
          runSequence,
        }),
      );
      this.board.set(updatedBoard);
      this.closePlanReview();
      if (decision === 'approve' && this.expandedPlanningLogId() === plan.id) {
        this.closePlanningLog();
      }
      const remainingPlanning = updatedBoard.plans
        .filter((candidate) => candidate.status !== 'approved' && candidate.status !== 'completed' && candidate.status !== 'not_feasible')
        .sort((left, right) => right.createdAt.localeCompare(left.createdAt));
      this.focusedPlanningId.set(remainingPlanning[0]?.id ?? null);
      this.successMessage.set(
        runSequence
          ? `"${plan.request.title}" approved. Sequence scheduled and will start automatically when the agent slot is available.`
          : decision === 'approve'
          ? `"${plan.request.title}" approved. ${
              remainingPlanning.length > 0
                ? `Next pending plan: "${remainingPlanning[0].request.title}".`
                : 'No pending plans remain.'
            }`
          : 'Feedback sent. Team Lead is preparing the next revision.',
      );
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not review this plan.'));
    } finally {
      this.saving.set(false);
    }
  }

  private async startBacklogPlanning(backlogId: string): Promise<void> {
    this.board.set(await firstValueFrom(this.boardApi.planBacklog(this.selectedProjectId(), backlogId)));
    this.successMessage.set('Team Lead started planning this backlog item in the background.');
  }

  private draggedTask(): BoardTask | null {
    const id = this.draggedTaskId();
    return id ? this.taskById(id) : null;
  }

  private draggedTaskFromEvent(event: DragEvent): BoardTask | null {
    const id = this.draggedTaskId() || event.dataTransfer?.getData(TASK_DRAG_TYPE) || '';
    return id ? this.taskById(id) : null;
  }

  private backlogDragId(event: DragEvent): string {
    return this.draggedBacklogId() || event.dataTransfer?.getData(BACKLOG_DRAG_TYPE) || '';
  }

  private isBacklogDrag(event: DragEvent): boolean {
    return !!this.draggedBacklogId() || this.hasDragType(event, BACKLOG_DRAG_TYPE);
  }

  private isTaskDrag(event: DragEvent): boolean {
    return !!this.draggedTaskId() || this.hasDragType(event, TASK_DRAG_TYPE);
  }

  private hasDragType(event: DragEvent, type: string): boolean {
    return Array.from(event.dataTransfer?.types ?? []).includes(type);
  }

  private dropError(task: BoardTask, column: BoardColumn): string {
    const plan = this.planFor(task);
    if (plan?.status !== 'approved') return 'Approve the Team Lead plan before routing its tasks.';
    if (column === 'done') return 'Only the assigned agent can complete a task.';
    if (column !== 'planning' && column !== task.role) {
      return `${this.roleLabel(task.role)} tasks cannot be moved to ${this.columnLabel(column)}.`;
    }
    return 'This move is not allowed.';
  }

  private priorityRank(priority: BoardTask['priority']): number {
    return { critical: 0, high: 1, medium: 2, low: 3 }[priority];
  }

  private errorText(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse;
    return response.error?.error ?? fallback;
  }
}
