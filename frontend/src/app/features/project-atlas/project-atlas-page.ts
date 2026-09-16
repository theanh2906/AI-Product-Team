import { DatePipe, NgTemplateOutlet } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { Title } from '@angular/platform-browser';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { firstValueFrom, Subscription } from 'rxjs';

import { ProjectContextService } from '../../core/project-context.service';
import { ProjectIntelligenceApiService } from '../../core/project-intelligence-api.service';
import {
  ProjectStudyComponent,
  ProjectStudyDataEntity,
  ProjectStudyFolder,
  ProjectStudyJob,
  ProjectStudySequence,
  ProjectStudyWorkflow,
} from '../../core/project.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type AtlasLens = 'architecture' | 'folders' | 'data' | 'sequences' | 'workflows';
type ProjectStudySequenceStep = ProjectStudySequence['steps'][number];

const LENSES: Array<{ id: AtlasLens; label: string; icon: string }> = [
  { id: 'architecture', label: 'Architecture', icon: 'account_tree' },
  { id: 'folders', label: 'Folder map', icon: 'folder_open' },
  { id: 'data', label: 'Data model', icon: 'database' },
  { id: 'sequences', label: 'Sequences', icon: 'conversion_path' },
  { id: 'workflows', label: 'Workflows', icon: 'route' },
];

@Component({
  selector: 'app-project-atlas-page',
  imports: [AppShell, DatePipe, NgTemplateOutlet, RouterLink],
  templateUrl: './project-atlas-page.html',
})
export class ProjectAtlasPage implements OnInit, OnDestroy {
  private readonly api = inject(ProjectIntelligenceApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly route = inject(ActivatedRoute);
  private readonly title = inject(Title);
  private projectSubscription?: Subscription;
  private studySubscription?: Subscription;
  private activeProjectId = '';
  private toastTimer?: ReturnType<typeof setTimeout>;

  readonly selectedProject = this.projectContext.selectedProject;
  readonly selectedProjectId = this.projectContext.selectedProjectId;
  readonly lenses = LENSES;
  readonly lens = signal<AtlasLens>('architecture');
  readonly job = signal<ProjectStudyJob | null>(null);
  readonly loading = signal(true);
  readonly applying = signal(false);
  readonly errorMessage = signal<string | null>(null);
  readonly successMessage = signal<string | null>(null);
  readonly selectedComponentId = signal('');
  readonly selectedFolderPath = signal('');
  readonly selectedEntityName = signal('');
  readonly selectedSequenceId = signal('');
  readonly selectedWorkflowId = signal('');
  readonly skillsOpen = signal(false);

  readonly result = computed(() => this.job()?.result ?? null);
  readonly studying = computed(() => this.job()?.status === 'running');
  readonly selectedComponent = computed(() => this.pickBy(this.result()?.components ?? [], this.selectedComponentId(), 'id'));
  readonly selectedFolder = computed(() => this.pickBy(this.result()?.folders ?? [], this.selectedFolderPath(), 'path'));
  readonly selectedEntity = computed(() => this.pickBy(this.result()?.dataEntities ?? [], this.selectedEntityName(), 'name'));
  readonly selectedSequence = computed(() => this.pickBy(this.result()?.sequences ?? [], this.selectedSequenceId(), 'id'));
  readonly selectedWorkflow = computed(() => this.pickBy(this.result()?.workflows ?? [], this.selectedWorkflowId(), 'id'));
  readonly selectedSequenceParticipants = computed(() => this.sequenceParticipants(this.selectedSequence()));
  readonly visibleEvidence = computed(() => {
    if (this.lens() === 'architecture') return this.selectedComponent()?.evidence ?? [];
    if (this.lens() === 'data') return this.selectedEntity()?.evidence ?? [];
    if (this.lens() === 'sequences') return this.selectedSequence()?.evidence ?? [];
    if (this.lens() === 'workflows') return this.selectedWorkflow()?.evidence ?? [];
    return [];
  });
  readonly statusLabel = computed(() => {
    if (this.studying()) return `Studying · ${this.job()?.progress ?? 0}%`;
    if (this.result()?.freshness === 'stale') return 'Refresh recommended';
    if (this.job()?.status === 'needs_attention') return 'Partial study';
    if (this.result()) return 'Knowledge ready';
    return 'Not studied';
  });

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Project Atlas · ProductCrew');
    try {
      await this.projectContext.initialize(this.route.snapshot.queryParamMap.get('projectId') ?? undefined);
      const projectId = this.selectedProjectId();
      if (projectId) await this.activateProject(projectId);
      else this.loading.set(false);
      this.projectSubscription = this.projectContext.selectionChanges.subscribe((nextProjectId) => {
        if (nextProjectId && nextProjectId !== this.activeProjectId) void this.activateProject(nextProjectId);
      });
    } catch (error) {
      this.loading.set(false);
      this.errorMessage.set(this.errorText(error, 'Could not load Project Atlas.'));
    }
  }

