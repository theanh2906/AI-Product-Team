import { DatePipe } from '@angular/common';
import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Title } from '@angular/platform-browser';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { firstValueFrom, Subscription } from 'rxjs';

import { ProjectContextService } from '../../core/project-context.service';
import { WorkItemApiService } from '../../core/work-item-api.service';
import {
  AgentRole,
  BacklogItem,
  BoardPlan,
  BoardTask,
  DesignArtifact,
  ProjectBoard,
  TaskStatus,
} from '../../core/work-item.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type FeatureFilter = 'all' | 'backlog' | 'active' | 'shipped' | 'not_feasible';
type FeatureState = 'backlog' | 'planning' | 'in_progress' | 'blocked' | 'shipped' | 'not_feasible';
type StageState = 'complete' | 'active' | 'blocked' | 'pending';
type FeatureSort = 'updated' | 'created' | 'title';
type LibraryStreamState = 'loading' | 'live' | 'reconnecting' | 'stale';

interface FeatureEntry {
  item: BacklogItem;
  plan: BoardPlan | null;
  tasks: BoardTask[];
  state: FeatureState;
  progress: number;
  updatedAt: string;
}

interface FeatureSection {
  label: string;
  tone: FeatureState;
  features: FeatureEntry[];
}

interface LifecycleStage {
  label: string;
  icon: string;
  state: StageState;
  date?: string;
}

interface TicketGroup {
  role: AgentRole;
  label: string;
  icon: string;
  tasks: BoardTask[];
}

interface FeatureArtifact {
  task: BoardTask;
  artifact: DesignArtifact;
  createdAt: string;
}

interface FeatureDesignSource {
  task: BoardTask;
  path: string;
  title: string;
  createdAt: string;
}

interface HistoryEvent {
  id: string;
  label: string;
  actor: string;
  createdAt: string;
  tone: 'success' | 'active' | 'warning' | 'error';
}

