import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { finalize, Observable, of, shareReplay, tap } from 'rxjs';

import { apiUrl } from './api-url';

import {
  FolderSelection,
  GitCredential,
  GitRepository,
  ImportedProject,
  ProductWorkspace,
  CreateProductWorkspaceRequest,
  ImportGitProjectRequest,
  ProjectRemovalResult,
  ProjectSettings,
  EmailTestResult,
  UpdateEmailNotificationSettingsRequest,
  AgentModelOptionsResponse,
  AgentRoleKey,
  AgentRuntimeRole,
  AgentSkill,
  EffectiveAgentProfile,
  ProjectAgentProfile,
  UpdateProjectSettingsRequest,
  UpdateAgentSkillRequest,
  UpdateRoleDefinitionRequest,
  SystemTool,
  ToolchainSnapshot,
  BuildProfile,
  BuildRun,
  DeployProfile,
  DeployRun,
  BackupPayload,
  BackupPreviewResponse,
  ProductBlueprint,
  ProductBlueprintRequest,
  ProductCreationProfile,
  ProductCreationResult,
  StorageDatasourceConfig,
  StorageDatasourceMigrationResult,
  StorageDatasourceStatus,
} from './project.models';

@Injectable({ providedIn: 'root' })
export class ProjectApiService {
  private readonly http = inject(HttpClient);
  private settingsCache: ProjectSettings | null = null;
  private settingsRequest: Observable<ProjectSettings> | null = null;
  private projectsCache: ImportedProject[] | null = null;
  private projectsRequest: Observable<ImportedProject[]> | null = null;

  listWorkspaces(): Observable<ProductWorkspace[]> {
    return this.http.get<ProductWorkspace[]>('/api/workspaces');
  }

  createWorkspace(request: CreateProductWorkspaceRequest): Observable<ProductWorkspace> {
    return this.http.post<ProductWorkspace>('/api/workspaces', request);
  }

  updateWorkspace(workspaceId: string, request: CreateProductWorkspaceRequest): Observable<ProductWorkspace> {
    return this.http.put<ProductWorkspace>(`/api/workspaces/${encodeURIComponent(workspaceId)}`, request);
  }
  private gitCredentialsCache: GitCredential[] | null = null;
  private gitCredentialsRequest: Observable<GitCredential[]> | null = null;
  private gitRepositoriesCache: GitRepository[] | null = null;
  private gitRepositoriesRequest: Observable<GitRepository[]> | null = null;
  private agentModelOptionsCache: AgentModelOptionsResponse | null = null;
  private agentModelOptionsRequest: Observable<AgentModelOptionsResponse> | null = null;
  private systemToolsCache: ToolchainSnapshot | null = null;
  private systemToolsRequest: Observable<ToolchainSnapshot> | null = null;