  ngOnDestroy(): void {
    this.projectSubscription?.unsubscribe();
    this.studySubscription?.unsubscribe();
    if (this.toastTimer) clearTimeout(this.toastTimer);
  }

  async runStudy(): Promise<void> {
    const projectId = this.selectedProjectId();
    if (!projectId || this.studying()) return;
    this.clearNotices();
    try {
      const job = await firstValueFrom(this.api.start(projectId));
      this.applyJob(job);
      if (job.status === 'running') this.connect(projectId, job.studyId);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not start the project study.'));
    }
  }

  async applyGuidance(): Promise<void> {
    const result = this.result();
    if (!result || this.applying() || result.roleGuidance.length === 0) return;
    this.applying.set(true);
    this.clearNotices();
    try {
      const response = await firstValueFrom(this.api.applyGuidance(result.projectId, result.studyId));
      this.job.update((job) => job ? { ...job, result: response.result } : job);
      this.showSuccess(`Project guidance applied to ${response.result.roleGuidance.length} agent roles.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not apply the learned project guidance.'));
    } finally {
      this.applying.set(false);
    }
  }

  setLens(lens: AtlasLens): void {
    this.lens.set(lens);
  }

  selectComponent(component: ProjectStudyComponent): void { this.selectedComponentId.set(component.id); }
  selectFolder(folder: ProjectStudyFolder): void { this.selectedFolderPath.set(folder.path); }
  selectEntity(entity: ProjectStudyDataEntity): void { this.selectedEntityName.set(entity.name); }
  selectSequence(sequence: ProjectStudySequence): void { this.selectedSequenceId.set(sequence.id); }
  selectWorkflow(workflow: ProjectStudyWorkflow): void { this.selectedWorkflowId.set(workflow.id); }

  sequenceParticipants(sequence: ProjectStudySequence | null): string[] {
    if (!sequence) return [];
    const participants: string[] = [];
    for (const step of sequence.steps) {
      for (const value of [step.from, step.to]) {
        const label = String(value || '').trim();
        if (label && !participants.includes(label)) participants.push(label);
      }
    }
    return participants.length ? participants : ['System'];
  }

  sequenceGridColumns(participants: string[]): string {
    return `repeat(${Math.max(participants.length, 1)}, minmax(120px, 1fr))`;
  }

  sequenceStepGridColumn(step: ProjectStudySequenceStep, participants: string[]): string {
    const from = Math.max(participants.indexOf(step.from), 0) + 1;
    const toIndex = participants.indexOf(step.to);
    const to = (toIndex >= 0 ? toIndex : from - 1) + 1;
    return `${Math.min(from, to)} / ${Math.max(from, to) + 1}`;
  }

  sequenceStepAlignment(step: ProjectStudySequenceStep, participants: string[]): string {
    const from = participants.indexOf(step.from);
    const to = participants.indexOf(step.to);
    if (from === to || to < 0) return 'center';
    return from < to ? 'left' : 'right';
  }

  workflowStageKind(index: number, total: number): string {
    if (index === 0) return 'Start';
    if (index === total - 1) return 'Finish';
    return 'Stage';
  }

  relationCount(componentId: string): number {
    return (this.result()?.relations ?? []).filter((relation) => relation.from === componentId || relation.to === componentId).length;
  }

  relatedComponents(componentId: string): ProjectStudyComponent[] {
    const result = this.result();
    if (!result) return [];
    const ids = new Set(result.relations.flatMap((relation) => relation.from === componentId ? [relation.to] : relation.to === componentId ? [relation.from] : []));
    return result.components.filter((component) => ids.has(component.id));
  }

  lensCount(lens: AtlasLens): number {
    const result = this.result();
    if (!result) return 0;
    return {
      architecture: result.components.length,
      folders: result.folders.length,
      data: result.dataEntities.length,
      sequences: result.sequences.length,
      workflows: result.workflows.length,
    }[lens];
  }

  iconForKind(kind: string): string {
    const value = kind.toLowerCase();
    if (value.includes('front') || value.includes('ui')) return 'web_asset';
    if (value.includes('data') || value.includes('store')) return 'database';
    if (value.includes('test') || value.includes('qa')) return 'verified';
    if (value.includes('infra') || value.includes('deploy')) return 'cloud';
    if (value.includes('api') || value.includes('back')) return 'dns';
    return 'deployed_code';
  }

  roleLabel(role: string): string {
    return ({ 'team-lead': 'Team Lead', designer: 'Designer', developer: 'Developer', qa: 'QA', 'bug-scanner': 'Bug Scanner', 'feature-radar': 'Feature Radar' } as Record<string, string>)[role] ?? role;
  }

  private async activateProject(projectId: string): Promise<void> {
    this.activeProjectId = projectId;
    this.studySubscription?.unsubscribe();
    this.studySubscription = undefined;
    this.job.set(null);
    this.resetSelections();
    this.clearNotices();
    this.loading.set(true);
    try {
      const job = await firstValueFrom(this.api.getLatest(projectId));
      if (job) {
        this.applyJob(job);
        if (job.status === 'running') this.connect(projectId, job.studyId);
      }
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not restore saved project knowledge.'));
    } finally {
      this.loading.set(false);
    }
  }

  private connect(projectId: string, studyId: string): void {
    this.studySubscription?.unsubscribe();
    this.studySubscription = this.api.watch(projectId, studyId).subscribe({
      next: (job) => this.applyJob(job),
      error: (error: Error) => this.errorMessage.set(error.message),
    });
  }

  private applyJob(job: ProjectStudyJob): void {
    if (job.projectId !== this.activeProjectId) return;
    this.job.set(job);
    if (job.result) this.ensureSelections(job.result);
    if (job.status === 'needs_attention') this.errorMessage.set(this.projectStudyError(job));
    if (job.status === 'completed' && job.result) {
      this.errorMessage.set(null);
      this.showSuccess('Project study completed. Atlas knowledge is ready.');
    }
  }

  private ensureSelections(result: NonNullable<ProjectStudyJob['result']>): void {
    if (!result.components.some((item) => item.id === this.selectedComponentId())) this.selectedComponentId.set(result.components[0]?.id ?? '');
    if (!result.folders.some((item) => item.path === this.selectedFolderPath())) this.selectedFolderPath.set(result.folders[0]?.path ?? '');
    if (!result.dataEntities.some((item) => item.name === this.selectedEntityName())) this.selectedEntityName.set(result.dataEntities[0]?.name ?? '');
    if (!result.sequences.some((item) => item.id === this.selectedSequenceId())) this.selectedSequenceId.set(result.sequences[0]?.id ?? '');
    if (!result.workflows.some((item) => item.id === this.selectedWorkflowId())) this.selectedWorkflowId.set(result.workflows[0]?.id ?? '');
  }

  private resetSelections(): void {
    this.selectedComponentId.set('');
    this.selectedFolderPath.set('');
    this.selectedEntityName.set('');
    this.selectedSequenceId.set('');
    this.selectedWorkflowId.set('');
    this.lens.set('architecture');
    this.skillsOpen.set(false);
  }

  private pickBy<T>(items: T[], value: string, key: keyof T): T | null {
    return items.find((item) => String(item[key]) === value) ?? items[0] ?? null;
  }

  private clearNotices(): void {
    this.errorMessage.set(null);
    this.successMessage.set(null);
    if (this.toastTimer) clearTimeout(this.toastTimer);
  }

  private showSuccess(message: string): void {
    this.successMessage.set(message);
    if (this.toastTimer) clearTimeout(this.toastTimer);
    this.toastTimer = setTimeout(() => this.successMessage.set(null), 5000);
  }

  private projectStudyError(job: ProjectStudyJob): string {
    return job.error || job.result?.enrichmentError || 'AI enrichment was limited. Folder Map is still available.';
  }

  private errorText(error: unknown, fallback: string): string {
    return (error as HttpErrorResponse).error?.error ?? fallback;
  }
}