@Component({
  selector: 'app-feature-library-page',
  imports: [AppShell, DatePipe, RouterLink],
  templateUrl: './feature-library-page.html',
  styleUrl: './feature-library-page.css',
})
export class FeatureLibraryPage implements OnInit, OnDestroy {
  private readonly boardApi = inject(WorkItemApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly title = inject(Title);

  private boardSubscription?: Subscription;
  private projectSubscription?: Subscription;
  private activeProjectId = '';
  private loadSequence = 0;
  private pendingFeatureReference = '';

  readonly board = signal<ProjectBoard | null>(null);
  readonly loading = signal(true);
  readonly errorMessage = signal<string | null>(null);
  readonly search = signal('');
  readonly filter = signal<FeatureFilter>('all');
  readonly sort = signal<FeatureSort>('updated');
  readonly selectedFeatureId = signal('');
  readonly selectedArtifact = signal<FeatureArtifact | null>(null);
  readonly streamState = signal<LibraryStreamState>('loading');
  readonly lastSyncedAt = signal<string | null>(null);

  readonly selectedProjectId = this.projectContext.selectedProjectId;
  readonly selectedProject = this.projectContext.selectedProject;

  readonly streamLabel = computed(() => {
    return {
      loading: 'Waiting for SSE',
      live: 'Live via SSE',
      reconnecting: 'Reconnecting',
      stale: 'Sync paused',
    }[this.streamState()];
  });

  readonly streamTitle = computed(() => {
    const syncedAt = this.lastSyncedAt();
    const suffix = syncedAt ? ` Last sync: ${new Date(syncedAt).toLocaleString()}.` : '';
    return {
      loading: `ProductCrew is opening the Feature Library stream.${suffix}`,
      live: `Feature statuses, tickets, lifecycle, and history update from Workboard SSE.${suffix}`,
      reconnecting: `The browser is reconnecting to the local SSE stream. Last visible data is retained.${suffix}`,
      stale: `Feature Library is showing the last loaded board snapshot.${suffix}`,
    }[this.streamState()];
  });

  readonly features = computed<FeatureEntry[]>(() => {
    const board = this.board();
    if (!board) return [];
    return board.backlog
      .filter((item) => item.type === 'feature')
      .map((item) => this.toFeatureEntry(board, item));
  });

  readonly filteredFeatures = computed(() => {
    const query = this.search().trim().toLowerCase();
    const filter = this.filter();
    const features = this.features().filter((feature) => {
      const matchesQuery =
        !query ||
        [feature.item.key, feature.item.title, feature.item.description, feature.item.source]
          .filter(Boolean)
          .some((value) => value.toLowerCase().includes(query));
      if (!matchesQuery) return false;
      if (filter === 'all') return true;
      if (filter === 'active') {
        return ['planning', 'in_progress', 'blocked'].includes(feature.state);
      }
      return feature.state === filter;
    });

    return [...features].sort((left, right) => {
      if (this.sort() === 'title') return left.item.title.localeCompare(right.item.title);
      if (this.sort() === 'created') return right.item.createdAt.localeCompare(left.item.createdAt);
      return right.updatedAt.localeCompare(left.updatedAt);
    });
  });

  readonly sections = computed<FeatureSection[]>(() => {
    const features = this.filteredFeatures();
    const definitions: Array<{ label: string; tone: FeatureState; states: FeatureState[] }> = [
      { label: 'Needs attention', tone: 'blocked', states: ['blocked'] },
      { label: 'In progress', tone: 'in_progress', states: ['planning', 'in_progress'] },
      { label: 'Backlog', tone: 'backlog', states: ['backlog'] },
      { label: 'Shipped', tone: 'shipped', states: ['shipped'] },
      { label: 'Not feasible', tone: 'not_feasible', states: ['not_feasible'] },
    ];
    return definitions
      .map((definition) => ({
        label: definition.label,
        tone: definition.tone,
        features: features.filter((feature) => definition.states.includes(feature.state)),
      }))
      .filter((section) => section.features.length > 0);
  });

  readonly selectedFeature = computed(() => {
    const features = this.features();
    return features.find((feature) => feature.item.id === this.selectedFeatureId()) ?? features[0] ?? null;
  });

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Feature Library · ProductCrew');
    this.pendingFeatureReference = this.route.snapshot.queryParamMap.get('featureId') ?? '';
    try {
      const requestedProjectId = this.route.snapshot.queryParamMap.get('projectId') ?? undefined;
      await this.projectContext.initialize(requestedProjectId);
      if (this.selectedProjectId()) await this.activateProject(this.selectedProjectId());
      this.projectSubscription = this.projectContext.selectionChanges.subscribe((projectId) => {
        if (projectId && projectId !== this.activeProjectId) void this.activateProject(projectId);
      });
    } catch {
      this.errorMessage.set('Could not load connected projects.');
    } finally {
      this.loading.set(false);
    }
  }

  ngOnDestroy(): void {
    this.boardSubscription?.unsubscribe();
    this.projectSubscription?.unsubscribe();
  }

  setSearch(event: Event): void {
    this.search.set((event.target as HTMLInputElement).value);
    this.syncVisibleSelection();
  }

  setFilter(filter: FeatureFilter): void {
    this.filter.set(filter);
    this.syncVisibleSelection();
  }

  setSort(event: Event): void {
    this.sort.set((event.target as HTMLSelectElement).value as FeatureSort);
  }

  selectFeature(feature: FeatureEntry): void {
    this.selectedFeatureId.set(feature.item.id);
    this.selectedArtifact.set(null);
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { featureId: feature.item.key },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }

  isSelected(feature: FeatureEntry): boolean {
    return this.selectedFeatureId() === feature.item.id;
  }

  stateLabel(state: FeatureState): string {
    return {
      backlog: 'Backlog',
      planning: 'Planning',
      in_progress: 'In progress',
      blocked: 'Needs attention',
      shipped: 'Shipped',
      not_feasible: 'Not feasible',
    }[state];
  }

