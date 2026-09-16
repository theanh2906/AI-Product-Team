import { computed, inject, Injectable, signal } from '@angular/core';
import { firstValueFrom, Observable, ReplaySubject } from 'rxjs';

import { ProjectApiService } from './project-api.service';
import { ImportedProject } from './project.models';

const SELECTED_PROJECT_STORAGE_KEY = 'productcrew.selected-project-id';

@Injectable({ providedIn: 'root' })
export class ProjectContextService {
  private readonly projectApi = inject(ProjectApiService);
  private readonly selectionSubject = new ReplaySubject<string>(1);
  private initialization?: Promise<void>;

  readonly projects = signal<ImportedProject[]>([]);
  readonly selectedProjectId = signal('');
  readonly selectedProject = computed(
    () => this.projects().find((project) => project.id === this.selectedProjectId()) ?? null,
  );
  readonly loading = signal(true);
  readonly error = signal<string | null>(null);
  readonly selectionChanges: Observable<string> = this.selectionSubject.asObservable();

  async initialize(preferredProjectId?: string): Promise<void> {
    if (!this.initialization) this.initialization = this.loadProjects(false);
    await this.initialization;

    if (preferredProjectId && this.hasProject(preferredProjectId)) {
      this.select(preferredProjectId);
    }
  }

  async refresh(): Promise<void> {
    await this.loadProjects(true);
  }

  select(projectId: string): boolean {
    if (!this.hasProject(projectId)) return false;
    if (projectId === this.selectedProjectId()) return true;

    this.selectedProjectId.set(projectId);
    this.persist(projectId);
    this.selectionSubject.next(projectId);
    return true;
  }

  private async loadProjects(refresh: boolean): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      const projects = await firstValueFrom(this.projectApi.listProjects(refresh));
      this.projects.set(projects);

      const current = this.selectedProjectId();
      const persisted = this.readPersisted();
      const next =
        projects.find((project) => project.id === current)?.id ??
        projects.find((project) => project.id === persisted)?.id ??
        projects[0]?.id ??
        '';

      if (!next) {
        this.selectedProjectId.set('');
        this.persist('');
        this.selectionSubject.next('');
      } else if (next !== current) {
        this.selectedProjectId.set(next);
        this.persist(next);
        this.selectionSubject.next(next);
      }
    } catch {
      this.error.set('Could not load connected projects.');
    } finally {
      this.loading.set(false);
    }
  }

  private hasProject(projectId: string): boolean {
    return this.projects().some((project) => project.id === projectId);
  }

  private readPersisted(): string {
    if (typeof localStorage === 'undefined') return '';
    return localStorage.getItem(SELECTED_PROJECT_STORAGE_KEY) ?? '';
  }

  private persist(projectId: string): void {
    if (typeof localStorage === 'undefined') return;
    if (projectId) localStorage.setItem(SELECTED_PROJECT_STORAGE_KEY, projectId);
    else localStorage.removeItem(SELECTED_PROJECT_STORAGE_KEY);
  }
}
