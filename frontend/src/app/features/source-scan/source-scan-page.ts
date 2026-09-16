import { DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, DestroyRef, inject, OnInit, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { ActivatedRoute } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { Subscription } from 'rxjs';

import { ProjectContextService } from '../../core/project-context.service';
import { SourceScanApiService } from '../../core/source-scan-api.service';
import {
  FindingSeverity,
  SourceScanFinding,
  SourceScanJob,
  SourceScanResult,
} from '../../core/source-scan.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type SeverityFilter = 'all' | FindingSeverity;
const ACTIVE_SCAN_STORAGE_KEY = 'productcrew.active-source-scan';
const DEFAULT_REVIEW_FOCUS_AREAS = ['correctness', 'security', 'reliability', 'performance', 'maintainability'];
const ACTION_ERROR_COPY: Record<'plan' | 'backlog' | 'ignore', string> = {
  plan: 'Could not start planning for this finding.',
  backlog: 'Could not add this bug to the backlog.',
  ignore: 'Could not ignore this finding.',
};

@Component({
  selector: 'app-source-scan-page',
  imports: [DecimalPipe, ReactiveFormsModule, AppShell],
  templateUrl: './source-scan-page.html',
})
export class SourceScanPage implements OnInit {
  private readonly formBuilder = inject(FormBuilder);
  private readonly projectContext = inject(ProjectContextService);
  private readonly sourceScanApi = inject(SourceScanApiService);
  private readonly title = inject(Title);
  private readonly route = inject(ActivatedRoute);
  private readonly destroyRef = inject(DestroyRef);
  private destroyed = false;
  private scanEvents?: Subscription;
  private projectSelection?: Subscription;
  private activeProjectId = '';

  readonly form = this.formBuilder.nonNullable.group({
    depth: 'balanced' as 'focused' | 'balanced' | 'deep',
  });
  readonly selectedProject = this.projectContext.selectedProject;
  readonly focusAreas = signal(new Set(DEFAULT_REVIEW_FOCUS_AREAS));
  readonly result = signal<SourceScanResult | null>(null);
  readonly updatingFinding = signal<string | null>(null);
  readonly updatingAction = signal<'plan' | 'backlog' | 'ignore' | null>(null);
  readonly severityFilter = signal<SeverityFilter>('all');
  readonly scanning = signal(false);
  readonly activeScanId = signal<string | null>(null);
  readonly errorMessage = signal<string | null>(null);
  readonly successMessage = signal<string | null>(null);

  readonly visibleFindings = computed(() => {
    const result = this.result();
    if (!result) return [];
    const filter = this.severityFilter();
    return result.findings.filter(
      (finding) => (!finding.status || finding.status === 'suggested') && (filter === 'all' || finding.severity === filter),
    );
  });

  readonly activeFindingCount = computed(
    () => this.result()?.findings.filter((finding) => finding.status === 'suggested' || !finding.status).length ?? 0,
  );

  constructor() {
    this.destroyRef.onDestroy(() => {
      this.destroyed = true;
      this.scanEvents?.unsubscribe();
      this.projectSelection?.unsubscribe();
    });
  }

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Bug Scanner · ProductCrew');
    try {
      await this.projectContext.initialize(this.route.snapshot.queryParamMap.get('projectId') ?? undefined);
      const projectId = this.projectContext.selectedProjectId();
      if (projectId) await this.activateProject(projectId);
      this.projectSelection = this.projectContext.selectionChanges.subscribe((nextProjectId) => {
        if (nextProjectId && nextProjectId !== this.activeProjectId) void this.activateProject(nextProjectId);
      });
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load connected projects.'));
    }
  }

  toggleFocusArea(area: string): void {
    this.focusAreas.update((current) => {
      const next = new Set(current);
      next.has(area) ? next.delete(area) : next.add(area);
      return next;
    });
  }

  focusAreaSelected(area: string): boolean {
    return this.focusAreas().has(area);
  }

  async startScan(): Promise<void> {
    const projectId = this.projectContext.selectedProjectId();
    if (!projectId) {
      this.errorMessage.set('Select an active project before starting the bug scan.');
      return;
    }
    if (this.focusAreas().size === 0) {
      this.errorMessage.set('Select at least one review focus.');
      return;
    }

    this.scanning.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.result.set(null);
    this.severityFilter.set('all');
    try {
      const job = await firstValueFrom(
        this.sourceScanApi.startScan({
          projectId,
          depth: this.form.controls.depth.value,
          focusAreas: [...this.focusAreas()],
        }),
      );
      this.activeScanId.set(job.scanId);
      localStorage.setItem(ACTIVE_SCAN_STORAGE_KEY, job.scanId);
      this.connectToScan(job.scanId);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'The selected AI runtime could not complete the bug scan.'));
    }
  }

  private async activateProject(projectId: string): Promise<void> {
    this.activeProjectId = projectId;
    this.scanEvents?.unsubscribe();
    this.scanEvents = undefined;
    this.result.set(null);
    this.activeScanId.set(null);
    this.scanning.set(false);
    this.severityFilter.set('all');
    this.errorMessage.set(null);
    this.successMessage.set(null);
    await this.resumeLatestScan(projectId);
  }

  private async resumeLatestScan(projectId: string): Promise<void> {
    if (!projectId) return;
    try {
      const job = await firstValueFrom(this.sourceScanApi.getLatestScan(projectId));
      if (!job) {
        localStorage.removeItem(ACTIVE_SCAN_STORAGE_KEY);
        return;
      }
      this.applyScanJob(job);
      if (job.status === 'running') {
        localStorage.setItem(ACTIVE_SCAN_STORAGE_KEY, job.scanId);
        this.connectToScan(job.scanId);
      }
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not restore the latest bug scan.'));
    }
  }

  private connectToScan(scanID: string): void {
    this.scanEvents?.unsubscribe();
    this.scanEvents = this.sourceScanApi.watchScan(scanID).subscribe({
      next: (job) => {
        if (this.destroyed) return;
        this.applyScanJob(job);
      },
      error: (error: unknown) => {
        if (this.destroyed) return;
        localStorage.removeItem(ACTIVE_SCAN_STORAGE_KEY);
        this.activeScanId.set(null);
        this.errorMessage.set(
          error instanceof Error ? error.message : 'Could not reconnect to the bug scan.',
        );
        this.scanning.set(false);
      },
    });
  }

  private applyScanJob(job: SourceScanJob): void {
    if (job.projectId !== this.activeProjectId) return;
    this.activeScanId.set(job.scanId);
    if (job.status === 'running') {
      this.scanning.set(true);
      return;
    }

    this.scanning.set(false);
    localStorage.removeItem(ACTIVE_SCAN_STORAGE_KEY);
    if (job.status === 'completed' && job.result) {
      this.result.set(job.result);
      this.errorMessage.set(null);
      return;
    }
    if (job.status === 'failed') {
      this.errorMessage.set(job.error || 'The selected AI runtime could not complete the bug scan.');
    }
  }

  countSeverity(severity: FindingSeverity): number {
    return (
      this.result()?.findings.filter(
        (finding) => finding.severity === severity && (finding.status === 'suggested' || !finding.status),
      ).length ?? 0
    );
  }

  async updateFinding(finding: SourceScanFinding, action: 'plan' | 'backlog' | 'ignore'): Promise<void> {
    const result = this.result();
    if (!result || this.updatingFinding()) return;
    this.updatingFinding.set(finding.id);
    this.updatingAction.set(action);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      const response = await firstValueFrom(
        this.sourceScanApi.updateFinding(result.projectId, finding.id, action),
      );
      this.result.set(response.result);
      if (action === 'plan') this.successMessage.set(`“${finding.title}” moved to Team Lead planning.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, ACTION_ERROR_COPY[action]));
    } finally {
      this.updatingFinding.set(null);
      this.updatingAction.set(null);
    }
  }

  private errorText(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse;
    return response.error?.error ?? fallback;
  }
}