  lifecycle(feature: FeatureEntry): LifecycleStage[] {
    const plan = feature.plan;
    const designerTasks = feature.tasks.filter((task) => task.role === 'designer');
    const developerTasks = feature.tasks.filter((task) => task.role === 'developer');
    const qaTasks = feature.tasks.filter((task) => task.role === 'qa');
    const approvedReview = plan ? [...plan.reviews].reverse().find((review) => review.decision === 'approve') : undefined;
    const planDate = approvedReview?.createdAt ?? plan?.updatedAt;

    return [
      { label: 'Request', icon: 'inbox', state: 'complete', date: feature.item.createdAt },
      { label: 'Plan', icon: 'account_tree', state: this.planStageState(plan), date: planDate },
      {
        label: 'Design',
        icon: 'palette',
        state: this.taskStageState(designerTasks, feature.item.requiresUI, plan),
        date: this.latestTaskDate(designerTasks),
      },
      {
        label: 'Build',
        icon: 'terminal',
        state: this.taskStageState(developerTasks, true, plan),
        date: this.latestTaskDate(developerTasks),
      },
      {
        label: 'QA',
        icon: 'verified',
        state: this.taskStageState(qaTasks, true, plan),
        date: this.latestTaskDate(qaTasks),
      },
      {
        label: 'Shipped',
        icon: 'rocket_launch',
        state: feature.state === 'shipped' ? 'complete' : feature.state === 'not_feasible' ? 'blocked' : 'pending',
        date: feature.state === 'shipped' ? feature.item.updatedAt : undefined,
      },
    ];
  }

  ticketGroups(feature: FeatureEntry): TicketGroup[] {
    const definitions: Array<{ role: AgentRole; label: string; icon: string }> = [
      { role: 'designer', label: 'Designer', icon: 'palette' },
      { role: 'developer', label: 'Developer', icon: 'terminal' },
      { role: 'qa', label: 'QA', icon: 'verified' },
    ];
    return definitions
      .map((definition) => ({
        ...definition,
        tasks: feature.tasks
          .filter((task) => task.role === definition.role)
          .sort((left, right) => (left.sequenceOrder ?? 999) - (right.sequenceOrder ?? 999)),
      }))
      .filter((group) => group.tasks.length > 0);
  }

  taskStatusLabel(status: TaskStatus): string {
    return {
      planned: 'Planned',
      queued: 'Queued',
      in_progress: 'In progress',
      verifying: 'Build verifying',
      design_review: 'PM review',
      blocked: 'Blocked',
      completed: 'Completed',
    }[status];
  }

  artifacts(feature: FeatureEntry): FeatureArtifact[] {
    const artifacts: FeatureArtifact[] = [];
    const seen = new Set<string>();
    for (const task of feature.tasks) {
      const executions = [
        ...(task.revisionHistory ?? []).map((revision) => ({
          execution: revision.execution,
          createdAt: revision.execution.completedAt || revision.requestedAt,
        })),
        ...(task.execution ? [{ execution: task.execution, createdAt: task.execution.completedAt }] : []),
      ];
      for (const record of executions) {
        for (const artifact of record.execution.artifacts ?? []) {
          const key = `${task.id}:${artifact.id}`;
          if (seen.has(key)) continue;
          seen.add(key);
          artifacts.push({ task, artifact, createdAt: record.createdAt || task.updatedAt });
        }
      }
    }
    return artifacts.sort((left, right) => right.createdAt.localeCompare(left.createdAt));
  }

  artifactUrl(entry: FeatureArtifact): string {
    return this.boardApi.taskArtifactUrl(
      this.selectedProjectId(),
      entry.task.id,
      entry.artifact.id,
    );
  }

  designSources(feature: FeatureEntry): FeatureDesignSource[] {
    const sources: FeatureDesignSource[] = [];
    const seen = new Set<string>();
    for (const task of feature.tasks.filter((candidate) => candidate.role === 'designer')) {
      const taskDirectory = `.productcrew/design-artifacts/${task.id}/`;
      const executions = [
        ...(task.revisionHistory ?? []).map((revision) => ({
          execution: revision.execution,
          createdAt: revision.execution.completedAt || revision.requestedAt,
        })),
        ...(task.execution ? [{ execution: task.execution, createdAt: task.execution.completedAt }] : []),
      ];
      for (const record of executions) {
        for (const changedFile of record.execution.changedFiles ?? []) {
          const path = changedFile.trim().replaceAll('\\', '/');
          if (!path.toLowerCase().endsWith('.html') || !path.startsWith(taskDirectory) || seen.has(path)) continue;
          seen.add(path);
          sources.push({
            task,
            path,
            title: this.designSourceTitle(path),
            createdAt: record.createdAt || task.updatedAt,
          });
        }
      }
    }
    return sources.sort((left, right) => right.createdAt.localeCompare(left.createdAt));
  }

  designSourceHref(entry: FeatureDesignSource): string {
    return this.boardApi.taskDesignSourceUrl(this.selectedProjectId(), entry.task.id, entry.path);
  }

