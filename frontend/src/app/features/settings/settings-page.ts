import { HttpErrorResponse } from '@angular/common/http';
import { DatePipe } from '@angular/common';
import { Component, computed, inject, OnDestroy, OnInit, signal } from '@angular/core';
import {
  AbstractControl,
  FormBuilder,
  ReactiveFormsModule,
  ValidationErrors,
  ValidatorFn,
  Validators,
} from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { firstValueFrom, Subscription } from 'rxjs';

import { ProjectApiService } from '../../core/project-api.service';
import { ProjectContextService } from '../../core/project-context.service';
import { AppUpdateService } from '../../core/app-update.service';
import { AgentModelOption, AgentModelOptionGroup, AgentRoleKey, AgentRuntimeRole, AgentSkill, AIProvider, BuildProfile, BuildRun, DeployProfile, EffectiveAgentProfile, ProjectAgentProfile, ProjectSettings, SystemTool, SystemToolCheckOutcome, ToolchainSnapshot, BackupPayload, BackupPreviewResponse, StorageDatasourceConfig, StorageDatasourceKind, StorageDatasourceMigrationResult, StorageDatasourceStatus } from '../../core/project.models';
import { AppShell } from '../../shared/app-shell/app-shell';

type AgentStudioProfileKey = 'standard' | AgentRoleKey;
type AgentStudioProfileView = Pick<ProjectSettings, 'agentProfileVersion' | 'workingStandard' | 'agentSkills' | 'roleDefinitions'> & Partial<Pick<ProjectAgentProfile, 'projectId' | 'projectName' | 'projectPath' | 'dataPath' | 'inherited' | 'updatedAt'>>;

