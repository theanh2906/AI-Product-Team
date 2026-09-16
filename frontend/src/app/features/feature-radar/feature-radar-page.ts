import { DatePipe, DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, inject, OnDestroy, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { ActivatedRoute } from '@angular/router';
import { firstValueFrom, Subscription } from 'rxjs';

import { FeatureRadarApiService } from '../../core/feature-radar-api.service';
import { FeatureRadarJob, FeatureRadarResult, FeatureSuggestion } from '../../core/feature-radar.models';
import { ProjectContextService } from '../../core/project-context.service';
import { AppShell } from '../../shared/app-shell/app-shell';

const ACTION_ERROR_COPY: Record<'plan' | 'backlog' | 'ignore', string> = {
  plan: 'Could not start planning for this suggestion.',
  backlog: 'Could not add this suggestion to the backlog.',
  ignore: 'Could not ignore this suggestion.',
};

@Component({ selector: 'app-feature-radar-page', imports: [AppShell, DatePipe, DecimalPipe, FormsModule], templateUrl: './feature-radar-page.html' })
export class FeatureRadarPage implements OnInit, OnDestroy {
  private readonly api = inject(FeatureRadarApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly title = inject(Title);
  private readonly route = inject(ActivatedRoute);
  private events?: Subscription;
  private projectSelection?: Subscription;
  private activeProjectId = '';

  readonly projectId = this.projectContext.selectedProjectId;
  readonly selectedProject = this.projectContext.selectedProject;
  readonly depth = signal('balanced');
  readonly result = signal<FeatureRadarResult | null>(null);
  readonly analyzing = signal(false);
  readonly updating = signal<string | null>(null);
  readonly updatingAction = signal<'plan' | 'backlog' | 'ignore' | null>(null);
  readonly errorMessage = signal<string | null>(null);
  readonly successMessage = signal<string | null>(null);
  readonly activeAnalysisId = signal<string | null>(null);
  readonly suggestions = computed(() => (this.result()?.suggestions ?? []).filter((item) => !item.status || item.status === 'suggested'));
  readonly backlogCount = computed(() => this.result()?.suggestions.filter((item) => item.status === 'backlog').length ?? 0);

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Feature Radar · ProductCrew');
    try {
      await this.projectContext.initialize(this.route.snapshot.queryParamMap.get('projectId') ?? undefined);
      const projectId = this.projectId();
      if (projectId) await this.activateProject(projectId);
      this.projectSelection = this.projectContext.selectionChanges.subscribe((nextProjectId) => {
        if (nextProjectId && nextProjectId !== this.activeProjectId) void this.activateProject(nextProjectId);
      });
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load Feature Radar.'));
    }
  }

  ngOnDestroy(): void {
    this.events?.unsubscribe();
    this.projectSelection?.unsubscribe();
  }

  private async activateProject(projectId: string): Promise<void> {
    this.activeProjectId = projectId;
    this.events?.unsubscribe();
    this.events = undefined;
    this.result.set(null);
    this.activeAnalysisId.set(null);
    this.analyzing.set(false);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    await this.load(projectId);
  }

  async analyze(reanalyze = false): Promise<void> {
    if (!this.projectId() || this.analyzing()) return;
    this.analyzing.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      const job = await firstValueFrom(this.api.start(this.projectId(), this.depth(), reanalyze));
      this.applyJob(job);
      if (job.status === 'running') this.connect(job.analysisId);
    } catch (error) {
      this.analyzing.set(false);
      this.errorMessage.set(this.errorText(error, 'The selected AI runtime could not analyze this project.'));
    }
  }

  async action(item: FeatureSuggestion, action: 'plan' | 'backlog' | 'ignore'): Promise<void> {
    if (this.updating()) return;
    this.updating.set(item.id);
    this.updatingAction.set(action);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      const response = await firstValueFrom(this.api.update(this.projectId(), item.id, action));
      this.result.set(response.result);
      if (action === 'plan') this.successMessage.set(`“${item.title}” moved to Team Lead planning.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, ACTION_ERROR_COPY[action]));
    } finally {
      this.updating.set(null);
      this.updatingAction.set(null);
    }
  }

  private async load(projectId: string): Promise<void> {
    try {
      const job = await firstValueFrom(this.api.getLatest(projectId));
      if (!job) return;
      this.applyJob(job);
      if (job.status === 'running') this.connect(job.analysisId);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not restore the saved analysis.'));
    }
  }

  private connect(analysisId: string): void {
    this.events?.unsubscribe();
    this.events = this.api.watch(analysisId).subscribe({
      next: (job) => this.applyJob(job),
      error: (error) => { this.analyzing.set(false); this.errorMessage.set(error.message); },
    });
  }

  private applyJob(job: FeatureRadarJob): void {
    if (job.projectId !== this.activeProjectId) return;
    this.activeAnalysisId.set(job.analysisId);
    this.analyzing.set(job.status === 'running');
    if (job.result) this.result.set(job.result);
    if (job.status === 'failed') this.errorMessage.set(job.error || 'Feature Radar failed.');
  }

  private errorText(error: unknown, fallback: string): string {
    return (error as HttpErrorResponse).error?.error ?? fallback;
  }
}