  designPreviewCount(feature: FeatureEntry): number {
    return this.artifacts(feature).length + this.designSources(feature).length;
  }

  history(feature: FeatureEntry): HistoryEvent[] {
    const events: HistoryEvent[] = [
      {
        id: `${feature.item.id}:request`,
        label: 'Feature request created',
        actor: feature.item.source || 'PM request',
        createdAt: feature.item.createdAt,
        tone: 'success',
      },
    ];
    const plan = feature.plan;
    if (plan) {
      events.push({
        id: `${plan.id}:created`,
        label: 'Team Lead planning started',
        actor: 'Team Lead',
        createdAt: plan.createdAt,
        tone: 'active',
      });
      for (const review of plan.reviews) {
        events.push({
          id: `${plan.id}:review:${review.revision}:${review.createdAt}`,
          label: review.decision === 'approve' ? 'Plan approved' : 'Plan changes requested',
          actor: review.reviewer || 'PM',
          createdAt: review.createdAt,
          tone: review.decision === 'approve' ? 'success' : 'warning',
        });
      }
    }
    for (const task of feature.tasks) {
      for (const revision of task.revisionHistory ?? []) {
        events.push({
          id: `${task.id}:revision:${revision.revision}`,
          label: `${task.key} revision ${revision.revision} submitted`,
          actor: this.roleLabel(task.role),
          createdAt: revision.execution.completedAt || revision.requestedAt,
          tone: 'warning',
        });
      }
      if (task.status === 'completed' || task.status === 'blocked' || task.status === 'design_review') {
        events.push({
          id: `${task.id}:${task.status}`,
          label: this.taskHistoryLabel(task),
          actor: this.roleLabel(task.role),
          createdAt: task.execution?.completedAt || task.updatedAt,
          tone: task.status === 'blocked' ? 'error' : task.status === 'design_review' ? 'warning' : 'success',
        });
      }
      const verification = task.buildVerification ?? task.execution?.buildVerification;
      if (verification?.completedAt) {
        events.push({
          id: `${task.id}:build:${verification.runId}`,
          label: verification.status === 'passed' ? 'Build verification passed' : 'Build verification failed',
          actor: verification.actionLabel || 'Build verify',
          createdAt: verification.completedAt,
          tone: verification.status === 'passed' ? 'success' : 'error',
        });
      }
    }
    if (feature.state === 'shipped') {
      events.push({
        id: `${feature.item.id}:shipped`,
        label: 'Feature shipped',
        actor: 'ProductCrew',
        createdAt: feature.item.updatedAt,
        tone: 'success',
      });
    }
    return events
      .filter((event) => !!event.createdAt)
      .sort((left, right) => left.createdAt.localeCompare(right.createdAt))
      .slice(-8);
  }

  workBoardQuery(feature: FeatureEntry): Record<string, string | null> {
    return {
      projectId: this.selectedProjectId(),
      taskId: feature.tasks[0]?.id ?? null,
    };
  }

  trackStage(index: number, stage: LifecycleStage): string {
    return `${index}:${stage.label}`;
  }

  private async activateProject(projectId: string): Promise<void> {
    this.activeProjectId = projectId;
    this.selectedArtifact.set(null);
    await this.loadBoard(projectId);
  }

  private async loadBoard(projectId: string): Promise<void> {
    const sequence = ++this.loadSequence;
    this.loading.set(true);
    this.errorMessage.set(null);
    this.streamState.set('loading');
    this.boardSubscription?.unsubscribe();
    try {
      const board = await firstValueFrom(this.boardApi.getBoard(projectId));
      if (sequence !== this.loadSequence) return;
      this.board.set(board);
      this.lastSyncedAt.set(new Date().toISOString());
      this.syncSelection();
      this.boardSubscription = this.boardApi.watchBoardStream(projectId).subscribe({
        next: (update) => {
          this.streamState.set(update.state === 'connected' ? 'live' : 'reconnecting');
          if (!update.board || update.board.projectId !== this.selectedProjectId()) return;
          this.board.set(update.board);
          this.lastSyncedAt.set(new Date().toISOString());
          this.syncSelection();
        },
        error: () => {
          this.streamState.set('stale');
          this.errorMessage.set('Feature Library live updates paused. Showing the last loaded board snapshot.');
        },
      });
    } catch {
      if (sequence !== this.loadSequence) return;
      this.board.set(null);
      this.streamState.set('stale');
      this.errorMessage.set('Could not load the Feature Library for this project.');
    } finally {
      if (sequence === this.loadSequence) this.loading.set(false);
    }
  }