const ABSOLUTE_FOLDER_PATH_PATTERNS = [
  /^[A-Za-z]:\\(?:[^<>:"/\\|?*\u0000-\u001F]+\\?)*$/,
  /^\\\\[^<>:"/\\|?*\u0000-\u001F]+\\[^<>:"/\\|?*\u0000-\u001F]+(?:\\[^<>:"/\\|?*\u0000-\u001F]+)*$/,
  /^\/(?:[^/\u0000]+\/?)*$/,
];

const absoluteFolderPathValidator: ValidatorFn = (
  control: AbstractControl<string>,
): ValidationErrors | null => {
  const value = control.value.trim();
  return value === '' || ABSOLUTE_FOLDER_PATH_PATTERNS.some((pattern) => pattern.test(value))
    ? null
    : { folderPath: true };
};

@Component({
  selector: 'app-settings-page',
  imports: [ReactiveFormsModule, DatePipe, RouterLink, AppShell],
  templateUrl: './settings-page.html',
})
export class SettingsPage implements OnInit, OnDestroy {
  private readonly formBuilder = inject(FormBuilder);
  private readonly projectApi = inject(ProjectApiService);
  private readonly projectContext = inject(ProjectContextService);
  private readonly title = inject(Title);
  readonly updates = inject(AppUpdateService);
  private buildRunSubscription?: Subscription;
  private feedbackMessageTimer?: ReturnType<typeof setTimeout>;

  readonly form = this.formBuilder.nonNullable.group({
    clonePath: ['', [Validators.required, absoluteFolderPathValidator]],
    updatePath: ['C:\\Tools\\updates', [Validators.required, absoluteFolderPathValidator]],
    gitProvider: ['github'],
    gitUsername: [''],
    aiProvider: ['codex'],
    aiFallbackProvider: ['claude-code'],
    aiFallbackEnabled: [false],
    codexModel: [''],
    codexEffort: ['high'],
    claudeCodeModel: ['sonnet'],
    claudeCodeEffort: ['high'],
    githubCopilotModel: ['auto'],
    githubCopilotEffort: [''],
    personalAccessToken: [''],
  });
  readonly emailForm = this.formBuilder.nonNullable.group({
    enabled: [false],
    destinationEmail: ['', [Validators.required, Validators.email]],
    senderName: [''],
    senderAddress: ['', [Validators.required, Validators.email]],
    smtpHost: ['', [Validators.required]],
    smtpPort: [587, [Validators.required, Validators.min(1), Validators.max(65535)]],
    smtpSecurity: ['starttls'],
    smtpUsername: [''],
    appBaseUrl: [''],
    password: [''],
    eventPreferences: this.formBuilder.nonNullable.group({
      taskCompleted: [true],
      blockedNeedsAttention: [true],
      qaResults: [true],
      buildVerificationFailed: [true],
      approvalRequired: [true],
    }),
  });
  readonly emailSaving = signal(false);
  readonly emailTesting = signal(false);
  readonly emailError = signal<string | null>(null);
  readonly showEmailPassword = signal(false);
  readonly emailSettings = computed(() => this.settings()?.emailNotifications ?? null);
  readonly canEnableEmail = computed(() => this.emailSettings()?.readyToEnable ?? false);
  readonly settings = signal<ProjectSettings | null>(null);
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly showToken = signal(false);
  readonly successMessage = signal<string | null>(null);
  readonly errorMessage = signal<string | null>(null);
  readonly exportLoading = signal<boolean>(false);
  readonly exportSuccess = signal<string | null>(null);
  readonly exportError = signal<string | null>(null);
  readonly selectedAgentProfile = signal<AgentStudioProfileKey>('standard');
  readonly selectedAgentStudioProjectId = signal('');
  readonly agentStudioProjectMenuOpen = signal(false);
  readonly agentStudioProfile = signal<ProjectAgentProfile | null>(null);
  readonly agentStudioProfileLoading = signal(false);
  readonly editingAgentProfile = signal<AgentStudioProfileKey | null>(null);
  readonly agentProfileDraft = signal('');
  readonly savingAgentProfile = signal(false);
  readonly selectedSkillID = signal('evidence-first-investigation');
  readonly editingSkillID = signal<string | null>(null);
  readonly skillDraft = signal<AgentSkill | null>(null);
  readonly savingSkill = signal(false);
  readonly effectiveProfile = signal<EffectiveAgentProfile | null>(null);
  readonly effectiveProfileLoading = signal(false);
  readonly toolchain = signal<ToolchainSnapshot | null>(null);
  readonly toolchainLoading = signal(true);
  readonly toolchainRefreshing = signal(false);
  readonly toolchainError = signal<string | null>(null);
  readonly toolSearch = signal('');
  readonly toolFilter = signal<'all' | 'attention' | 'ready'>('all');
  readonly agentModelOptions = signal<AgentModelOptionGroup[]>([]);
  readonly agentModelOptionsLoading = signal(true);
  readonly agentModelOptionsError = signal<string | null>(null);
  readonly selectedToolID = signal('gh');
  readonly runningToolAction = signal<string | null>(null);
  readonly expandedInstallation = signal(false);
  readonly projects = this.projectContext.projects;
  readonly selectedAgentStudioProject = computed(() =>
    this.projects().find((project) => project.id === this.selectedAgentStudioProjectId()) ?? null,
  );
  readonly selectedBuildProjectId = signal('');
  readonly buildProfile = signal<BuildProfile | null>(null);
  readonly buildProfileLoading = signal(true);
  readonly buildDetecting = signal(false);
  readonly activeBuildRun = signal<BuildRun | null>(null);
  readonly runningBuildAction = signal<string | null>(null);
  readonly selectedBuildAction = computed(() => {
    const profile = this.buildProfile();
    return profile?.actions.find((action) => action.id === profile.selectedActionId) ?? null;
  });

  readonly selectedDeployProjectId = signal('');
  readonly deployProfile = signal<DeployProfile | null>(null);
  readonly deployProfileLoading = signal(true);
  readonly deployDetecting = signal(false);
  readonly selectedDeployAction = computed(() => {
    const profile = this.deployProfile();
    return profile?.actions.find((action) => action.id === profile.selectedActionId) ?? null;
  });

  // Backup & Restore signals
  readonly backupPreview = signal<BackupPreviewResponse | null>(null);
  readonly showDiffPanel = signal<boolean>(false);
  readonly backupLoading = signal<boolean>(false);
  readonly backupError = signal<string | null>(null);
  readonly backupErrorVersion = signal<boolean>(false);
  readonly dragOverDropzone = signal<boolean>(false);
  readonly backupPayload = signal<BackupPayload | null>(null);

  // Storage datasource signals
  readonly datasourceStatus = signal<StorageDatasourceStatus | null>(null);
  readonly datasourceLoading = signal<boolean>(true);
  readonly datasourceChecking = signal<boolean>(false);
  readonly datasourceError = signal<string | null>(null);
  readonly changeFlowOpen = signal<boolean>(false);
  readonly changeStep = signal<1 | 2 | 3>(1);
  readonly targetKind = signal<StorageDatasourceKind>('local-json');
  readonly targetLocalDirectory = signal('');
  readonly targetSqlitePath = signal('');
  readonly targetCheckResult = signal<StorageDatasourceStatus | null>(null);
  readonly targetChecking = signal<boolean>(false);
  readonly targetCheckStale = signal<boolean>(false);
  readonly migrationAcknowledged = signal<boolean>(false);
  readonly migrating = signal<boolean>(false);
  readonly migrationResult = signal<StorageDatasourceMigrationResult | null>(null);
  readonly migrationError = signal<string | null>(null);
  readonly datasourceBadgeLabel = computed(() => {
    if (this.migrating()) return 'Migrating';
    if (this.datasourceStatus()?.status === 'unavailable') return 'Needs attention';
    return 'Ready';
  });
  readonly targetIsActiveDatasource = computed(() => {
    const status = this.datasourceStatus();
    if (!status) return false;
    if (status.kind !== this.targetKind()) return false;
    const activeDetail = status.kind === 'sqlite' ? status.details?.['path'] : status.details?.['directory'];
    const targetDetail = status.kind === 'sqlite' ? this.targetSqlitePath().trim() : this.targetLocalDirectory().trim();
    return (activeDetail ?? '').trim() === targetDetail;
  });

  readonly diffList = computed(() => {
    const preview = this.backupPreview();
    if (!preview) return [];

    const keysToCompare = [
      { key: 'clonePath', label: 'Default clone path' },
      { key: 'gitProvider', label: 'Git provider' },
      { key: 'gitUsername', label: 'Git username' },
      { key: 'aiProvider', label: 'Preferred AI platform' },
      { key: 'aiFallbackProvider', label: 'Fallback AI platform' },
      { key: 'aiFallbackEnabled', label: 'Enable fallback runtime' },
      { key: 'codexModel', label: 'Codex model' },
      { key: 'codexEffort', label: 'Codex effort' },
      { key: 'claudeCodeModel', label: 'Claude Code model' },
      { key: 'claudeCodeEffort', label: 'Claude Code effort' },
      { key: 'githubCopilotModel', label: 'GitHub Copilot model' },
      { key: 'githubCopilotEffort', label: 'GitHub Copilot effort' },
      { key: 'theme', label: 'Theme' },
      { key: 'updatePath', label: 'Update folder' }
    ];

    const list: Array<{
      key: string;
      label: string;
      status: 'added' | 'modified' | 'unchanged';
      oldVal: string;
      newVal: string;
    }> = [];

    const current = preview.current as any;
    const incoming = preview.incoming as any;

    for (const item of keysToCompare) {
      const curVal = current[item.key];
      const incVal = incoming[item.key];

      const oldValStr = curVal !== undefined && curVal !== null ? String(curVal) : '';
      const newValStr = incVal !== undefined && incVal !== null ? String(incVal) : '';

      if (curVal === undefined || curVal === null || curVal === '') {
        if (incVal !== undefined && incVal !== null && incVal !== '') {
          list.push({
            key: item.key,
            label: item.label,
            status: 'added',
            oldVal: 'None (Default)',
            newVal: newValStr
          });
        } else {
          list.push({
            key: item.key,
            label: item.label,
            status: 'unchanged',
            oldVal: '',
            newVal: 'None (Default)'
          });
        }
      } else if (oldValStr !== newValStr && incVal !== undefined && incVal !== null) {
        list.push({
          key: item.key,
          label: item.label,
          status: 'modified',
          oldVal: oldValStr,
          newVal: newValStr
        });
      } else {
        list.push({
          key: item.key,
          label: item.label,
          status: 'unchanged',
          oldVal: '',
          newVal: oldValStr
        });
      }
    }

    return list;
  });
  readonly filteredTools = computed(() => {
    const query = this.toolSearch().trim().toLowerCase();
    const filter = this.toolFilter();
    return (this.toolchain()?.tools ?? []).filter((tool) => {
      const matchesQuery = query === '' || `${tool.name} ${tool.description}`.toLowerCase().includes(query);
      const matchesFilter = filter === 'all' || (filter === 'ready' ? tool.status === 'ready' : tool.status !== 'ready');
      return matchesQuery && matchesFilter;
    });
  });
  readonly selectedTool = computed<SystemTool | null>(() => {
    const tools = this.toolchain()?.tools ?? [];
    return tools.find((tool) => tool.id === this.selectedToolID()) ?? tools.at(0) ?? null;
  });
  readonly selectedSkill = computed<AgentSkill | null>(() => {
    const skills = this.currentAgentStudioProfile()?.agentSkills ?? [];
    return skills.find((skill) => skill.id === this.selectedSkillID()) ?? skills.at(0) ?? null;
  });
  readonly roleOptions: ReadonlyArray<{ id: AgentRoleKey; label: string; description: string; icon: string }> = [
    { id: 'teamLead', label: 'Team Lead', description: 'Research & planning', icon: 'account_tree' },
    { id: 'designer', label: 'Designer', description: 'UI/UX specification', icon: 'palette' },
    { id: 'developer', label: 'Developer', description: 'Implementation', icon: 'terminal' },
    { id: 'qa', label: 'QA', description: 'Independent verification', icon: 'verified' },
    { id: 'bugScanner', label: 'Bug Scanner', description: 'Repository defect discovery', icon: 'bug_report' },
    { id: 'featureRadar', label: 'Feature Radar', description: 'Product opportunity discovery', icon: 'radar' },
  ];
  readonly aiProviderOptions: ReadonlyArray<{ id: AIProvider; label: string; description: string; icon: string; tone: string }> = [
    { id: 'codex', label: 'Codex', description: 'Primary local app-server runtime. Stable path for ProductCrew agents.', icon: 'smart_toy', tone: 'Recommended' },
    { id: 'claude-code', label: 'Claude Code', description: 'Optional local fallback using the current Claude Code CLI login.', icon: 'psychology_alt', tone: 'Fallback' },
    { id: 'github-copilot', label: 'GitHub Copilot', description: 'Local provider invoked through the authenticated GitHub CLI session.', icon: 'hub', tone: 'Local session' },
  ];

  async ngOnInit(): Promise<void> {
    this.title.setTitle('Settings · ProductCrew');
    void this.loadToolchain(false);
    void this.loadAgentModelOptions();
    void this.initializeBuildVerification();
    void this.initializeDeployment();
    void this.loadStorageDatasource();
    try {
      const settings = await firstValueFrom(this.projectApi.getSettings());
      this.applySettings(settings);
      await this.initializeAgentStudio();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load local settings.'));
    } finally {
      this.loading.set(false);
    }
  }

  async loadAgentModelOptions(): Promise<void> {
    this.agentModelOptionsLoading.set(true);
    this.agentModelOptionsError.set(null);
    try {
      const response = await firstValueFrom(this.projectApi.getAgentModelOptions());
      this.agentModelOptions.set(response.providers);
    } catch (error) {
      this.agentModelOptionsError.set(this.errorText(error, 'Could not load model options from local CLI tools.'));
    } finally {
      this.agentModelOptionsLoading.set(false);
    }
  }

  ngOnDestroy(): void {
    this.buildRunSubscription?.unsubscribe();
    this.clearFeedbackMessageTimer();
  }

  private clearFeedbackMessages(): void {
    this.clearFeedbackMessageTimer();
    this.successMessage.set(null);
    this.errorMessage.set(null);
  }

  private showSuccessMessage(message: string): void {
    this.successMessage.set(message);
    this.errorMessage.set(null);
    this.scheduleFeedbackMessageDismiss();
  }

  private showErrorMessage(message: string): void {
    this.errorMessage.set(message);
    this.successMessage.set(null);
    this.scheduleFeedbackMessageDismiss();
  }

  private scheduleFeedbackMessageDismiss(): void {
    this.clearFeedbackMessageTimer();
    this.feedbackMessageTimer = setTimeout(() => {
      this.successMessage.set(null);
      this.errorMessage.set(null);
      this.feedbackMessageTimer = undefined;
    }, 5_000);
  }

  private clearFeedbackMessageTimer(): void {
    if (!this.feedbackMessageTimer) return;
    clearTimeout(this.feedbackMessageTimer);
    this.feedbackMessageTimer = undefined;
  }

  private async initializeBuildVerification(): Promise<void> {
    try {
      await this.projectContext.initialize();
      this.selectedBuildProjectId.set(this.projectContext.selectedProjectId() || this.projects()[0]?.id || '');
      await this.loadBuildProfile();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load build verification settings.'));
    }
  }

  private async initializeDeployment(): Promise<void> {
    try {
      await this.projectContext.initialize();
      this.selectedDeployProjectId.set(this.projectContext.selectedProjectId() || this.projects()[0]?.id || '');
      await this.loadDeployProfile();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load production deployment settings.'));
    }
  }

  private async initializeAgentStudio(): Promise<void> {
    try {
      await this.projectContext.initialize();
      const projectId = this.projectContext.selectedProjectId() || this.projects()[0]?.id || '';
      this.selectedAgentStudioProjectId.set(projectId);
      await this.loadAgentStudioProfile();
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load project agent profile.'));
    }
  }

  async selectAgentStudioProject(event: Event): Promise<void> {
    if (this.editingAgentProfile() !== null || this.editingSkillID() !== null) return;
    this.agentStudioProjectMenuOpen.set(false);
    this.selectedAgentStudioProjectId.set((event.target as HTMLSelectElement).value);
    await this.loadAgentStudioProfile();
  }

  toggleAgentStudioProjectMenu(): void {
    if (this.agentStudioProfileLoading() || this.editingAgentProfile() !== null || this.editingSkillID() !== null) return;
    this.agentStudioProjectMenuOpen.update((open) => !open);
  }

  async chooseAgentStudioProject(projectId: string): Promise<void> {
    if (this.editingAgentProfile() !== null || this.editingSkillID() !== null) return;
    this.agentStudioProjectMenuOpen.set(false);
    if (projectId === this.selectedAgentStudioProjectId()) return;
    this.selectedAgentStudioProjectId.set(projectId);
    await this.loadAgentStudioProfile();
  }

  async loadAgentStudioProfile(): Promise<void> {
    const projectId = this.selectedAgentStudioProjectId();
    this.effectiveProfile.set(null);
    if (!projectId) {
      this.agentStudioProfile.set(null);
      return;
    }
    this.agentStudioProfileLoading.set(true);
    try {
      this.agentStudioProfile.set(await firstValueFrom(this.projectApi.getProjectAgentProfile(projectId)));
      const role = this.selectedRole();
      if (role !== null) await this.loadEffectiveProfile(role);
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not load project agent profile.'));
    } finally {
      this.agentStudioProfileLoading.set(false);
    }
  }

  async selectBuildProject(event: Event): Promise<void> {
    this.selectedBuildProjectId.set((event.target as HTMLSelectElement).value);
    await this.loadBuildProfile();
  }

  async loadBuildProfile(): Promise<void> {
    const projectId = this.currentBuildProjectId();
    this.buildRunSubscription?.unsubscribe();
    this.activeBuildRun.set(null);
    if (!projectId) { this.buildProfile.set(null); this.buildProfileLoading.set(false); return; }
    this.buildProfileLoading.set(true);
    try {
      const profile = await firstValueFrom(this.projectApi.getBuildProfile(projectId));
      this.buildProfile.set(profile);
      this.activeBuildRun.set(profile.lastRun ?? null);
      if (profile.lastRun && ['queued', 'running'].includes(profile.lastRun.status)) this.watchBuildRun(projectId, profile.lastRun);
    }
    catch (error) { this.errorMessage.set(this.errorText(error, 'Could not load detected build actions.')); }
    finally { this.buildProfileLoading.set(false); }
  }

  async detectBuildActions(): Promise<void> {
    const projectId = this.currentBuildProjectId(); if (!projectId || this.buildDetecting()) return;
    this.buildDetecting.set(true); this.clearFeedbackMessages();
    try { this.buildProfile.set(await firstValueFrom(this.projectApi.detectBuildProfile(projectId))); this.showSuccessMessage('Build actions detected and cached locally. Choose the Work Board action below.'); }
    catch (error) { this.showErrorMessage(this.errorText(error, 'Could not detect build actions.')); }
    finally { this.buildDetecting.set(false); }
  }

  async selectBuildAction(event: Event): Promise<void> {
    const projectId = this.currentBuildProjectId();
    if (!projectId) return;
    const actionId = (event.target as HTMLSelectElement).value;
    this.clearFeedbackMessages();
    try {
      this.buildProfile.set(await firstValueFrom(this.projectApi.selectBuildAction(projectId, actionId)));
      this.showSuccessMessage(actionId ? 'Work Board build action saved.' : 'Work Board build action cleared.');
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not save the build action.'));
    }
  }

  private currentBuildProjectId(): string {
    const projectId = this.selectedBuildProjectId() || this.projectContext.selectedProjectId() || this.projects()[0]?.id || '';
    if (projectId && projectId !== this.selectedBuildProjectId()) this.selectedBuildProjectId.set(projectId);
    return projectId;
  }

  private watchBuildRun(projectId: string, run: BuildRun): void {
      this.runningBuildAction.set(run.actionId);
      this.buildRunSubscription?.unsubscribe();
      this.buildRunSubscription = this.projectApi.watchBuildRun(projectId, run.id).subscribe({
        next: (update) => this.activeBuildRun.set(update),
        complete: () => { const finished = this.activeBuildRun(); this.runningBuildAction.set(null); if (finished?.status === 'passed') this.showSuccessMessage(finished.reason ?? 'Build verification passed.'); else this.showErrorMessage(finished?.reason ?? 'Build verification failed.'); void this.loadBuildProfile(); },
      });
  }

  buildStatusLabel(profile: BuildProfile | null): string {
    if (!profile || profile.status === 'not_detected') return 'Not detected';
    if (profile.status === 'not_configured') return 'Needs setup';
    return `${profile.actions.length} action${profile.actions.length === 1 ? '' : 's'} ready`;
  }

  async selectDeployProject(event: Event): Promise<void> {
    this.selectedDeployProjectId.set((event.target as HTMLSelectElement).value);
    await this.loadDeployProfile();
  }

  async loadDeployProfile(): Promise<void> {
    const projectId = this.currentDeployProjectId();
    if (!projectId) { this.deployProfile.set(null); this.deployProfileLoading.set(false); return; }
    this.deployProfileLoading.set(true);
    try {
      this.deployProfile.set(await firstValueFrom(this.projectApi.getDeployProfile(projectId)));
    }
    catch (error) { this.errorMessage.set(this.errorText(error, 'Could not load detected deploy actions.')); }
    finally { this.deployProfileLoading.set(false); }
  }

  async detectDeployActions(): Promise<void> {
    const projectId = this.currentDeployProjectId(); if (!projectId || this.deployDetecting()) return;
    this.deployDetecting.set(true); this.clearFeedbackMessages();
    try { this.deployProfile.set(await firstValueFrom(this.projectApi.detectDeployProfile(projectId))); this.showSuccessMessage('Deploy scripts detected and cached locally. Choose the production deploy action below.'); }
    catch (error) { this.showErrorMessage(this.errorText(error, 'Could not detect deploy actions.')); }
    finally { this.deployDetecting.set(false); }
  }

  async selectDeployAction(event: Event): Promise<void> {
    const projectId = this.currentDeployProjectId();
    if (!projectId) return;
    const actionId = (event.target as HTMLSelectElement).value;
    this.clearFeedbackMessages();
    try {
      this.deployProfile.set(await firstValueFrom(this.projectApi.selectDeployAction(projectId, actionId)));
      this.showSuccessMessage(actionId ? 'Production deploy action saved.' : 'Production deploy action cleared.');
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not save the deploy action.'));
    }
  }

  private currentDeployProjectId(): string {
    const projectId = this.selectedDeployProjectId() || this.projectContext.selectedProjectId() || this.projects()[0]?.id || '';
    if (projectId && projectId !== this.selectedDeployProjectId()) this.selectedDeployProjectId.set(projectId);
    return projectId;
  }

  deployStatusLabel(profile: DeployProfile | null): string {
    if (!profile || profile.status === 'not_detected') return 'Not detected';
    if (profile.status === 'not_configured') return 'Not configured';
    return `${profile.actions.length} action${profile.actions.length === 1 ? '' : 's'} ready`;
  }

  async loadToolchain(refresh: boolean): Promise<void> {
    if (refresh) this.toolchainRefreshing.set(true);
    else this.toolchainLoading.set(true);
    this.toolchainError.set(null);
    try {
      const snapshot = await firstValueFrom(this.projectApi.getSystemTools(refresh));
      this.toolchain.set(snapshot);
      if (!snapshot.tools.some((tool) => tool.id === this.selectedToolID())) {
        this.selectedToolID.set(snapshot.tools[0]?.id ?? '');
      }
    } catch (error) {
      this.toolchainError.set(this.errorText(error, 'Could not inspect local CLI tools.'));
    } finally {
      this.toolchainLoading.set(false);
      this.toolchainRefreshing.set(false);
    }
  }

  selectTool(tool: SystemTool): void {
    this.selectedToolID.set(tool.id);
    this.expandedInstallation.set(false);
  }

  updateToolSearch(event: Event): void {
    this.toolSearch.set((event.target as HTMLInputElement).value);
  }

  async runToolAction(tool: SystemTool): Promise<void> {
    if (!tool.action || !tool.actionAvailable || this.runningToolAction() !== null) return;
    this.runningToolAction.set(tool.id);
    this.toolchainError.set(null);
    this.clearFeedbackMessages();
    try {
      const updated = await firstValueFrom(this.projectApi.runSystemToolAction(tool.id, tool.action));
      this.replaceTool(updated);
      this.showSuccessMessage(`${tool.name} ${tool.action === 'install' ? 'installed' : 'updated'} successfully.`);
      await this.loadToolchain(true);
    } catch (error) {
      this.toolchainError.set(this.errorText(error, `Could not ${tool.action} ${tool.name}.`));
    } finally {
      this.runningToolAction.set(null);
    }
  }

  statusLabel(tool: SystemTool): string {
    return ({ ready: 'Ready', update_available: 'Update available', not_installed: 'Not installed', check_failed: 'Check failed' })[tool.status];
  }

  checkLabel(outcome: SystemToolCheckOutcome): string {
    return ({ passed: 'Passed', warning: 'Warning', failed: 'Failed', blocked: 'Blocked' })[outcome];
  }

  checkIcon(outcome: SystemToolCheckOutcome): string {
    return ({ passed: 'check_circle', warning: 'warning', failed: 'error', blocked: 'block' })[outcome];
  }

  formattedToolchainCheck(): string {
    const value = this.toolchain()?.checkedAt;
    return value ? new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : 'Not checked yet';
  }

  private replaceTool(updated: SystemTool): void {
    const snapshot = this.toolchain();
    if (!snapshot) return;
    this.toolchain.set({ ...snapshot, tools: snapshot.tools.map((tool) => tool.id === updated.id ? updated : tool) });
  }

  async save(): Promise<void> {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    await this.persist(false);
  }

  async clearPAT(): Promise<void> {
    await this.persist(true);
  }

  async saveEmailSettings(): Promise<void> {
    if (this.emailForm.invalid) {
      this.emailForm.markAllAsTouched();
      return;
    }
    await this.persistEmailSettings(false);
  }

  async clearEmailPassword(): Promise<void> {
    await this.persistEmailSettings(true);
  }

  async sendTestEmail(): Promise<void> {
    this.emailTesting.set(true);
    this.emailError.set(null);
    try {
      const result = await firstValueFrom(this.projectApi.sendTestEmail());
      const current = this.settings();
      if (current) this.applySettings({ ...current, emailNotifications: result.settings });
      if (!result.success) this.emailError.set(result.message || 'Test email failed to send.');
    } catch (error) {
      this.emailError.set(this.errorText(error, 'Could not send a test email.'));
    } finally {
      this.emailTesting.set(false);
    }
  }

  private async persistEmailSettings(clearPassword: boolean): Promise<void> {
    this.emailSaving.set(true);
    this.emailError.set(null);
    const value = this.emailForm.getRawValue();
    try {
      const settings = await firstValueFrom(
        this.projectApi.updateEmailNotificationSettings({
          enabled: value.enabled,
          destinationEmail: value.destinationEmail,
          senderName: value.senderName,
          senderAddress: value.senderAddress,
          smtpHost: value.smtpHost,
          smtpPort: Number(value.smtpPort),
          smtpSecurity: value.smtpSecurity,
          smtpUsername: value.smtpUsername,
          appBaseUrl: value.appBaseUrl,
          eventPreferences: value.eventPreferences,
          password: clearPassword ? '' : value.password,
          clearPassword,
        }),
      );
      this.applySettings(settings);
    } catch (error) {
      this.emailError.set(this.errorText(error, clearPassword ? 'Could not remove the SMTP credential.' : 'Could not save email notification settings.'));
    } finally {
      this.emailSaving.set(false);
    }
  }

  selectAgentProfile(profile: AgentStudioProfileKey): void {
    if (this.editingAgentProfile() !== null || this.editingSkillID() !== null) return;
    this.selectedAgentProfile.set(profile);
    if (profile !== 'standard') void this.loadEffectiveProfile(profile);
  }

  beginAgentProfileEdit(): void {
    const profile = this.selectedAgentProfile();
    this.agentProfileDraft.set(this.agentProfileContent(profile));
    this.editingAgentProfile.set(profile);
    this.clearFeedbackMessages();
  }

  cancelAgentProfileEdit(): void {
    this.editingAgentProfile.set(null);
    this.agentProfileDraft.set('');
  }

  updateAgentProfileDraft(event: Event): void {
    this.agentProfileDraft.set((event.target as HTMLTextAreaElement).value);
  }

  async saveAgentProfile(): Promise<void> {
    const profile = this.editingAgentProfile();
    const content = this.agentProfileDraft().trim();
    if (profile === null || content === '') return;
    this.savingAgentProfile.set(true);
    this.clearFeedbackMessages();
    try {
      const projectId = this.selectedAgentStudioProjectId();
      const updatedProfile = projectId
        ? profile === 'standard'
          ? await firstValueFrom(this.projectApi.updateProjectWorkingStandard(projectId, content))
          : await firstValueFrom(this.projectApi.updateProjectRoleDefinition(projectId, profile, { content }))
        : null;
      if (updatedProfile) this.agentStudioProfile.set(updatedProfile);
      else {
        const settings = profile === 'standard'
          ? await firstValueFrom(this.projectApi.updateWorkingStandard(content))
          : await firstValueFrom(this.projectApi.updateRoleDefinition(profile, { content }));
        this.applySettings(settings);
      }
      this.editingAgentProfile.set(null);
      this.agentProfileDraft.set('');
      this.showSuccessMessage(`${this.agentProfileLabel(profile)} saved to ${updatedProfile?.inherited === false ? '.productcrew' : 'settings.json'}.`);
      if (profile !== 'standard') await this.loadEffectiveProfile(profile);
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not save the agent profile.'));
    } finally {
      this.savingAgentProfile.set(false);
    }
  }

  agentProfileContent(profile: AgentStudioProfileKey = this.selectedAgentProfile()): string {
    const studio = this.currentAgentStudioProfile();
    if (profile === 'standard') return studio?.workingStandard ?? '';
    return studio?.roleDefinitions[profile] ?? '';
  }

  agentProfileLabel(profile: AgentStudioProfileKey = this.selectedAgentProfile()): string {
    if (profile === 'standard') return 'Working Standard';
    return this.roleOptions.find((option) => option.id === profile)?.label ?? profile;
  }

  selectedRole(): AgentRoleKey | null {
    const profile = this.selectedAgentProfile();
    return profile === 'standard' ? null : profile;
  }

  isSkillAssigned(skill: AgentSkill, role: AgentRoleKey | null = this.selectedRole()): boolean {
    return role !== null && skill.roles.includes(this.runtimeRole(role));
  }

  async toggleSkillAssignment(skill: AgentSkill): Promise<void> {
    const role = this.selectedRole();
    if (role === null || this.savingSkill()) return;
    const runtimeRole = this.runtimeRole(role);
    const roles = skill.roles.includes(runtimeRole)
      ? skill.roles.filter((candidate) => candidate !== runtimeRole)
      : [...skill.roles, runtimeRole];
    await this.persistSkill({ ...skill, roles }, `${skill.name} routing updated.`);
  }

  selectSkill(skill: AgentSkill): void {
    if (this.editingSkillID() !== null) return;
    this.selectedSkillID.set(skill.id);
  }

  beginSkillEdit(skill: AgentSkill): void {
    this.selectedSkillID.set(skill.id);
    this.skillDraft.set({ ...skill, roles: [...skill.roles] });
    this.editingSkillID.set(skill.id);
    this.clearFeedbackMessages();
  }

  updateSkillInstructions(event: Event): void {
    const draft = this.skillDraft();
    if (draft) this.skillDraft.set({ ...draft, instructions: (event.target as HTMLTextAreaElement).value });
  }

  cancelSkillEdit(): void {
    this.editingSkillID.set(null);
    this.skillDraft.set(null);
  }

  async saveSkill(): Promise<void> {
    const draft = this.skillDraft();
    if (!draft || draft.instructions.trim() === '') return;
    await this.persistSkill({ ...draft, instructions: draft.instructions.trim() }, `${draft.name} instructions saved.`);
    this.cancelSkillEdit();
  }

  private async persistSkill(skill: AgentSkill, success: string): Promise<void> {
    this.savingSkill.set(true);
    this.clearFeedbackMessages();
    try {
      const projectId = this.selectedAgentStudioProjectId();
      if (projectId) {
        this.agentStudioProfile.set(await firstValueFrom(this.projectApi.updateProjectAgentSkill(projectId, skill)));
      } else {
        const settings = await firstValueFrom(this.projectApi.updateAgentSkill(skill));
        this.applySettings(settings);
      }
      this.showSuccessMessage(success);
      const role = this.selectedRole();
      if (role !== null) await this.loadEffectiveProfile(role);
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not update the agent skill.'));
    } finally {
      this.savingSkill.set(false);
    }
  }

  async loadEffectiveProfile(role: AgentRoleKey | null = this.selectedRole()): Promise<void> {
    if (role === null) {
      this.effectiveProfile.set(null);
      return;
    }
    this.effectiveProfileLoading.set(true);
    try {
      const projectId = this.selectedAgentStudioProjectId();
      this.effectiveProfile.set(projectId
        ? await firstValueFrom(this.projectApi.getProjectEffectiveAgentProfile(projectId, role))
        : await firstValueFrom(this.projectApi.getEffectiveAgentProfile(role)));
    } catch (error) {
      this.errorMessage.set(this.errorText(error, 'Could not compose the effective agent profile.'));
    } finally {
      this.effectiveProfileLoading.set(false);
    }
  }

  private runtimeRole(role: AgentRoleKey): AgentRuntimeRole {
    if (role === 'teamLead') return 'team-lead';
    if (role === 'bugScanner') return 'bug-scanner';
    if (role === 'featureRadar') return 'feature-radar';
    return role;
  }

  currentAgentStudioProfile(): AgentStudioProfileView | null {
    return this.agentStudioProfile() ?? this.settings();
  }

  agentStudioProjectPath(): string {
    return this.agentStudioProfile()?.projectPath ?? this.projects().find((project) => project.id === this.selectedAgentStudioProjectId())?.path ?? '';
  }

  agentStudioStorageLabel(): string {
    const profile = this.agentStudioProfile();
    if (!profile) return 'settings.json';
    return profile.inherited ? 'Default seed · save to create project profile' : '.productcrew/agent-studio/profile.json';
  }

  agentStudioScopeLabel(): string {
    if (!this.selectedAgentStudioProjectId()) return 'No project';
    return this.agentStudioProfile()?.inherited ? 'Default seed' : 'Project scoped';
  }

  aiProviderLabel(provider: string | null | undefined): string {
    if (provider === 'claude-code') return 'Claude Code';
    if (provider === 'github-copilot') return 'GitHub Copilot';
    if (provider === 'codex') return 'Codex';
    return 'None';
  }

  runtimeTool(provider: string | null | undefined): SystemTool | null {
    const id = provider === 'claude-code' ? 'claude-code' : provider === 'github-copilot' ? 'github-copilot' : provider === 'codex' ? 'codex' : '';
    return this.toolchain()?.tools.find((tool) => tool.id === id) ?? null;
  }

  modelGroup(provider: AIProvider): AgentModelOptionGroup | null {
    return this.agentModelOptions().find((group) => group.provider === provider) ?? null;
  }

  modelOptions(provider: AIProvider): AgentModelOption[] {
    const group = this.modelGroup(provider);
    const options = group?.models ?? [];
    const current = this.currentModelValue(provider).trim();
    if (current && !options.some((option) => option.id === current)) {
      return [
        ...options,
        {
          id: current,
          label: `Configured: ${current}`,
          description: 'Saved in Settings, but not returned by the current CLI catalog.',
          defaultEffort: '',
          efforts: provider === 'github-copilot' && current === 'auto'
            ? []
            : group?.efforts?.length ? group.efforts : this.defaultEffortOptions(provider),
          source: 'settings',
        },
      ];
    }
    return options;
  }

  effortOptions(provider: AIProvider): string[] {
    const group = this.modelGroup(provider);
    if (!group) return this.defaultEffortOptions(provider);
    const model = this.currentModelValue(provider);
    const selected = this.modelOptions(provider).find((candidate) => candidate.id === model);
    return selected ? selected.efforts : group.efforts.length ? group.efforts : this.defaultEffortOptions(provider);
  }

  modelSupportsEffort(provider: AIProvider): boolean {
    return this.effortOptions(provider).length > 0;
  }

  selectAgentModel(provider: AIProvider, event: Event): void {
    const model = (event.target as HTMLSelectElement).value;
    if (provider === 'codex') this.form.controls.codexModel.setValue(model);
    else if (provider === 'claude-code') this.form.controls.claudeCodeModel.setValue(model);
    else this.form.controls.githubCopilotModel.setValue(model);
    const option = this.modelOptions(provider).find((candidate) => candidate.id === model);
    const effort = option?.defaultEffort;
    if (option && option.efforts.length === 0) {
      if (provider === 'codex') this.form.controls.codexEffort.setValue('');
      else if (provider === 'claude-code') this.form.controls.claudeCodeEffort.setValue('');
      else this.form.controls.githubCopilotEffort.setValue('');
    } else if (effort) {
      if (provider === 'codex') this.form.controls.codexEffort.setValue(effort);
      else if (provider === 'claude-code') this.form.controls.claudeCodeEffort.setValue(effort);
      else this.form.controls.githubCopilotEffort.setValue(effort);
    }
  }

  claudeCodeRuntimeLabel(): string {
    return `${this.readableClaudeModel(this.form.controls.claudeCodeModel.value)} · ${this.readableEffort(this.form.controls.claudeCodeEffort.value)}`;
  }

  codexRuntimeLabel(): string {
    return `${this.readableModel('codex', this.form.controls.codexModel.value)} · ${this.readableEffort(this.form.controls.codexEffort.value)}`;
  }

  githubCopilotRuntimeLabel(): string {
    if (!this.modelSupportsEffort('github-copilot')) {
      return `${this.readableModel('github-copilot', this.form.controls.githubCopilotModel.value)} · Automatic effort`;
    }
    return `${this.readableModel('github-copilot', this.form.controls.githubCopilotModel.value)} · ${this.readableEffort(this.form.controls.githubCopilotEffort.value)}`;
  }

  private readableClaudeModel(model: string): string {
    return this.readableModel('claude-code', model);
  }

  readableModel(provider: AIProvider, model: string): string {
    if (model === '') return 'CLI default';
    return this.modelOptions(provider).find((option) => option.id === model)?.label ?? model;
  }

  readableEffort(effort: string): string {
    if (effort.trim() === '') return 'Automatic effort';
    return effort.toLowerCase() === 'high' ? 'High effort' : `${effort} effort`;
  }

  selectedModelUnavailable(provider: AIProvider): boolean {
    const current = this.currentModelValue(provider).trim();
    if (!current) return false;
    const group = this.modelGroup(provider);
    return !!group && !group.models.some((option) => option.id === current);
  }

  private currentModelValue(provider: AIProvider): string {
    if (provider === 'codex') return this.form.controls.codexModel.value;
    if (provider === 'claude-code') return this.form.controls.claudeCodeModel.value;
    return this.form.controls.githubCopilotModel.value;
  }

  private defaultEffortOptions(provider: AIProvider): string[] {
    return provider === 'github-copilot'
      ? ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']
      : ['low', 'medium', 'high', 'xhigh', 'max'];
  }

  private async persist(clearPersonalAccessToken: boolean): Promise<void> {
    this.saving.set(true);
    this.clearFeedbackMessages();
    const value = this.form.getRawValue();
    try {
      const settings = await firstValueFrom(
        this.projectApi.updateSettings({
          clonePath: value.clonePath,
          gitProvider: value.gitProvider,
          gitUsername: value.gitUsername,
          updatePath: value.updatePath,
          aiProvider: value.aiProvider,
          aiFallbackProvider: value.aiFallbackProvider,
          aiFallbackEnabled: value.aiFallbackEnabled,
          codexModel: value.codexModel,
          codexEffort: value.codexEffort,
          claudeCodeModel: value.claudeCodeModel,
          claudeCodeEffort: value.claudeCodeEffort,
          githubCopilotModel: value.githubCopilotModel,
          githubCopilotEffort: value.githubCopilotModel === 'auto' ? '' : value.githubCopilotEffort,
          personalAccessToken: clearPersonalAccessToken ? '' : value.personalAccessToken,
          clearPersonalAccessToken,
        }),
      );
      this.applySettings(settings);
      this.showSuccessMessage(
        clearPersonalAccessToken ? 'Git credential removed.' : 'Settings saved locally.',
      );
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not save settings.'));
    } finally {
      this.saving.set(false);
    }
  }

  async exportSettingsBackup(): Promise<void> {
    this.clearFeedbackMessages();
    this.exportSuccess.set(null);
    this.exportError.set(null);
    this.exportLoading.set(true);
    try {
      const payload = await firstValueFrom(this.projectApi.exportBackup());
      const dataStr = JSON.stringify(payload, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = window.URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      const dateStr = new Date().toISOString().split('T')[0];
      link.download = `productcrew-backup-${dateStr}.json`;
      link.click();
      window.URL.revokeObjectURL(url);
      const msg = 'Settings backup downloaded successfully.';
      this.showSuccessMessage(msg);
      this.exportSuccess.set(msg);
    } catch (error) {
      const errMsg = this.errorText(error, 'Could not export settings.');
      this.showErrorMessage(errMsg);
      this.exportError.set(errMsg);
    } finally {
      this.exportLoading.set(false);
    }
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragOverDropzone.set(true);
  }

  onDragLeave(): void {
    this.dragOverDropzone.set(false);
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragOverDropzone.set(false);
    const files = event.dataTransfer?.files;
    if (files && files.length > 0) {
      this.handleBackupFile(files[0]);
    }
  }

  onFileSelected(event: Event): void {
    const files = (event.target as HTMLInputElement).files;
    if (files && files.length > 0) {
      this.handleBackupFile(files[0]);
    }
  }

  handleBackupFile(file: File): void {
    this.backupError.set(null);
    this.backupErrorVersion.set(false);
    this.backupLoading.set(true);
    this.clearFeedbackMessages();

    const reader = new FileReader();
    reader.onload = async (e) => {
      try {
        const text = e.target?.result as string;
        const payload = JSON.parse(text) as BackupPayload;

        if (!payload || typeof payload !== 'object' || !payload.settings) {
          throw new Error('Invalid backup format');
        }

        if (payload.version > 1) {
          this.backupError.set('Unsupported Backup Version: This backup was created with a newer version of ProductCrew (Schema v2). Please update your ProductCrew app to restore these settings.');
          this.backupErrorVersion.set(true);
          this.backupLoading.set(false);
          return;
        }

        this.backupPayload.set(payload);
        const preview = await firstValueFrom(this.projectApi.previewBackup(payload));
        this.backupPreview.set(preview);
        this.showDiffPanel.set(true);
      } catch (error) {
        console.error(error);
        this.backupError.set('Invalid Backup File: The selected file is not a valid ProductCrew backup JSON payload. Check the file content and try again.');
        this.backupErrorVersion.set(false);
      } finally {
        this.backupLoading.set(false);
      }
    };
    reader.onerror = () => {
      this.backupError.set('Invalid Backup File: The selected file is not a valid ProductCrew backup JSON payload. Check the file content and try again.');
      this.backupErrorVersion.set(false);
      this.backupLoading.set(false);
    };
    reader.readAsText(file);
  }

  cancelRestore(): void {
    this.backupPreview.set(null);
    this.showDiffPanel.set(false);
    this.backupError.set(null);
    this.backupErrorVersion.set(false);
    this.dragOverDropzone.set(false);
    this.backupPayload.set(null);
  }

  async applyRestore(): Promise<void> {
    const payload = this.backupPayload();
    if (!payload) return;

    this.backupLoading.set(true);
    this.clearFeedbackMessages();

    try {
      const settings = await firstValueFrom(this.projectApi.restoreBackup(payload));
      this.applySettings(settings);
      this.showSuccessMessage('Settings restored successfully from backup.');
      this.cancelRestore();
    } catch (error) {
      this.showErrorMessage(this.errorText(error, 'Could not restore settings backup.'));
    } finally {
      this.backupLoading.set(false);
    }
  }

  async loadStorageDatasource(): Promise<void> {
    this.datasourceLoading.set(true);
    this.datasourceError.set(null);
    try {
      this.datasourceStatus.set(await firstValueFrom(this.projectApi.getStorageDatasource()));
    } catch (error) {
      this.datasourceError.set(this.errorText(error, 'Could not load the active storage datasource.'));
    } finally {
      this.datasourceLoading.set(false);
    }
  }

  async checkActiveDatasource(): Promise<void> {
    if (this.datasourceChecking()) return;
    this.datasourceChecking.set(true);
    this.datasourceError.set(null);
    try {
      this.datasourceStatus.set(await firstValueFrom(this.projectApi.getStorageDatasource()));
    } catch (error) {
      this.datasourceError.set(this.errorText(error, 'Could not check the active storage datasource.'));
    } finally {
      this.datasourceChecking.set(false);
    }
  }

  openChangeDatasource(): void {
    const status = this.datasourceStatus();
    this.targetKind.set(status?.kind ?? 'local-json');
    this.targetLocalDirectory.set(status?.kind === 'local-json' ? status.details?.['directory'] ?? '' : '');
    this.targetSqlitePath.set(status?.kind === 'sqlite' ? status.details?.['path'] ?? '' : '');
    this.targetCheckResult.set(null);
    this.targetCheckStale.set(false);
    this.migrationAcknowledged.set(false);
    this.migrationResult.set(null);
    this.migrationError.set(null);
    this.changeStep.set(1);
    this.changeFlowOpen.set(true);
  }

  cancelChangeDatasource(): void {
    this.changeFlowOpen.set(false);
    this.changeStep.set(1);
    this.targetCheckResult.set(null);
    this.targetCheckStale.set(false);
    this.migrationAcknowledged.set(false);
  }

  selectTargetKind(kind: StorageDatasourceKind): void {
    if (this.migrating()) return;
    this.targetKind.set(kind);
    this.invalidateTargetCheck();
  }

  updateTargetLocalDirectory(event: Event): void {
    this.targetLocalDirectory.set((event.target as HTMLInputElement).value);
    this.invalidateTargetCheck();
  }

  updateTargetSqlitePath(event: Event): void {
    this.targetSqlitePath.set((event.target as HTMLInputElement).value);
    this.invalidateTargetCheck();
  }

  private invalidateTargetCheck(): void {
    if (this.targetCheckResult()) this.targetCheckStale.set(true);
    this.targetCheckResult.set(null);
  }

  targetConfig(): StorageDatasourceConfig {
    return {
      kind: this.targetKind(),
      localJson: { directory: this.targetLocalDirectory().trim() },
      sqlite: { path: this.targetSqlitePath().trim() },
    };
  }

  async checkTargetDatasource(): Promise<void> {
    if (this.targetChecking()) return;
    this.targetChecking.set(true);
    this.targetCheckStale.set(false);
    try {
      const result = await firstValueFrom(this.projectApi.checkStorageDatasource(this.targetConfig()));
      this.targetCheckResult.set(result);
      this.changeStep.set(2);
    } catch (error) {
      this.targetCheckResult.set({
        kind: this.targetKind(),
        summary: '',
        status: 'unavailable',
        message: this.errorText(error, 'Could not verify the target datasource.'),
        lastCheckedAt: new Date().toISOString(),
      });
      this.changeStep.set(2);
    } finally {
      this.targetChecking.set(false);
    }
  }

  backToConfigureStep(): void {
    this.changeStep.set(1);
  }

  continueToMigration(): void {
    if (this.targetCheckResult()?.status !== 'ok' || this.targetCheckStale()) return;
    this.migrationResult.set(null);
    this.migrationError.set(null);
    this.migrationAcknowledged.set(false);
    this.changeStep.set(3);
  }

  toggleMigrationAcknowledged(event: Event): void {
    this.migrationAcknowledged.set((event.target as HTMLInputElement).checked);
  }

  async confirmMigration(): Promise<void> {
    if (this.migrating() || !this.migrationAcknowledged() || this.targetCheckResult()?.status !== 'ok' || this.targetCheckStale()) return;
    this.migrating.set(true);
    this.migrationError.set(null);
    try {
      const result = await firstValueFrom(this.projectApi.switchStorageDatasource(this.targetConfig(), true));
      this.migrationResult.set(result);
      this.changeFlowOpen.set(false);
      await this.loadStorageDatasource();
      this.showSuccessMessage(`Datasource switched to ${this.datasourceLabel(result.targetKind)}. Restart ProductCrew to use it in this running session.`);
    } catch (error) {
      const response = error as HttpErrorResponse;
      const result = (response.error?.result ?? null) as StorageDatasourceMigrationResult | null;
      if (result) this.migrationResult.set(result);
      this.migrationError.set(this.errorText(error, 'Migration failed. The previous datasource remains active.'));
    } finally {
      this.migrating.set(false);
    }
  }

  datasourceLabel(kind: StorageDatasourceKind | undefined): string {
    return kind === 'sqlite' ? 'SQLite' : 'Local JSON';
  }

  formattedDatasourceCheckedAt(value: string | undefined): string {
    return value ? new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : 'Not checked yet';
  }

  private applySettings(settings: ProjectSettings): void {
    this.settings.set(settings);
    this.form.patchValue({
      clonePath: settings.clonePath,
      updatePath: settings.updatePath,
      gitProvider: settings.gitProvider,
      gitUsername: settings.gitUsername,
      aiProvider: settings.aiProvider,
      aiFallbackProvider: settings.aiFallbackProvider || 'claude-code',
      aiFallbackEnabled: settings.aiFallbackEnabled,
      codexModel: settings.codexModel || '',
      codexEffort: settings.codexEffort || 'high',
      claudeCodeModel: settings.claudeCodeModel || 'sonnet',
      claudeCodeEffort: settings.claudeCodeEffort || 'high',
      githubCopilotModel: settings.githubCopilotModel || 'auto',
      githubCopilotEffort: (settings.githubCopilotModel || 'auto') === 'auto' ? '' : settings.githubCopilotEffort || 'high',
      personalAccessToken: '',
    });
    const email = settings.emailNotifications;
    this.emailForm.patchValue({
      enabled: email?.enabled ?? false,
      destinationEmail: email?.destinationEmail ?? '',
      senderName: email?.senderName ?? '',
      senderAddress: email?.senderAddress ?? '',
      smtpHost: email?.smtpHost ?? '',
      smtpPort: email?.smtpPort || 587,
      smtpSecurity: email?.smtpSecurity || 'starttls',
      smtpUsername: email?.smtpUsername ?? '',
      appBaseUrl: email?.appBaseUrl ?? '',
      password: '',
      eventPreferences: {
        taskCompleted: email?.eventPreferences?.taskCompleted ?? true,
        blockedNeedsAttention: email?.eventPreferences?.blockedNeedsAttention ?? true,
        qaResults: email?.eventPreferences?.qaResults ?? true,
        buildVerificationFailed: email?.eventPreferences?.buildVerificationFailed ?? true,
        approvalRequired: email?.eventPreferences?.approvalRequired ?? true,
      },
    });
  }

  private errorText(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse;
    return response.error?.error ?? fallback;
  }
}
