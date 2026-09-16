import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Subject, of, throwError } from 'rxjs';

import { ProjectApiService } from '../../core/project-api.service';
import { ImportedProject, ProjectRemovalResult, ProjectSettings } from '../../core/project.models';
import { ProjectsPage } from './projects-page';

const settings: ProjectSettings = {
  clonePath: 'C:\\Workspaces',
  updatePath: 'C:\\Tools\\updates',
  gitProvider: 'github',
  gitUsername: '',
  githubAccount: '',
  aiProvider: 'codex',
  aiFallbackProvider: 'claude-code',
  aiFallbackEnabled: false,
  codexModel: '',
  codexEffort: 'high',
  claudeCodeModel: 'sonnet',
  claudeCodeEffort: 'high',
  githubCopilotModel: 'auto',
  githubCopilotEffort: 'high',
  theme: 'light',
  patConfigured: true,
  credentialType: 'OS keyring',
  configPath: 'C:\\Users\\test\\.productcrew\\settings.json',
  boardDataPath: 'C:\\Users\\test\\.productcrew\\boards.json',
  insightDataPath: 'C:\\Users\\test\\.productcrew\\insights.json',
  eventDataPath: 'C:\\Users\\test\\.productcrew\\events.jsonl',
  logPath: 'C:\\Users\\test\\.productcrew\\logs\\application.jsonl',
  agentProfileVersion: 1,
  workingStandard: '# ProductCrew Working Standard',
  agentSkills: [],
  roleDefinitions: {
    teamLead: '# Team Lead',
    designer: '# Designer',
    developer: '# Developer',
    qa: '# QA',
    bugScanner: '# Bug Scanner',
    featureRadar: '# Feature Radar',
  },
  emailNotifications: {
    enabled: false,
    destinationEmail: '',
    senderName: '',
    senderAddress: '',
    smtpHost: '',
    smtpPort: 587,
    smtpSecurity: 'starttls',
    smtpUsername: '',
    secretConfigured: false,
    appBaseUrl: '',
    eventPreferences: {
      taskCompleted: true,
      blockedNeedsAttention: true,
      qaResults: true,
      buildVerificationFailed: true,
      approvalRequired: true,
    },
    readyToEnable: false,
    lastTestResult: 'never',
  },
};

