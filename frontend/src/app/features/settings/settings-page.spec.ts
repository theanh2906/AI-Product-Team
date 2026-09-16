import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError, Subject } from 'rxjs';
import { vi } from 'vitest';

import { ProjectApiService } from '../../core/project-api.service';
import { ProjectContextService } from '../../core/project-context.service';
import { BuildProfile, DeployProfile, EmailNotificationSettings, ProjectAgentProfile, ProjectSettings, StorageDatasourceConfig, StorageDatasourceMigrationResult, StorageDatasourceStatus, ToolchainSnapshot, UpdateEmailNotificationSettingsRequest } from '../../core/project.models';
import { SettingsPage } from './settings-page';

const settings: ProjectSettings = {
  clonePath: 'C:\\AI-Workspaces',
  updatePath: 'C:\\Tools\\updates',
  gitProvider: 'github',
  gitUsername: 'octocat',
  githubAccount: '',
  aiProvider: 'codex',
  aiFallbackProvider: 'claude-code',
  aiFallbackEnabled: false,
  codexModel: 'gpt-5.3-codex-spark',
  codexEffort: 'high',
  claudeCodeModel: 'sonnet',
  claudeCodeEffort: 'high',
  githubCopilotModel: 'auto',
  githubCopilotEffort: '',
  theme: 'light',
  patConfigured: true,
  credentialType: 'OS keyring',
  configPath: 'C:\\Users\\test\\.productcrew\\settings.json',
  boardDataPath: 'C:\\Users\\test\\.productcrew\\boards.json',
  insightDataPath: 'C:\\Users\\test\\.productcrew\\insights.json',
  eventDataPath: 'C:\\Users\\test\\.productcrew\\events.jsonl',
  logPath: 'C:\\Users\\test\\.productcrew\\logs\\application.jsonl',
  agentProfileVersion: 1,
  workingStandard: '# ProductCrew Working Standard\n\nInspect first.',
  agentSkills: [
    { id: 'evidence-first-investigation', name: 'Evidence-first investigation', description: 'Inspect first', instructions: 'Collect evidence.', roles: ['team-lead', 'designer', 'developer', 'qa'] },
    { id: 'minimal-safe-implementation', name: 'Minimal safe implementation', description: 'Change safely', instructions: 'Make the smallest change.', roles: ['developer'] },
  ],
  roleDefinitions: { teamLead: '# Team Lead', designer: '# Designer', developer: '# Developer', qa: '# QA', bugScanner: '# Bug Scanner', featureRadar: '# Feature Radar' },
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

const toolchain: ToolchainSnapshot = {
  operational: true,
  actionsRecommended: 1,
  packageManagerReady: true,
  checkedAt: '2026-08-12T08:00:00Z',
  tools: [{
    id: 'gh',
    name: 'GitHub CLI',
    description: 'GitHub operations',
    icon: 'hub',
    required: true,
    status: 'update_available',
    installed: true,
    installedVersion: '2.89.0',
    latestVersion: '2.97.0',
    executablePath: 'C:\\Program Files\\GitHub CLI\\gh.exe',
    account: 'octocat',
    action: 'update',
    actionAvailable: true,
    checks: [],
  }],
};

describe('SettingsPage', () => {
  let projectAgentProfile: ProjectAgentProfile;
  let datasourceStatus: StorageDatasourceStatus;
  let emailNotificationSettings: EmailNotificationSettings;
  let emailReadyToEnable: boolean;
  const buildProfile: BuildProfile = {
    projectId: 'project-1', projectName: 'local-app', status: 'ready', detector: 'build-detector-v1',
    summary: 'Run npm check.', configFiles: ['package.json'], detectedAt: '2026-08-18T08:00:00Z',
    selectedActionId: 'check',
    actions: [{ id: 'check', label: 'npm run check', description: 'Full verification', executable: 'npm', arguments: ['run', 'check'], workingDir: '', source: 'package.json', recommended: true, confidence: 98 }],
  };
  const deployProfile: DeployProfile = {
    projectId: 'project-1', projectName: 'local-app', status: 'not_configured', detector: 'deploy-detector-v1',
    summary: 'Detected 1 candidate deploy script.', configFiles: ['package.json'], detectedAt: '2026-08-18T08:00:00Z',
    selectedActionId: '',
    actions: [{ id: 'deploy-prod', label: 'npm run deploy:prod', description: 'Deploy to production', executable: 'npm', arguments: ['run', 'deploy:prod'], workingDir: '', source: 'package.json', recommended: true, confidence: 90 }],
  };
  const projectApi = {
    getSettings: () => of({ ...settings, emailNotifications: emailNotificationSettings }),
    getAgentModelOptions: () => of({
      providers: [
        {
          provider: 'codex',
          source: 'codex debug models',
          models: [{ id: 'gpt-5.3-codex-spark', label: 'GPT-5.3-Codex-Spark', defaultEffort: 'high', efforts: ['low', 'medium', 'high'], source: 'codex debug models' }],
          efforts: ['low', 'medium', 'high'],
        },
        {
          provider: 'claude-code',
          source: 'claude --help',
          models: [{ id: 'sonnet', label: 'Sonnet', defaultEffort: 'high', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], source: 'claude --help' }],
          efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
        },
        {
          provider: 'github-copilot',
          source: 'gh copilot -- --help',
          models: [
            { id: 'auto', label: 'Auto', defaultEffort: '', efforts: [], source: 'gh copilot -- --help' },
            { id: 'gpt-5.6-sol', label: 'gpt-5.6-sol', defaultEffort: 'high', efforts: ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'], source: 'gh copilot -- help config' },
          ],
          efforts: ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'],
        },
      ],
    }),
    getSystemTools: () => of(toolchain),
    listProjects: () => of([
      { id: 'project-1', name: 'local-app', path: 'C:\\Projects\\local-app', source: 'local', importedAt: '2026-08-18T00:00:00Z' },
      { id: 'project-2', name: 'other-app', path: 'C:\\Projects\\other-app', source: 'local', importedAt: '2026-08-18T00:00:00Z' },
    ]),
    getBuildProfile: (projectId: string) => of({ ...buildProfile, projectId, projectName: projectId === 'project-2' ? 'other-app' : 'local-app' }),
    detectBuildProfile: () => of(buildProfile),
    selectBuildAction: (_projectId: string, actionId: string) => of({ ...buildProfile, selectedActionId: actionId }),
    getDeployProfile: (projectId: string) => of({ ...deployProfile, projectId, projectName: projectId === 'project-2' ? 'other-app' : 'local-app' }),
    detectDeployProfile: () => of(deployProfile),
    selectDeployAction: (_projectId: string, actionId: string) => of({ ...deployProfile, status: actionId ? 'ready' : 'not_configured', selectedActionId: actionId }),
    updateRoleDefinition: (role: keyof ProjectSettings['roleDefinitions'], request: { content: string }) =>
      of({ ...settings, agentProfileVersion: settings.agentProfileVersion + 1, roleDefinitions: { ...settings.roleDefinitions, [role]: request.content } }),
    updateWorkingStandard: (content: string) => of({ ...settings, agentProfileVersion: settings.agentProfileVersion + 1, workingStandard: content }),
    updateAgentSkill: (skill: ProjectSettings['agentSkills'][number]) => of({
      ...settings,
      agentProfileVersion: settings.agentProfileVersion + 1,
      agentSkills: settings.agentSkills.map((candidate) => candidate.id === skill.id ? skill : candidate),
    }),
    getEffectiveAgentProfile: (role: keyof ProjectSettings['roleDefinitions']) => of({
      role: role === 'teamLead' ? 'team-lead' : role,
      version: settings.agentProfileVersion,
      skills: role === 'developer' ? ['evidence-first-investigation', 'minimal-safe-implementation'] : ['evidence-first-investigation'],
      content: `${settings.workingStandard}\n${settings.roleDefinitions[role]}`,
    }),
    getProjectAgentProfile: (projectId: string) => of({ ...projectAgentProfile, projectId }),
    updateProjectWorkingStandard: (projectId: string, content: string) => {
      projectAgentProfile = { ...projectAgentProfile, projectId, inherited: false, agentProfileVersion: projectAgentProfile.agentProfileVersion + 1, workingStandard: content };
      return of(projectAgentProfile);
    },
    updateProjectRoleDefinition: (projectId: string, role: keyof ProjectSettings['roleDefinitions'], request: { content: string }) => {
      projectAgentProfile = {
        ...projectAgentProfile,
        projectId,
        inherited: false,
        agentProfileVersion: projectAgentProfile.agentProfileVersion + 1,
        roleDefinitions: { ...projectAgentProfile.roleDefinitions, [role]: request.content },
      };
      return of(projectAgentProfile);
    },
    updateProjectAgentSkill: (projectId: string, skill: ProjectSettings['agentSkills'][number]) => {
      projectAgentProfile = {
        ...projectAgentProfile,
        projectId,
        inherited: false,
        agentProfileVersion: projectAgentProfile.agentProfileVersion + 1,
        agentSkills: projectAgentProfile.agentSkills.map((candidate) => candidate.id === skill.id ? skill : candidate),
      };
      return of(projectAgentProfile);
    },
    getProjectEffectiveAgentProfile: (_projectId: string, role: keyof ProjectSettings['roleDefinitions']) => of({
      role: role === 'teamLead' ? 'team-lead' : role,
      version: projectAgentProfile.agentProfileVersion,
      skills: role === 'developer' ? ['evidence-first-investigation', 'minimal-safe-implementation'] : ['evidence-first-investigation'],
      content: `${projectAgentProfile.workingStandard}\n${projectAgentProfile.roleDefinitions[role]}`,
    }),
    exportBackup: () => of({
      version: 1,
      createdAt: '2026-08-26T19:49:02Z',
      settings: {
        clonePath: 'C:\\AI-Workspaces-Restore',
        updatePath: 'C:\\Tools\\updates',
        gitProvider: 'github',
        gitUsername: 'octocat',
        aiProvider: 'claude-code',
        aiFallbackProvider: 'claude-code',
        aiFallbackEnabled: false,
        codexModel: 'gpt-5.3-codex-spark',
        codexEffort: 'high',
        claudeCodeModel: 'sonnet',
        claudeCodeEffort: 'high',
        githubCopilotModel: 'auto',
        githubCopilotEffort: '',
        theme: 'light',
        roleDefinitions: { teamLead: '# Team Lead', designer: '# Designer', developer: '# Developer', qa: '# QA', bugScanner: '# Bug Scanner', featureRadar: '# Feature Radar' }
      }
    }),
    previewBackup: (payload: any) => of({
      current: settings,
      incoming: payload.settings
    }),
    restoreBackup: (payload: any) => of({
      ...settings,
      clonePath: payload.settings.clonePath,
      aiProvider: payload.settings.aiProvider
    }),
    getStorageDatasource: () => of(datasourceStatus),
    checkStorageDatasource: vi.fn((config: StorageDatasourceConfig) => of({
      kind: config.kind,
      summary: config.kind === 'sqlite' ? `SQLite database at ${config.sqlite.path}` : `Local JSON files in ${config.localJson.directory}`,
      status: 'ok' as const,
      lastCheckedAt: '2026-08-27T10:00:00Z',
      details: config.kind === 'sqlite' ? { path: config.sqlite.path } : { directory: config.localJson.directory },
    })),
    switchStorageDatasource: vi.fn((config: StorageDatasourceConfig) => of({
      sourceKind: 'local-json',
      targetKind: config.kind,
      applied: true,
      status: 'applied' as const,
      counts: { boards: 3, insights: 2, events: 5, notifications: 1, buildProfiles: 1 },
      durationMs: 42,
      activeDatasource: config,
    } satisfies StorageDatasourceMigrationResult)),
    updateEmailNotificationSettings: vi.fn((request: UpdateEmailNotificationSettingsRequest) => {
      if (request.enabled && !emailReadyToEnable) {
        return throwError(() => ({ error: { error: 'send a passing test email after the most recent configuration change before enabling notifications' } }));
      }
      emailNotificationSettings = {
        ...emailNotificationSettings,
        enabled: request.enabled,
        destinationEmail: request.destinationEmail,
        senderName: request.senderName,
        senderAddress: request.senderAddress,
        smtpHost: request.smtpHost,
        smtpPort: request.smtpPort,
        smtpSecurity: request.smtpSecurity,
        smtpUsername: request.smtpUsername,
        appBaseUrl: request.appBaseUrl,
        eventPreferences: request.eventPreferences,
        secretConfigured: request.clearPassword ? false : (request.password ? true : emailNotificationSettings.secretConfigured),
      };
      return of({ ...settings, emailNotifications: emailNotificationSettings });
    }),
    sendTestEmail: vi.fn(() => {
      emailReadyToEnable = true;
      emailNotificationSettings = { ...emailNotificationSettings, lastTestResult: 'passed', lastTestAt: '2026-09-13T10:00:00Z', readyToEnable: true };
      return of({ success: true, message: 'Test email sent successfully', settings: emailNotificationSettings });
    }),
  };

  beforeEach(async () => {
    localStorage.clear();
    vi.clearAllMocks();
    datasourceStatus = {
      kind: 'local-json',
      summary: 'Local JSON files in C:\\Users\\test\\.productcrew\\data',
      status: 'ok',
      lastCheckedAt: '2026-08-27T09:00:00Z',
      details: { directory: 'C:\\Users\\test\\.productcrew\\data' },
    };
    emailReadyToEnable = false;
    emailNotificationSettings = { ...settings.emailNotifications };
    projectAgentProfile = {
      projectId: 'project-1',
      projectName: 'local-app',
      projectPath: 'C:\\Projects\\local-app',
      dataPath: 'C:\\Projects\\local-app\\.productcrew\\agent-studio\\profile.json',
      inherited: true,
      agentProfileVersion: settings.agentProfileVersion,
      workingStandard: settings.workingStandard,
      agentSkills: settings.agentSkills.map((skill) => ({ ...skill, roles: [...skill.roles] })),
      roleDefinitions: { ...settings.roleDefinitions },
    };
    await TestBed.configureTestingModule({
      imports: [SettingsPage],
      providers: [
        provideRouter([]),
        { provide: ProjectApiService, useValue: projectApi },
      ],
    }).compileComponents();
  });

  it('loads non-secret settings without exposing the PAT', async () => {
    const fixture = TestBed.createComponent(SettingsPage);
    const component = fixture.componentInstance;

    await component.ngOnInit();

    expect(component.form.controls.clonePath.value).toBe('C:\\AI-Workspaces');
    expect(component.form.controls.personalAccessToken.value).toBe('');
    expect(component.settings()?.patConfigured).toBe(true);
  });

  it('loads agent model options from the backend CLI catalog', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;

    await component.ngOnInit();

    expect(component.modelOptions('codex').map((model) => model.id)).toEqual(['gpt-5.3-codex-spark']);
    expect(component.modelOptions('claude-code').map((model) => model.id)).toEqual(['sonnet']);
    expect(component.modelOptions('github-copilot').map((model) => model.id)).toEqual(['auto', 'gpt-5.6-sol']);
    expect(component.effortOptions('claude-code')).toContain('xhigh');
    expect(component.effortOptions('github-copilot')).toEqual([]);
    expect(component.githubCopilotRuntimeLabel()).toBe('Auto · Automatic effort');
  });

  it('shows the persisted current models after CLI options load', async () => {
    const fixture = TestBed.createComponent(SettingsPage);
    await fixture.componentInstance.ngOnInit();
    fixture.detectChanges();

    const codexSelect = fixture.nativeElement.querySelector(
      'select[formControlName="codexModel"]',
    ) as HTMLSelectElement;
    const claudeSelect = fixture.nativeElement.querySelector(
      'select[formControlName="claudeCodeModel"]',
    ) as HTMLSelectElement;
    const copilotSelect = fixture.nativeElement.querySelector(
      'select[formControlName="githubCopilotModel"]',
    ) as HTMLSelectElement;

    expect(codexSelect.value).toBe('gpt-5.3-codex-spark');
    expect(claudeSelect.value).toBe('sonnet');
    expect(copilotSelect.value).toBe('auto');
  });

  it('keeps a stale configured model visible when the CLI catalog cannot return it', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.form.controls.githubCopilotModel.setValue('claude-opus-5');
    component.agentModelOptions.set([
      {
        provider: 'github-copilot',
        source: 'gh copilot -- help config',
        error: 'GitHub Copilot CLI timed out.',
        models: [{ id: 'auto', label: 'Auto', defaultEffort: '', efforts: [], source: 'gh copilot -- --help' }],
        efforts: ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'],
      },
    ]);

    expect(component.modelOptions('github-copilot').map((model) => model.id)).toEqual(['auto', 'claude-opus-5']);
    expect(component.selectedModelUnavailable('github-copilot')).toBe(true);
    expect(component.effortOptions('github-copilot')).toContain('high');
  });

  it('validates the clone path as an absolute folder path', () => {
    const fixture = TestBed.createComponent(SettingsPage);
    const component = fixture.componentInstance;

    component.form.controls.clonePath.setValue('relative\\folder');
    expect(component.form.controls.clonePath.hasError('folderPath')).toBe(true);

    component.form.controls.clonePath.setValue('D:\\Projects');
    expect(component.form.controls.clonePath.valid).toBe(true);
  });

  it('loads cached build actions for the selected build settings project', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    await component.loadBuildProfile();

    expect(component.buildProfile()?.actions[0].recommended).toBe(true);
    expect(component.buildStatusLabel(component.buildProfile())).toBe('1 action ready');
  });

  it('keeps the Build Verification project independent from the global active project', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    const projectContext = TestBed.inject(ProjectContextService);
    await component.ngOnInit();
    await projectContext.initialize();

    expect(projectContext.selectedProjectId()).toBe('project-1');

    await component.selectBuildProject({ target: { value: 'project-2' } } as unknown as Event);

    expect(component.selectedBuildProjectId()).toBe('project-2');
    expect(component.buildProfile()?.projectId).toBe('project-2');
    expect(projectContext.selectedProjectId()).toBe('project-1');
  });

  it('renders the Agent Studio project source as a badge instead of inline separator text', async () => {
    const fixture = TestBed.createComponent(SettingsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();
    fixture.detectChanges();

    const trigger = fixture.nativeElement.querySelector('.agent-project-trigger') as HTMLButtonElement;
    const badge = trigger.querySelector('.project-source-badge') as HTMLElement;

    expect(trigger.textContent).toContain('local-app');
    expect(badge.textContent?.trim()).toBe('local');
    expect(trigger.textContent).not.toContain('local-app · local');

    component.toggleAgentStudioProjectMenu();
    fixture.detectChanges();

    const options = Array.from(fixture.nativeElement.querySelectorAll('.agent-project-option')) as HTMLElement[];
    expect(options.length).toBe(2);
    expect(options[0].querySelector('.project-source-badge')?.textContent?.trim()).toBe('local');
  });

  it('saves the selected Work Board build action', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    await component.selectBuildAction({ target: { value: 'check' } } as unknown as Event);

    expect(component.buildProfile()?.selectedActionId).toBe('check');
    expect(component.selectedBuildAction()?.label).toBe('npm run check');
  });

  it('loads cached deploy candidates for the selected deploy settings project', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    await component.loadDeployProfile();

    expect(component.deployProfile()?.actions[0].recommended).toBe(true);
    expect(component.deployStatusLabel(component.deployProfile())).toBe('Not configured');
  });

  it('keeps the Production Deployment project independent from the global active project', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    const projectContext = TestBed.inject(ProjectContextService);
    await component.ngOnInit();
    await projectContext.initialize();

    expect(projectContext.selectedProjectId()).toBe('project-1');

    await component.selectDeployProject({ target: { value: 'project-2' } } as unknown as Event);

    expect(component.selectedDeployProjectId()).toBe('project-2');
    expect(component.deployProfile()?.projectId).toBe('project-2');
    expect(projectContext.selectedProjectId()).toBe('project-1');
  });

  it('detects deploy scripts and reports them as an unconfigured, not-yet-saved candidate list', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    await component.detectDeployActions();

    expect(component.deployProfile()?.status).toBe('not_configured');
    expect(component.deployProfile()?.selectedActionId).toBe('');
    expect(component.selectedDeployAction()).toBeNull();
    expect(component.successMessage()).toBe('Deploy scripts detected and cached locally. Choose the production deploy action below.');
  });

  it('saves the explicitly selected production deploy action', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    await component.selectDeployAction({ target: { value: 'deploy-prod' } } as unknown as Event);

    expect(component.deployProfile()?.selectedActionId).toBe('deploy-prod');
    expect(component.deployProfile()?.status).toBe('ready');
    expect(component.selectedDeployAction()?.label).toBe('npm run deploy:prod');
    expect(component.successMessage()).toBe('Production deploy action saved.');
  });

  it('clears the production deploy action when reset to "No action configured"', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    await component.selectDeployAction({ target: { value: 'deploy-prod' } } as unknown as Event);

    await component.selectDeployAction({ target: { value: '' } } as unknown as Event);

    expect(component.deployProfile()?.selectedActionId).toBe('');
    expect(component.deployProfile()?.status).toBe('not_configured');
    expect(component.selectedDeployAction()).toBeNull();
    expect(component.successMessage()).toBe('Production deploy action cleared.');
  });

  it('auto-dismisses transient Settings feedback after 5 seconds', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    vi.useFakeTimers();

    await component.selectBuildAction({ target: { value: 'check' } } as unknown as Event);

    expect(component.successMessage()).toBe('Work Board build action saved.');

    vi.advanceTimersByTime(4_999);
    expect(component.successMessage()).toBe('Work Board build action saved.');

    vi.advanceTimersByTime(1);
    expect(component.successMessage()).toBeNull();
    expect(component.errorMessage()).toBeNull();

    vi.useRealTimers();
  });

  it('keeps agent profiles read-only until Edit and saves Markdown content', async () => {
    const fixture = TestBed.createComponent(SettingsPage);
    const component = fixture.componentInstance;
    await component.ngOnInit();

    expect(component.editingAgentProfile()).toBeNull();
    expect(component.agentProfileContent()).toContain('ProductCrew Working Standard');

    component.beginAgentProfileEdit();
    component.agentProfileDraft.set('# Working Standard\n\nUpdated instructions.');
    await component.saveAgentProfile();

    expect(component.editingAgentProfile()).toBeNull();
    expect(component.agentProfileContent()).toContain('Updated instructions.');
  });

  it('loads the effective role profile and updates skill routing independently', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.selectAgentProfile('developer');
    await component.loadEffectiveProfile('developer');
    expect(component.effectiveProfile()?.skills).toContain('minimal-safe-implementation');

    const skill = component.agentStudioProfile()!.agentSkills[1];
    expect(component.isSkillAssigned(skill, 'developer')).toBe(true);
    await component.toggleSkillAssignment(skill);

    expect(component.agentStudioProfile()!.agentSkills[1].roles).not.toContain('developer');
  });

  it('supports a custom skill assignment for Feature Radar', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.selectAgentProfile('featureRadar');
    expect(component.agentProfileContent()).toContain('# Feature Radar');

    const skill = component.agentStudioProfile()!.agentSkills[0];
    expect(component.isSkillAssigned(skill, 'featureRadar')).toBe(false);
    await component.toggleSkillAssignment(skill);

    expect(component.agentStudioProfile()!.agentSkills[0].roles).toContain('feature-radar');
  });

  it('triggers settings backup export', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    vi.spyOn(window.URL, 'createObjectURL').mockReturnValue('blob:url');
    vi.spyOn(window.URL, 'revokeObjectURL').mockImplementation(() => {});
    const dummyLink = document.createElement('a');
    vi.spyOn(dummyLink, 'click').mockImplementation(() => {});
    const originalCreateElement = document.createElement.bind(document);
    const docSpy = vi.spyOn(document, 'createElement').mockImplementation((tagName: string, options?: any) => {
      if (tagName === 'a') {
        return dummyLink as any;
      }
      return originalCreateElement(tagName, options);
    });

    await component.exportSettingsBackup();
    expect(component.successMessage()).toBe('Settings backup downloaded successfully.');
    expect(component.exportSuccess()).toBe('Settings backup downloaded successfully.');
    expect(component.exportLoading()).toBe(false);
    expect(component.exportError()).toBeNull();

    docSpy.mockRestore();
  });

  it('handles failed settings backup export', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    // Force exportBackup to throw error with structure expected by errorText
    vi.spyOn(component['projectApi'], 'exportBackup').mockReturnValue(
      throwError(() => ({ error: { error: 'Export failed dynamically' } }))
    );

    await component.exportSettingsBackup();
    expect(component.errorMessage()).toBe('Export failed dynamically');
    expect(component.exportError()).toBe('Export failed dynamically');
    expect(component.exportLoading()).toBe(false);
    expect(component.exportSuccess()).toBeNull();
  });

  it('disables actions and sets exportLoading during export execution', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    // Create a Subject to control when the backup export completes/emits
    const exportSubject = new Subject<any>();
    vi.spyOn(component['projectApi'], 'exportBackup').mockReturnValue(exportSubject);
    vi.spyOn(window.URL, 'createObjectURL').mockReturnValue('blob:url');
    vi.spyOn(window.URL, 'revokeObjectURL').mockImplementation(() => {});
    const dummyLink = document.createElement('a');
    vi.spyOn(dummyLink, 'click').mockImplementation(() => {});
    const originalCreateElement = document.createElement.bind(document);
    const docSpy = vi.spyOn(document, 'createElement').mockImplementation((tagName: string, options?: any) => {
      if (tagName === 'a') {
        return dummyLink as any;
      }
      return originalCreateElement(tagName, options);
    });

    // Call exportSettingsBackup (don't await yet, as it won't resolve until Subject emits)
    const exportPromise = component.exportSettingsBackup();

    // Check that exportLoading() is true and there are no success/error messages
    expect(component.exportLoading()).toBe(true);
    expect(component.exportSuccess()).toBeNull();
    expect(component.exportError()).toBeNull();

    // Emit and complete
    exportSubject.next({ version: 1, settings: {} });
    exportSubject.complete();

    await exportPromise;

    // After completion, exportLoading should be false
    expect(component.exportLoading()).toBe(false);

    docSpy.mockRestore();
  });

  it('handles valid and invalid settings restore files', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    const waitBackupLoaded = async () => {
      // Small initial wait to ensure the async FileReader.readAsText has queued its task
      await new Promise((resolve) => setTimeout(resolve, 5));
      while (component.backupLoading()) {
        await new Promise((resolve) => setTimeout(resolve, 5));
      }
    };

    // 1. Invalid file content (missing settings property)
    const invalidFile = new File([JSON.stringify({ version: 1 })], 'invalid.json', { type: 'application/json' });
    component.handleBackupFile(invalidFile);
    await waitBackupLoaded();
    expect(component.backupError()).toContain('Invalid Backup File');

    // 2. Unsupported version (> 1)
    const unsupportedFile = new File([JSON.stringify({ version: 2, settings: {} })], 'unsupported.json', { type: 'application/json' });
    component.handleBackupFile(unsupportedFile);
    await waitBackupLoaded();
    expect(component.backupError()).toContain('Unsupported Backup Version');
    expect(component.backupErrorVersion()).toBe(true);

    // 3. Valid file - parses, diffs and applies restore
    const validPayload = {
      version: 1,
      createdAt: '2026-08-26T19:49:02Z',
      settings: {
        clonePath: 'C:\\AI-Workspaces-Restore',
        aiProvider: 'claude-code'
      }
    };
    const validFile = new File([JSON.stringify(validPayload)], 'valid.json', { type: 'application/json' });
    component.handleBackupFile(validFile);
    await waitBackupLoaded();

    expect(component.showDiffPanel()).toBe(true);
    expect(component.diffList().find(d => d.key === 'clonePath')?.status).toBe('modified');
    expect(component.diffList().find(d => d.key === 'clonePath')?.newVal).toBe('C:\\AI-Workspaces-Restore');

    // 4. Cancel restore resets state
    component.cancelRestore();
    expect(component.showDiffPanel()).toBe(false);
    expect(component.backupPreview()).toBeNull();

    // 5. Apply restore overwrites forms and settings
    component.handleBackupFile(validFile);
    await waitBackupLoaded();
    await component.applyRestore();

    expect(component.form.controls.clonePath.value).toBe('C:\\AI-Workspaces-Restore');
    expect(component.form.controls.aiProvider.value).toBe('claude-code');
    expect(component.successMessage()).toBe('Settings restored successfully from backup.');

    // 6. Malformed file content (corrupt JSON syntax)
    const malformedFile = new File(['{malformed'], 'malformed.json', { type: 'application/json' });
    component.handleBackupFile(malformedFile);
    await waitBackupLoaded();
    expect(component.backupError()).toContain('Invalid Backup File');
    expect(component.backupErrorVersion()).toBe(false);
  });

  it('loads the active storage datasource as Ready by default', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    expect(component.datasourceStatus()?.kind).toBe('local-json');
    expect(component.datasourceStatus()?.status).toBe('ok');
    expect(component.datasourceBadgeLabel()).toBe('Ready');
  });

  it('shows Needs attention when the active datasource is unavailable', async () => {
    datasourceStatus = {
      kind: 'sqlite',
      summary: 'SQLite database at C:\\data\\productcrew.db',
      status: 'unavailable',
      message: 'database is locked',
      lastCheckedAt: '2026-08-27T09:00:00Z',
      details: { path: 'C:\\data\\productcrew.db' },
    };
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    expect(component.datasourceBadgeLabel()).toBe('Needs attention');
  });

  it('re-checks the active datasource status without switching it', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    await component.checkActiveDatasource();

    expect(component.datasourceStatus()?.kind).toBe('local-json');
    expect(component.datasourceChecking()).toBe(false);
  });

  it('invalidates a successful target check when the target field changes afterwards', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.openChangeDatasource();
    component.selectTargetKind('sqlite');
    component.targetSqlitePath.set('C:\\data\\productcrew.db');
    await component.checkTargetDatasource();

    expect(component.targetCheckResult()?.status).toBe('ok');
    expect(component.changeStep()).toBe(2);

    component.updateTargetSqlitePath({ target: { value: 'C:\\data\\other.db' } } as unknown as Event);

    expect(component.targetCheckResult()).toBeNull();
    expect(component.targetCheckStale()).toBe(true);
    expect(component.continueToMigration()).toBeUndefined();
    expect(component.changeStep()).toBe(2);
  });

  it('requires a fresh successful check before continuing to the migration step', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.openChangeDatasource();
    component.selectTargetKind('sqlite');
    component.targetSqlitePath.set('C:\\data\\productcrew.db');
    await component.checkTargetDatasource();
    component.continueToMigration();

    expect(component.changeStep()).toBe(3);
  });

  it('completes a guarded migration and reports migrated counts per entity', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.openChangeDatasource();
    component.selectTargetKind('sqlite');
    component.targetSqlitePath.set('C:\\data\\productcrew.db');
    await component.checkTargetDatasource();
    component.continueToMigration();
    component.toggleMigrationAcknowledged({ target: { checked: true } } as unknown as Event);
    await component.confirmMigration();

    expect(component.migrationResult()?.applied).toBe(true);
    expect(component.migrationResult()?.counts.boards).toBe(3);
    expect(component.changeFlowOpen()).toBe(false);
  });

  it('blocks migration until the confirmation checkbox is acknowledged', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.openChangeDatasource();
    component.selectTargetKind('sqlite');
    component.targetSqlitePath.set('C:\\data\\productcrew.db');
    await component.checkTargetDatasource();
    component.continueToMigration();
    await component.confirmMigration();

    expect(projectApi.switchStorageDatasource).not.toHaveBeenCalled();
    expect(component.migrationResult()).toBeNull();
  });

  it('keeps the previous datasource active and reports rollback on migration failure', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    projectApi.switchStorageDatasource.mockReturnValueOnce(throwError(() => ({
      error: {
        error: 'migrate datasource: write probe failed',
        result: {
          sourceKind: 'local-json',
          targetKind: 'sqlite',
          applied: false,
          status: 'rolled-back',
          message: 'write probe failed',
          counts: { boards: 1, insights: 0, events: 0, notifications: 0, buildProfiles: 0 },
          durationMs: 12,
          activeDatasource: { kind: 'local-json', localJson: { directory: 'C:\\Users\\test\\.productcrew\\data' }, sqlite: { path: '' } },
        },
      },
    })));

    component.openChangeDatasource();
    component.selectTargetKind('sqlite');
    component.targetSqlitePath.set('C:\\data\\productcrew.db');
    await component.checkTargetDatasource();
    component.continueToMigration();
    component.toggleMigrationAcknowledged({ target: { checked: true } } as unknown as Event);
    await component.confirmMigration();

    expect(component.migrationResult()?.applied).toBe(false);
    expect(component.migrationResult()?.status).toBe('rolled-back');
    expect(component.migrationError()).toContain('write probe failed');
  });

  it('defaults email event preferences to the high-signal set without exposing the SMTP password', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    expect(component.emailForm.controls.password.value).toBe('');
    expect(component.emailForm.controls.eventPreferences.controls.taskCompleted.value).toBe(true);
    expect(component.emailForm.controls.eventPreferences.controls.approvalRequired.value).toBe(true);
    expect(component.emailSettings()?.secretConfigured).toBe(false);
    expect(component.canEnableEmail()).toBe(false);
  });

  it('blocks enabling email notifications until a passing test email is sent for the current config', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.emailForm.patchValue({ destinationEmail: 'me@example.com', senderAddress: 'crew@example.com', smtpHost: 'smtp.example.com' });
    await component.saveEmailSettings();
    expect(component.emailSettings()?.destinationEmail).toBe('me@example.com');
    expect(component.canEnableEmail()).toBe(false);

    component.emailForm.patchValue({ enabled: true });
    await component.saveEmailSettings();
    expect(component.emailError()).toContain('passing test email');
    expect(component.emailSettings()?.enabled).toBe(false);

    await component.sendTestEmail();
    expect(component.emailSettings()?.lastTestResult).toBe('passed');
    expect(component.canEnableEmail()).toBe(true);

    component.emailForm.patchValue({ enabled: true });
    await component.saveEmailSettings();
    expect(component.emailSettings()?.enabled).toBe(true);
  });

  it('persists individual event preference toggles across a settings reload', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();

    component.emailForm.patchValue({ destinationEmail: 'me@example.com', senderAddress: 'crew@example.com', smtpHost: 'smtp.example.com' });
    component.emailForm.controls.eventPreferences.patchValue({ qaResults: false });
    await component.saveEmailSettings();

    expect(component.emailSettings()?.eventPreferences.qaResults).toBe(false);

    const reloaded = TestBed.createComponent(SettingsPage).componentInstance;
    await reloaded.ngOnInit();
    expect(reloaded.emailForm.controls.eventPreferences.controls.qaResults.value).toBe(false);
  });

  it('surfaces the last send failure summary without a global toast', async () => {
    const component = TestBed.createComponent(SettingsPage).componentInstance;
    await component.ngOnInit();
    emailNotificationSettings = { ...emailNotificationSettings, lastSendFailureAt: '2026-09-13T09:00:00Z', lastSendFailureSummary: 'dial tcp: connection refused' };
    component.settings.set({ ...settings, emailNotifications: emailNotificationSettings });

    expect(component.emailSettings()?.lastSendFailureSummary).toBe('dial tcp: connection refused');
    expect(component.errorMessage()).toBeNull();
  });
});
