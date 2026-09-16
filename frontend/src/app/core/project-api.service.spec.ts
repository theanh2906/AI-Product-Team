import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { AgentModelOptionsResponse, ImportedProject, ToolchainSnapshot } from './project.models';
import { ProjectApiService } from './project-api.service';

describe('ProjectApiService', () => {
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

  let service: ProjectApiService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [ProjectApiService, provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(ProjectApiService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
  });

  it('removes the deleted project from the cached project list', () => {
    let listedProjects: ImportedProject[] = [];
    service.listProjects().subscribe((projects) => {
      listedProjects = projects;
    });
    http.expectOne('/api/projects').flush([gitProject, localProject]);

    expect(listedProjects.map((project) => project.id)).toEqual(['project-git', 'project-local']);

    service.removeProject('project-git').subscribe();
    const request = http.expectOne('/api/projects/project-git');
    expect(request.request.method).toBe('DELETE');
    request.flush({ project: gitProject, boardRemoved: true, insightsRemoved: 2 });

    service.listProjects().subscribe((projects) => {
      listedProjects = projects;
    });
    http.expectNone('/api/projects');

    expect(listedProjects.map((project) => project.id)).toEqual(['project-local']);
  });

  it('caches agent model options after the first settings load', () => {
    const options: AgentModelOptionsResponse = {
      providers: [
        {
          provider: 'github-copilot',
          source: 'gh copilot -- help config',
          models: [{ id: 'auto', label: 'Auto', defaultEffort: 'high', efforts: ['high'], source: 'gh copilot -- --help' }],
          efforts: ['high'],
        },
      ],
    };
    let first: AgentModelOptionsResponse | null = null;
    let second: AgentModelOptionsResponse | null = null;

    service.getAgentModelOptions().subscribe((response) => {
      first = response;
    });
    http.expectOne('/api/settings/agent-model-options').flush(options);

    service.getAgentModelOptions().subscribe((response) => {
      second = response;
    });
    http.expectNone('/api/settings/agent-model-options');

    expect(first).toEqual(options);
    expect(second).toEqual(options);
  });

  it('shares one in-flight agent model options request across callers', () => {
    const options: AgentModelOptionsResponse = { providers: [] };
    let calls = 0;

    service.getAgentModelOptions().subscribe(() => calls++);
    service.getAgentModelOptions().subscribe(() => calls++);

    const request = http.expectOne('/api/settings/agent-model-options');
    request.flush(options);
    http.expectNone('/api/settings/agent-model-options');

    expect(calls).toBe(2);
  });

  it('can refresh agent model options when explicitly requested', () => {
    service.getAgentModelOptions().subscribe();
    http.expectOne('/api/settings/agent-model-options').flush({ providers: [] });

    service.getAgentModelOptions(true).subscribe();
    http.expectOne('/api/settings/agent-model-options').flush({ providers: [] });
  });

  it('caches system tool diagnostics until an explicit refresh is requested', () => {
    const snapshot: ToolchainSnapshot = {
      operational: true,
      actionsRecommended: 0,
      packageManagerReady: true,
      checkedAt: '2026-09-06T08:00:00Z',
      tools: [],
    };
    let first: ToolchainSnapshot | null = null;
    let second: ToolchainSnapshot | null = null;

    service.getSystemTools().subscribe((response) => {
      first = response;
    });
    http.expectOne('/api/system/tools').flush(snapshot);

    service.getSystemTools().subscribe((response) => {
      second = response;
    });
    http.expectNone('/api/system/tools');

    service.getSystemTools(true).subscribe();
    const refresh = http.expectOne((request) => request.url === '/api/system/tools' && request.params.get('refresh') === 'true');
    refresh.flush({ ...snapshot, actionsRecommended: 1 });

    expect(first).toEqual(snapshot);
    expect(second).toEqual(snapshot);
  });
});