  private syncSelection(): void {
    const features = this.features();
    const requested = this.pendingFeatureReference;
    const selected = this.selectedFeatureId();
    const next =
      features.find((feature) => feature.item.id === selected) ??
      features.find((feature) => feature.item.id === requested || feature.item.key === requested) ??
      this.filteredFeatures()[0] ??
      features[0];
    this.pendingFeatureReference = '';
    this.selectedFeatureId.set(next?.item.id ?? '');
  }

  private syncVisibleSelection(): void {
    const visible = this.filteredFeatures();
    if (visible.some((feature) => feature.item.id === this.selectedFeatureId())) return;
    this.selectedFeatureId.set(visible[0]?.item.id ?? '');
    this.selectedArtifact.set(null);
  }

  private designSourceTitle(path: string): string {
    const filename = path.split('/').at(-1)?.replace(/\.html$/i, '') ?? 'Design preview';
    return filename
      .split(/[-_]+/)
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  private toFeatureEntry(board: ProjectBoard, item: BacklogItem): FeatureEntry {
    const plan = item.planId ? board.plans.find((candidate) => candidate.id === item.planId) ?? null : null;
    const tasks = plan ? board.tasks.filter((task) => task.planId === plan.id) : [];
    const state = this.featureState(item, plan, tasks);
    const updatedAt = [item.updatedAt, plan?.updatedAt, ...tasks.map((task) => task.updatedAt)]
      .filter((value): value is string => !!value)
      .sort()
      .at(-1) ?? item.updatedAt;
    const provisional: FeatureEntry = { item, plan, tasks, state, progress: 0, updatedAt };
    const completedStages = this.lifecycle(provisional).filter((stage) => stage.state === 'complete').length;
    return { ...provisional, progress: Math.round((completedStages / 6) * 100) };
  }

  private featureState(item: BacklogItem, plan: BoardPlan | null, tasks: BoardTask[]): FeatureState {
    if (item.status === 'done') return 'shipped';
    if (item.status === 'not_feasible' || plan?.status === 'not_feasible') return 'not_feasible';
    if (tasks.some((task) => task.status === 'blocked') || plan?.status === 'failed') return 'blocked';
    if (tasks.some((task) => ['queued', 'in_progress', 'verifying', 'design_review'].includes(task.status))) {
      return 'in_progress';
    }
    if (item.status === 'planning' || plan) return 'planning';
    return 'backlog';
  }

  private planStageState(plan: BoardPlan | null): StageState {
    if (!plan) return 'pending';
    if (plan.status === 'failed' || plan.status === 'not_feasible') return 'blocked';
    if (['approved', 'completed'].includes(plan.status)) return 'complete';
    return 'active';
  }

  private taskStageState(tasks: BoardTask[], required: boolean, plan: BoardPlan | null): StageState {
    if (!required && plan && ['approved', 'completed'].includes(plan.status)) return 'complete';
    if (tasks.length === 0) return 'pending';
    if (tasks.some((task) => task.status === 'blocked')) return 'blocked';
    if (tasks.every((task) => task.status === 'completed')) return 'complete';
    if (tasks.some((task) => ['queued', 'in_progress', 'verifying', 'design_review'].includes(task.status))) {
      return 'active';
    }
    return 'pending';
  }

  private latestTaskDate(tasks: BoardTask[]): string | undefined {
    return tasks.map((task) => task.execution?.completedAt || task.updatedAt).sort().at(-1);
  }

  private taskHistoryLabel(task: BoardTask): string {
    if (task.status === 'blocked') return `${task.key} needs attention`;
    if (task.status === 'design_review') return `${task.key} awaiting PM approval`;
    return {
      designer: `${task.key} design completed`,
      developer: `${task.key} implementation completed`,
      qa: `${task.key} QA passed`,
    }[task.role];
  }

  private roleLabel(role: AgentRole): string {
    return { designer: 'Designer', developer: 'Developer', qa: 'QA' }[role];
  }
}