describe('ProjectsPage', () => {
  const gitProject: ImportedProject = {
    id: 'project-git',
    name: 'app',
    path: 'C:\\Workspaces\\app',
    source: 'git',
    remoteUrl: 'https://github.com/acme/app.git',
    branch: 'main',
    importedAt: '2026-08-14T00:00:00Z',
  };
  const localProject: ImportedProject = {
    id: 'project-local',
    name: 'local-app',
    path: 'C:\\Projects\\local-app',
    source: 'local',
    importedAt: '2026-08-14T00:00:00Z',
  };
  let projects: ImportedProject[];
  let removeProject: ReturnType<typeof vi.fn<(projectId: string) => unknown>>;

  const projectApi = {
    getSettings: () => of(settings),
    listProjects: () => of(projects),
    listGitCredentials: () =>
      of([
        {
          login: 'octocat',
          host: 'github.com',
          active: true,
          selected: true,
          source: 'GitHub CLI keyring',
        },
      ]),
    selectGitCredential: (login: string) =>
      of({
        login,
        host: 'github.com',
        active: true,
        selected: true,
        source: 'GitHub CLI keyring',
      }),
    listGitRepositories: () =>
      of([
        {
          id: 1,
          name: 'app',
          fullName: 'acme/app',
          cloneUrl: 'https://github.com/acme/app.git',
          defaultBranch: 'main',
          private: true,
          archived: false,
        },
      ]),
    selectFolder: () => of({ path: 'C:\\Projects\\local-app' }),
    removeProject: (projectId: string) => removeProject(projectId),
  };

  beforeEach(async () => {
    projects = [];
    removeProject = vi.fn((projectId: string) =>
      of({
        project: projects.find((project) => project.id === projectId) ?? gitProject,
        boardRemoved: true,
        insightsRemoved: 1,
      }),
    );
    await TestBed.configureTestingModule({
      imports: [ProjectsPage],
      providers: [provideRouter([]), { provide: ProjectApiService, useValue: projectApi }],
    }).compileComponents();
  });

  it('loads project settings and exposes both import modes', async () => {
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;

    await component.ngOnInit();

    expect(component.settings()?.clonePath).toBe('C:\\Workspaces');
    expect(component.gitRepositories().length).toBe(1);
    expect(component.selectedGitCredential()?.login).toBe('octocat');
    expect(component.importMode()).toBe('git');
    component.setImportMode('local');
    expect(component.importMode()).toBe('local');
  });

  it('uses the native folder selection result', async () => {
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;

    await component.pickLocalFolder();

    expect(component.localForm.controls.path.value).toBe('C:\\Projects\\local-app');
  });

  it('opens and cancels the project removal confirmation without calling the API', async () => {
    projects = [gitProject];
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    clickButton(fixture.nativeElement, 'Remove from ProductCrew');
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Remove app from ProductCrew?');
    expect(fixture.nativeElement.textContent).toContain('The repository folder will stay on disk');
    expect(fixture.nativeElement.textContent).toContain('Repository folder: C:\\Workspaces\\app');

    clickButton(fixture.nativeElement, 'Cancel');
    fixture.detectChanges();

    expect(removeProject).not.toHaveBeenCalled();
    expect(component.confirmingRemovalProjectId()).toBeNull();
    expect(component.projects().map((project) => project.id)).toEqual(['project-git']);
  });

  it('confirms removal, updates counts, preserves search, and shows success', async () => {
    projects = [gitProject, localProject];
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    component.search.set('app');

    await component.confirmProjectRemoval(gitProject);
    fixture.detectChanges();

    expect(removeProject).toHaveBeenCalledWith('project-git');
    expect(component.search()).toBe('app');
    expect(component.projects().map((project) => project.id)).toEqual(['project-local']);
    expect(component.gitProjectCount()).toBe(0);
    expect(component.localProjectCount()).toBe(1);
    expect(component.successTitle()).toBe('Project removed');
    expect(component.successMessage()).toContain('repository folder remains on disk');
    expect(fixture.nativeElement.textContent).toContain('1 connected');
    expect(fixture.nativeElement.textContent).not.toContain('C:\\Workspaces\\app');
  });

  it('keeps the row visible and shows the backend message for active-work rejection', async () => {
    projects = [gitProject];
    removeProject.mockReturnValue(
      throwError(
        () =>
          new HttpErrorResponse({
            status: 409,
            error: { error: 'Finish or stop active work before removing this project.' },
          }),
      ),
    );
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    component.openProjectRemoval(gitProject);

    await component.confirmProjectRemoval(gitProject);
    fixture.detectChanges();

    expect(component.projects().map((project) => project.id)).toEqual(['project-git']);
    expect(component.confirmingRemovalProjectId()).toBe('project-git');
    expect(component.errorTitle()).toBe('Project cannot be removed yet');
    expect(fixture.nativeElement.textContent).toContain(
      'Finish or stop active work before removing this project.',
    );
    expect(fixture.nativeElement.textContent).toContain('C:\\Workspaces\\app');
  });

  it('keeps the row visible and shows a safe generic removal failure', async () => {
    projects = [gitProject];
    removeProject.mockReturnValue(
      throwError(() => new HttpErrorResponse({ status: 500, error: {} })),
    );
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();

    await component.confirmProjectRemoval(gitProject);
    fixture.detectChanges();

    expect(component.projects().map((project) => project.id)).toEqual(['project-git']);
    expect(component.errorTitle()).toBe('Removal needs attention');
    expect(component.errorMessage()).toBe(
      'Could not remove the project. Nothing was deleted from disk.',
    );
  });

  it('prevents double-submit while removal is pending', async () => {
    projects = [gitProject];
    const removal = new Subject<ProjectRemovalResult>();
    removeProject.mockReturnValue(removal.asObservable());
    const fixture = TestBed.createComponent(ProjectsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();

    const pendingRemoval = component.confirmProjectRemoval(gitProject);
    await component.confirmProjectRemoval(gitProject);

    expect(removeProject).toHaveBeenCalledTimes(1);
    expect(component.isRemoving('project-git')).toBe(true);

    removal.next({ project: gitProject, boardRemoved: true, insightsRemoved: 1 });
    removal.complete();
    await pendingRemoval;

    expect(component.isRemoving('project-git')).toBe(false);
  });
});

function clickButton(container: HTMLElement, text: string): void {
  const button = Array.from(container.querySelectorAll('button')).find((item) =>
    item.textContent?.includes(text),
  );
  if (!button) throw new Error(`Could not find button containing "${text}"`);
  button.click();
}