  getSettings(refresh = false): Observable<ProjectSettings> {
    if (!refresh && this.settingsCache) return of(this.settingsCache);
    if (!refresh && this.settingsRequest) return this.settingsRequest;
    this.settingsRequest = this.http.get<ProjectSettings>('/api/settings').pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
      finalize(() => {
        this.settingsRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.settingsRequest;
  }

  updateSettings(request: UpdateProjectSettingsRequest): Observable<ProjectSettings> {
    return this.http.put<ProjectSettings>('/api/settings', request).pipe(
      tap((settings) => {
        this.settingsCache = settings;
        this.gitRepositoriesCache = null;
      }),
    );
  }

  updateEmailNotificationSettings(request: UpdateEmailNotificationSettingsRequest): Observable<ProjectSettings> {
    return this.http.put<ProjectSettings>('/api/settings/email', request).pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
    );
  }

  sendTestEmail(): Observable<EmailTestResult> {
    return this.http.post<EmailTestResult>('/api/settings/email/test', {});
  }

  getAgentModelOptions(refresh = false): Observable<AgentModelOptionsResponse> {
    if (!refresh && this.agentModelOptionsCache) return of(this.agentModelOptionsCache);
    if (!refresh && this.agentModelOptionsRequest) return this.agentModelOptionsRequest;
    this.agentModelOptionsRequest = this.http.get<AgentModelOptionsResponse>('/api/settings/agent-model-options').pipe(
      tap((options) => {
        this.agentModelOptionsCache = options;
      }),
      finalize(() => {
        this.agentModelOptionsRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.agentModelOptionsRequest;
  }

  updateTheme(theme: 'light' | 'dark'): Observable<ProjectSettings> {
    return this.http.put<ProjectSettings>('/api/settings/theme', { theme }).pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
    );
  }

  updateRoleDefinition(
    role: AgentRoleKey,
    request: UpdateRoleDefinitionRequest,
  ): Observable<ProjectSettings> {
    const pathRole = this.runtimeRole(role);
    return this.http.put<ProjectSettings>(`/api/settings/roles/${pathRole}`, request).pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
    );
  }

  updateWorkingStandard(content: string): Observable<ProjectSettings> {
    return this.http.put<ProjectSettings>('/api/settings/working-standard', { content }).pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
    );
  }

  exportBackup(): Observable<BackupPayload> {
    return this.http.get<BackupPayload>('/api/settings/backup/export');
  }

  previewBackup(payload: BackupPayload): Observable<BackupPreviewResponse> {
    return this.http.post<BackupPreviewResponse>('/api/settings/backup/preview', payload);
  }

  restoreBackup(payload: BackupPayload): Observable<ProjectSettings> {
    return this.http.post<ProjectSettings>('/api/settings/backup/restore', payload).pipe(
      tap((settings) => {
        this.settingsCache = settings;
        this.gitRepositoriesCache = null;
      }),
    );
  }

  getStorageDatasource(): Observable<StorageDatasourceStatus> {
    return this.http.get<StorageDatasourceStatus>('/api/settings/storage-datasource');
  }

  checkStorageDatasource(config: StorageDatasourceConfig): Observable<StorageDatasourceStatus> {
    return this.http.post<StorageDatasourceStatus>('/api/settings/storage-datasource/check', config);
  }

  switchStorageDatasource(config: StorageDatasourceConfig, confirm: boolean): Observable<StorageDatasourceMigrationResult> {
    return this.http.post<StorageDatasourceMigrationResult>('/api/settings/storage-datasource/switch', { ...config, confirm });
  }

  updateAgentSkill(skill: AgentSkill | UpdateAgentSkillRequest & { id: string }): Observable<ProjectSettings> {
    const request: UpdateAgentSkillRequest = {
      name: skill.name,
      description: skill.description,
      instructions: skill.instructions,
      roles: skill.roles,
    };
    return this.http.put<ProjectSettings>(`/api/settings/agent-skills/${encodeURIComponent(skill.id)}`, request).pipe(
      tap((settings) => {
        this.settingsCache = settings;
      }),
    );
  }

  getEffectiveAgentProfile(role: AgentRoleKey): Observable<EffectiveAgentProfile> {
    const pathRole = this.runtimeRole(role);
    return this.http.get<EffectiveAgentProfile>(`/api/settings/agents/${pathRole}/effective`);
  }

  getProjectAgentProfile(projectId: string): Observable<ProjectAgentProfile> {
    return this.http.get<ProjectAgentProfile>(`/api/projects/${encodeURIComponent(projectId)}/agent-studio`);
  }

  updateProjectWorkingStandard(projectId: string, content: string): Observable<ProjectAgentProfile> {
    return this.http.put<ProjectAgentProfile>(`/api/projects/${encodeURIComponent(projectId)}/agent-studio/working-standard`, { content });
  }

  updateProjectRoleDefinition(
    projectId: string,
    role: AgentRoleKey,
    request: UpdateRoleDefinitionRequest,
  ): Observable<ProjectAgentProfile> {
    const pathRole = this.runtimeRole(role);
    return this.http.put<ProjectAgentProfile>(`/api/projects/${encodeURIComponent(projectId)}/agent-studio/roles/${pathRole}`, request);
  }

  updateProjectAgentSkill(projectId: string, skill: AgentSkill | UpdateAgentSkillRequest & { id: string }): Observable<ProjectAgentProfile> {
    const request: UpdateAgentSkillRequest = {
      name: skill.name,
      description: skill.description,
      instructions: skill.instructions,
      roles: skill.roles,
    };
    return this.http.put<ProjectAgentProfile>(`/api/projects/${encodeURIComponent(projectId)}/agent-studio/agent-skills/${encodeURIComponent(skill.id)}`, request);
  }

  getProjectEffectiveAgentProfile(projectId: string, role: AgentRoleKey): Observable<EffectiveAgentProfile> {
    const pathRole = this.runtimeRole(role);
    return this.http.get<EffectiveAgentProfile>(`/api/projects/${encodeURIComponent(projectId)}/agent-studio/agents/${pathRole}/effective`);
  }

  private runtimeRole(role: AgentRoleKey): AgentRuntimeRole {
    if (role === 'teamLead') return 'team-lead';
    if (role === 'bugScanner') return 'bug-scanner';
    if (role === 'featureRadar') return 'feature-radar';
    return role;
  }

  getSystemTools(refresh = false): Observable<ToolchainSnapshot> {
    if (!refresh && this.systemToolsCache) return of(this.systemToolsCache);
    if (!refresh && this.systemToolsRequest) return this.systemToolsRequest;
    this.systemToolsRequest = this.http.get<ToolchainSnapshot>('/api/system/tools', {
      params: refresh ? { refresh: 'true' } : {},
    }).pipe(
      tap((snapshot) => {
        this.systemToolsCache = snapshot;
      }),
      finalize(() => {
        this.systemToolsRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.systemToolsRequest;
  }

  runSystemToolAction(toolID: string, action: 'install' | 'update'): Observable<SystemTool> {
    return this.http.post<SystemTool>(`/api/system/tools/${toolID}/actions/${action}`, {}).pipe(
      tap(() => {
        this.systemToolsCache = null;
      }),
    );
  }

  getBuildProfile(projectId: string): Observable<BuildProfile> {
    return this.http.get<BuildProfile>(`/api/projects/${encodeURIComponent(projectId)}/build-verification`);
  }

  detectBuildProfile(projectId: string): Observable<BuildProfile> {
    return this.http.post<BuildProfile>(`/api/projects/${encodeURIComponent(projectId)}/build-verification/detect`, {});
  }

  selectBuildAction(projectId: string, actionId: string): Observable<BuildProfile> {
    return this.http.put<BuildProfile>(`/api/projects/${encodeURIComponent(projectId)}/build-verification/action`, { actionId });
  }

  startBuildRun(projectId: string, actionId: string): Observable<BuildRun> {
    return this.http.post<BuildRun>(`/api/projects/${encodeURIComponent(projectId)}/build-verification/runs`, { actionId });
  }

  watchBuildRun(projectId: string, runId: string): Observable<BuildRun> {
    return new Observable<BuildRun>((subscriber) => {
      const source = new EventSource(apiUrl(`/api/projects/${encodeURIComponent(projectId)}/build-verification/runs/${encodeURIComponent(runId)}/events`));
      source.addEventListener('run', (event) => {
        const run = JSON.parse((event as MessageEvent<string>).data) as BuildRun;
        subscriber.next(run);
        if (!['queued', 'running'].includes(run.status)) subscriber.complete();
      });
      source.onerror = () => { /* EventSource reconnects while the service owns the run. */ };
      return () => source.close();
    });
  }

  getDeployProfile(projectId: string): Observable<DeployProfile> {
    return this.http.get<DeployProfile>(`/api/projects/${encodeURIComponent(projectId)}/deploy-action`);
  }

  detectDeployProfile(projectId: string): Observable<DeployProfile> {
    return this.http.post<DeployProfile>(`/api/projects/${encodeURIComponent(projectId)}/deploy-action/detect`, {});
  }

  selectDeployAction(projectId: string, actionId: string): Observable<DeployProfile> {
    return this.http.put<DeployProfile>(`/api/projects/${encodeURIComponent(projectId)}/deploy-action/action`, { actionId });
  }

  startDeployRun(projectId: string, actionId: string): Observable<DeployRun> {
    return this.http.post<DeployRun>(`/api/projects/${encodeURIComponent(projectId)}/deploy-action/runs`, { actionId });
  }

  watchDeployRun(projectId: string, runId: string, onLog?: (level: string, message: string) => void): Observable<DeployRun> {
    return new Observable<DeployRun>((subscriber) => {
      const source = new EventSource(apiUrl(`/api/projects/${encodeURIComponent(projectId)}/deploy-action/runs/${encodeURIComponent(runId)}/events`));
      source.addEventListener('run', (event) => {
        const run = JSON.parse((event as MessageEvent<string>).data) as DeployRun;
        subscriber.next(run);
        if (!['queued', 'running'].includes(run.status)) subscriber.complete();
      });
      source.addEventListener('log', (event) => {
        const payload = JSON.parse((event as MessageEvent<string>).data) as { level: string; message: string };
        onLog?.(payload.level, payload.message);
      });
      source.onerror = () => { /* EventSource reconnects while the service owns the run. */ };
      return () => source.close();
    });
  }

  listProjects(refresh = false): Observable<ImportedProject[]> {
    if (!refresh && this.projectsCache) return of(this.projectsCache);
    if (!refresh && this.projectsRequest) return this.projectsRequest;
    this.projectsRequest = this.http.get<ImportedProject[]>('/api/projects').pipe(
      tap((projects) => {
        this.projectsCache = projects;
      }),
      finalize(() => {
        this.projectsRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.projectsRequest;
  }

  listProductCreationProfiles(): Observable<ProductCreationProfile[]> {
    return this.http.get<ProductCreationProfile[]>('/api/product-creation/profiles');
  }

  createProductBlueprint(request: ProductBlueprintRequest): Observable<ProductBlueprint> {
    return this.http.post<ProductBlueprint>('/api/product-creation/blueprints', request);
  }

  approveProductBlueprint(blueprintId: string): Observable<ProductCreationResult> {
    return this.http
      .post<ProductCreationResult>(
        `/api/product-creation/blueprints/${encodeURIComponent(blueprintId)}/approve`,
        {},
      )
      .pipe(
        tap((result) => {
          this.projectsCache = [result.project, ...(this.projectsCache ?? [])];
        }),
      );
  }

  listGitRepositories(refresh = false): Observable<GitRepository[]> {
    if (!refresh && this.gitRepositoriesCache) return of(this.gitRepositoriesCache);
    if (!refresh && this.gitRepositoriesRequest) return this.gitRepositoriesRequest;
    this.gitRepositoriesRequest = this.http.get<GitRepository[]>('/api/git/repositories').pipe(
      tap((repositories) => {
        this.gitRepositoriesCache = repositories;
      }),
      finalize(() => {
        this.gitRepositoriesRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.gitRepositoriesRequest;
  }

  listGitCredentials(refresh = false): Observable<GitCredential[]> {
    if (!refresh && this.gitCredentialsCache) return of(this.gitCredentialsCache);
    if (!refresh && this.gitCredentialsRequest) return this.gitCredentialsRequest;
    this.gitCredentialsRequest = this.http.get<GitCredential[]>('/api/git/credentials').pipe(
      tap((credentials) => {
        this.gitCredentialsCache = credentials;
      }),
      finalize(() => {
        this.gitCredentialsRequest = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.gitCredentialsRequest;
  }

  selectGitCredential(login: string): Observable<GitCredential> {
    return this.http.put<GitCredential>('/api/git/credentials/selected', { login }).pipe(
      tap((selected) => {
        this.gitCredentialsCache = (this.gitCredentialsCache ?? []).map((credential) => ({
          ...credential,
          active: credential.login === selected.login,
          selected: credential.login === selected.login,
        }));
        this.gitRepositoriesCache = null;
      }),
    );
  }

  importGit(request: ImportGitProjectRequest): Observable<ImportedProject> {
    return this.http.post<ImportedProject>('/api/projects/import/git', request).pipe(
      tap((project) => {
        this.projectsCache = [project, ...(this.projectsCache ?? [])];
      }),
    );
  }

  importLocal(path: string): Observable<ImportedProject> {
    return this.http.post<ImportedProject>('/api/projects/import/local', { path }).pipe(
      tap((project) => {
        this.projectsCache = [project, ...(this.projectsCache ?? [])];
      }),
    );
  }

  removeProject(projectId: string): Observable<ProjectRemovalResult> {
    return this.http.delete<ProjectRemovalResult>(`/api/projects/${projectId}`).pipe(
      tap(() => {
        this.projectsCache =
          this.projectsCache?.filter((project) => project.id !== projectId) ?? null;
      }),
    );
  }

  selectFolder(): Observable<FolderSelection | null> {
    return this.http.post<FolderSelection | null>('/api/system/select-folder', {});
  }
}
