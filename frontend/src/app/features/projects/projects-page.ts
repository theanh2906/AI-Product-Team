import { HttpErrorResponse } from '@angular/common/http';
import { Component, ElementRef, computed, inject, OnInit, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ProjectApiService } from '../../core/project-api.service';
import { ProjectContextService } from '../../core/project-context.service';
import {
  GitCredential,
  GitRepository,
  ImportedProject,
  ProjectSettings,
} from '../../core/project.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type ImportMode = 'git' | 'local';

@Component({
  selector: 'app-projects-page',
  imports: [ReactiveFormsModule, RouterLink, AppShell],
  templateUrl: './projects-page.html',
})
export class ProjectsPage implements OnInit {
  private readonly formBuilder = inject(FormBuilder);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly projectApi = inject(ProjectApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly title = inject(Title);

  readonly importMode = signal<ImportMode>('git');
  readonly projects = signal<ImportedProject[]>([]);
  readonly gitRepositories = signal<GitRepository[]>([]);
  readonly selectedGitRepository = signal<GitRepository | null>(null);
  readonly gitCredentials = signal<GitCredential[]>([]);
  readonly selectedGitCredential = signal<GitCredential | null>(null);
  readonly settings = signal<ProjectSettings | null>(null);
  readonly loading = signal(true);
  readonly repositoriesLoading = signal(false);
  readonly credentialsLoading = signal(false);
  readonly importing = signal(false);
  readonly errorMessage = signal<string | null>(null);
  readonly errorTitle = signal('Import needs attention');
  readonly successMessage = signal<string | null>(null);
  readonly successTitle = signal('Project imported');
  readonly repositoryError = signal<string | null>(null);
  readonly credentialError = signal<string | null>(null);
  readonly search = signal('');
  readonly confirmingRemovalProjectId = signal<string | null>(null);
  readonly removingProjectId = signal<string | null>(null);

  readonly gitForm = this.formBuilder.nonNullable.group({
    credentialLogin: [{ value: '', disabled: true }],
    repositoryUrl: [{ value: '', disabled: true }, [Validators.required]],
  });
  readonly localForm = this.formBuilder.nonNullable.group({
    path: ['', [Validators.required]],
  });

  readonly filteredProjects = computed(() => {
    const query = this.search().trim().toLowerCase();
    if (!query) return this.projects();
    return this.projects().filter((project) =>
      [project.name, project.path, project.remoteUrl, project.branch]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(query)),
    );
  });
  readonly gitProjectCount = computed(
    () => this.projects().filter((project) => project.source === 'git').length,
  );
  readonly localProjectCount = computed(
    () => this.projects().filter((project) => project.source === 'local').length,
  );

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Projects · ProductCrew');
    try {
      const [settings, projects] = await Promise.all([
        firstValueFrom(this.projectApi.getSettings()),
        firstValueFrom(this.projectApi.listProjects()),
      ]);
      this.settings.set(settings);
      this.projects.set(projects);
      if (settings.gitProvider === 'github') {
        await this.loadGitCredentials();
        if (this.githubReady) await this.loadGitRepositories();
      }
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load projects.'));
    } finally {
      this.loading.set(false);
    }
  }

  setImportMode(mode: ImportMode): void {
    this.importMode.set(mode);
    this.errorMessage.set(null);
    this.successMessage.set(null);
  }

  setSearch(event: Event): void {
    this.search.set((event.target as HTMLInputElement).value);
  }

  isConfirmingRemoval(projectId: string): boolean {
    return this.confirmingRemovalProjectId() === projectId;
  }

  isRemoving(projectId: string): boolean {
    return this.removingProjectId() === projectId;
  }

  removeConfirmationHeadingId(projectId: string): string {
    return `remove-project-${projectId.replace(/[^A-Za-z0-9_-]/g, '-')}`;
  }

  get destinationPreview(): string {
    const clonePath = this.settings()?.clonePath ?? 'Configure a clone path in Settings';
    return this.selectedGitRepository()
      ? `${clonePath}\\${this.selectedGitRepository()!.name}`
      : clonePath;
  }

  get githubReady(): boolean {
    return (
      this.settings()?.gitProvider === 'github' &&
      (!!this.selectedGitCredential() || !!this.settings()?.patConfigured)
    );
  }

  async loadGitCredentials(): Promise<void> {
    this.credentialsLoading.set(true);
    this.credentialError.set(null);
    try {
      const credentials = await firstValueFrom(this.projectApi.listGitCredentials());
      this.gitCredentials.set(credentials);
      this.selectedGitCredential.set(
        credentials.find((credential) => credential.selected) ??
          credentials.find((credential) => credential.active) ??
          null,
      );
      this.gitForm.controls.credentialLogin.setValue(this.selectedGitCredential()?.login ?? '');
    } catch (error) {
      this.credentialError.set(
        this.errorText(error, 'Could not load accounts configured in GitHub CLI.'),
      );
    } finally {
      this.credentialsLoading.set(false);
      if (this.gitCredentials().length > 0) {
        this.gitForm.controls.credentialLogin.enable({ emitEvent: false });
      }
    }
  }

  async switchGitCredential(event: Event): Promise<void> {
    const login = (event.target as HTMLSelectElement).value;
    if (!login || login === this.selectedGitCredential()?.login) return;
    this.credentialsLoading.set(true);
    this.gitForm.controls.credentialLogin.disable({ emitEvent: false });
    this.gitForm.controls.repositoryUrl.disable({ emitEvent: false });
    this.credentialError.set(null);
    this.repositoryError.set(null);
    this.gitRepositories.set([]);
    this.selectedGitRepository.set(null);
    this.gitForm.controls.repositoryUrl.setValue('');
    try {
      const selected = await firstValueFrom(this.projectApi.selectGitCredential(login));
      this.gitCredentials.update((credentials) =>
        credentials.map((credential) => ({
          ...credential,
          active: credential.login === selected.login,
          selected: credential.login === selected.login,
        })),
      );
      this.selectedGitCredential.set(selected);
      this.gitForm.controls.credentialLogin.setValue(selected.login);
      const currentSettings = this.settings();
      if (currentSettings) {
        this.settings.set({
          ...currentSettings,
          githubAccount: selected.login,
          credentialType: 'GitHub CLI keyring',
        });
      }
      await this.loadGitRepositories(true);
    } catch (error) {
      this.credentialError.set(this.errorText(error, 'Could not switch GitHub account.'));
    } finally {
      this.credentialsLoading.set(false);
      this.gitForm.controls.credentialLogin.enable({ emitEvent: false });
    }
  }

  async loadGitRepositories(refresh = false): Promise<void> {
    this.repositoriesLoading.set(true);
    this.gitForm.controls.repositoryUrl.disable({ emitEvent: false });
    this.repositoryError.set(null);
    try {
      const repositories = await firstValueFrom(this.projectApi.listGitRepositories(refresh));
      this.gitRepositories.set(repositories);
      const selectedURL = this.gitForm.controls.repositoryUrl.value;
      this.selectedGitRepository.set(
        repositories.find((repository) => repository.cloneUrl === selectedURL) ?? null,
      );
    } catch (error) {
      this.repositoryError.set(this.errorText(error, 'Could not load GitHub repositories.'));
    } finally {
      this.repositoriesLoading.set(false);
      if (this.githubReady) this.gitForm.controls.repositoryUrl.enable({ emitEvent: false });
    }
  }

  selectGitRepository(event: Event): void {
    const cloneURL = (event.target as HTMLSelectElement).value;
    this.gitForm.controls.repositoryUrl.setValue(cloneURL);
    this.selectedGitRepository.set(
      this.gitRepositories().find((repository) => repository.cloneUrl === cloneURL) ?? null,
    );
    this.errorMessage.set(null);
  }

  async pickLocalFolder(): Promise<void> {
    this.errorMessage.set(null);
    try {
      const selection = await firstValueFrom(this.projectApi.selectFolder());
      if (selection?.path) this.localForm.controls.path.setValue(selection.path);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not open the folder picker.'));
    }
  }

  async importGit(): Promise<void> {
    if (this.gitForm.invalid) {
      this.gitForm.markAllAsTouched();
      return;
    }
    await this.runImport(() =>
      firstValueFrom(
        this.projectApi.importGit({
          repositoryUrl: this.gitForm.controls.repositoryUrl.value,
          branch: '',
          directoryName: '',
        }),
      ),
    );
    if (!this.errorMessage()) {
      this.gitForm.controls.repositoryUrl.setValue('');
      this.selectedGitRepository.set(null);
    }
  }

  async importLocal(): Promise<void> {
    if (this.localForm.invalid) {
      this.localForm.markAllAsTouched();
      return;
    }
    await this.runImport(() =>
      firstValueFrom(this.projectApi.importLocal(this.localForm.controls.path.value)),
    );
    if (!this.errorMessage()) this.localForm.reset({ path: '' });
  }

  private async runImport(action: () => Promise<ImportedProject>): Promise<void> {
    this.importing.set(true);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    this.errorTitle.set('Import needs attention');
    this.successTitle.set('Project imported');
    try {
      const created = await action();
      this.projects.update((projects) => [created, ...projects]);
      await this.projectContext.refresh();
      this.successMessage.set(`${created.name} is ready for orchestration.`);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not import the project.'));
    } finally {
      this.importing.set(false);
    }
  }

  openProjectRemoval(project: ImportedProject): void {
    this.confirmingRemovalProjectId.set(project.id);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    setTimeout(() => this.focusRemovalConfirmation(project.id));
  }

  cancelProjectRemoval(projectId = this.confirmingRemovalProjectId()): void {
    if (projectId && this.isRemoving(projectId)) return;
    this.confirmingRemovalProjectId.set(null);
    setTimeout(() => this.focusRemoveAction(projectId));
  }

  async confirmProjectRemoval(project: ImportedProject): Promise<void> {
    if (this.removingProjectId()) return;
    this.removingProjectId.set(project.id);
    this.errorMessage.set(null);
    this.successMessage.set(null);
    try {
      await firstValueFrom(this.projectApi.removeProject(project.id));
      this.projects.update((projects) => projects.filter((item) => item.id !== project.id));
      await this.projectContext.refresh();
      this.confirmingRemovalProjectId.set(null);
      this.successTitle.set('Project removed');
      this.successMessage.set(
        `${project.name} was removed from ProductCrew. The repository folder remains on disk.`,
      );
      setTimeout(() => this.focusAfterRemoval());
    } catch (error) {
      const response = error as HttpErrorResponse;
      this.errorTitle.set(
        response.status === 409 ? 'Project cannot be removed yet' : 'Removal needs attention',
      );
      this.errorMessage.set(
        this.errorText(error, 'Could not remove the project. Nothing was deleted from disk.'),
      );
    } finally {
      this.removingProjectId.set(null);
    }
  }

  private errorText(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse;
    return response.error?.error ?? fallback;
  }

  private focusRemovalConfirmation(projectId: string): void {
    this.findElementByData<HTMLElement>('removeConfirmationId', projectId)?.focus();
  }

  private focusRemoveAction(projectId: string | null): void {
    if (!projectId) return;
    this.findElementByData<HTMLButtonElement>('removeProjectId', projectId)?.focus();
  }

  private focusAfterRemoval(): void {
    const root = this.host.nativeElement;
    const nextAction = root.querySelector<HTMLButtonElement>('[data-remove-project-id]');
    const searchInput = root.querySelector<HTMLInputElement>('[data-project-search]');
    const libraryHeading = root.querySelector<HTMLElement>('#project-library-heading');
    (nextAction ?? searchInput ?? libraryHeading)?.focus();
  }

  private findElementByData<T extends HTMLElement>(key: string, value: string): T | null {
    const root = this.host.nativeElement;
    return (
      Array.from(
        root.querySelectorAll('[data-remove-project-id], [data-remove-confirmation-id]'),
      ).find(
        (element): element is T => element instanceof HTMLElement && element.dataset[key] === value,
      ) ?? null
    );
  }
}
