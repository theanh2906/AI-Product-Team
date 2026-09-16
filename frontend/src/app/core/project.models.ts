export interface ProjectSettings {
  clonePath: string;
  gitProvider: 'github' | 'gitlab' | 'generic';
  gitUsername: string;
  githubAccount: string;
  aiProvider: AIProvider;
  aiFallbackProvider: AIProvider | '';
  aiFallbackEnabled: boolean;
  codexModel: string;
  codexEffort: string;
  claudeCodeModel: string;
  claudeCodeEffort: string;
  githubCopilotModel: string;
  githubCopilotEffort: string;
  theme: 'light' | 'dark';
  patConfigured: boolean;
  credentialType: string;
  configPath: string;
  boardDataPath: string;
  insightDataPath: string;
  eventDataPath: string;
  notificationDataPath?: string;
  buildProfileDataPath?: string;
  logPath: string;
  updatePath: string;
  agentProfileVersion: number;
  workingStandard: string;
  agentSkills: AgentSkill[];
  roleDefinitions: AgentRoleDefinitions;
  emailNotifications: EmailNotificationSettings;
}

export interface EmailEventPreferences {
  taskCompleted: boolean;
  blockedNeedsAttention: boolean;
  qaResults: boolean;
  buildVerificationFailed: boolean;
  approvalRequired: boolean;
}

export type EmailSmtpSecurity = 'none' | 'starttls' | 'tls';

export interface EmailNotificationSettings {
  enabled: boolean;
  destinationEmail: string;
  senderName: string;
  senderAddress: string;
  smtpHost: string;
  smtpPort: number;
  smtpSecurity: EmailSmtpSecurity | string;
  smtpUsername: string;
  secretConfigured: boolean;
  appBaseUrl: string;
  eventPreferences: EmailEventPreferences;
  readyToEnable: boolean;
  lastTestResult: string;
  lastTestAt?: string;
  lastTestError?: string;
  lastTestHostPort?: string;
  lastSendFailureAt?: string;
  lastSendFailureSummary?: string;
}

export interface UpdateEmailNotificationSettingsRequest {
  enabled: boolean;
  destinationEmail: string;
  senderName: string;
  senderAddress: string;
  smtpHost: string;
  smtpPort: number;
  smtpSecurity: string;
  smtpUsername: string;
  appBaseUrl: string;
  eventPreferences: EmailEventPreferences;
  password: string;
  clearPassword: boolean;
}

export interface EmailTestResult {
  success: boolean;
  message?: string;
  settings: EmailNotificationSettings;
}

export interface ProjectAgentProfile {
  projectId: string;
  projectName: string;
  projectPath: string;
  dataPath: string;
  inherited: boolean;
  agentProfileVersion: number;
  workingStandard: string;
  agentSkills: AgentSkill[];
  roleDefinitions: AgentRoleDefinitions;
  learnedRoleGuidance?: AgentRoleDefinitions;
  updatedAt?: string;
}

export interface ProjectStudyEvidence {
  file: string;
  line: number;
  detail: string;
}

export interface ProjectStudyFolder {
  name: string;
  path: string;
  kind: string;
  children: string[];
}

export interface ProjectStudyComponent {
  id: string;
  name: string;
  kind: string;
  description: string;
  path: string;
  entryFiles: string[];
  responsibilities: string[];
  evidence: ProjectStudyEvidence[];
  confidence: number;
}

export interface ProjectStudyRelation {
  from: string;
  to: string;
  label: string;
  kind: string;
}

export interface ProjectStudyDataEntity {
  name: string;
  kind: string;
  description: string;
  path: string;
  fields: string[];
  relations: string[];
  evidence: ProjectStudyEvidence[];
}

export interface ProjectStudySequence {
  id: string;
  name: string;
  description: string;
  steps: Array<{ order: number; from: string; to: string; label: string }>;
  evidence: ProjectStudyEvidence[];
}

export interface ProjectStudyWorkflow {
  id: string;
  name: string;
  description: string;
  steps: string[];
  evidence: ProjectStudyEvidence[];
}

export interface ProjectStudyRoleGuidance {
  role: AgentRuntimeRole;
  summary: string;
  instructions: string;
  confidence: number;
  evidence: ProjectStudyEvidence[];
}

export interface ProjectStudyResult {
  schemaVersion: number;
  studyId: string;
  projectId: string;
  projectName: string;
  projectPath: string;
  studiedAt: string;
  durationMs: number;
  status: 'completed' | 'needs_attention';
  freshness: 'fresh' | 'stale';
  fingerprint: string;
  summary: string;
  enrichmentError?: string;
  scopeNote: string;
  coverage: { filesScanned: number; foldersMapped: number; evidenceSources: number; ignoredEntries: number };
  folders: ProjectStudyFolder[];
  components: ProjectStudyComponent[];
  relations: ProjectStudyRelation[];
  dataEntities: ProjectStudyDataEntity[];
  sequences: ProjectStudySequence[];
  workflows: ProjectStudyWorkflow[];
  roleGuidance: ProjectStudyRoleGuidance[];
  guidanceApplied: boolean;
  guidanceAppliedAt?: string;
}

export interface ProjectStudyLog {
  id: string;
  timestamp: string;
  phase: string;
  message: string;
}

export interface ProjectStudyJob {
  studyId: string;
  projectId: string;
  projectName: string;
  status: 'running' | 'completed' | 'needs_attention';
  phase: string;
  progress: number;
  startedAt: string;
  completedAt?: string;
  logs: ProjectStudyLog[];
  result?: ProjectStudyResult;
  error?: string;
}

export type AIProvider = 'codex' | 'claude-code' | 'github-copilot';

export interface ProductWorkspaceFolder {
  projectId: string;
  name: string;
  path: string;
  access: 'read-only' | 'read-write';
  primary?: boolean;
}

export interface ProductWorkspace {
  schemaVersion: number;
  id: string;
  name: string;
  folders: ProductWorkspaceFolder[];
  settings: { queueMode: 'serial' };
  filePath: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateProductWorkspaceRequest {
  name: string;
  folders: Array<{ projectId: string; access: 'read-only' | 'read-write'; primary: boolean }>;
}

export type AgentRoleKey = 'teamLead' | 'designer' | 'developer' | 'qa' | 'bugScanner' | 'featureRadar';

export interface AgentRoleDefinitions {
  teamLead: string;
  designer: string;
  developer: string;
  qa: string;
  bugScanner: string;
  featureRadar: string;
}

export interface AgentSkill {
  id: string;
  name: string;
  description: string;
  instructions: string;
  roles: AgentRuntimeRole[];
}

export type AgentRuntimeRole = 'team-lead' | 'designer' | 'developer' | 'qa' | 'bug-scanner' | 'feature-radar';

export interface EffectiveAgentProfile {
  role: AgentRuntimeRole;
  version: number;
  skills: string[];
  content: string;
}

export interface UpdateProjectSettingsRequest {
  clonePath: string;
  gitProvider: string;
  gitUsername: string;
  updatePath: string;
  aiProvider: string;
  aiFallbackProvider: string;
  aiFallbackEnabled: boolean;
  codexModel: string;
  codexEffort: string;
  claudeCodeModel: string;
  claudeCodeEffort: string;
  githubCopilotModel: string;
  githubCopilotEffort: string;
  personalAccessToken: string;
  clearPersonalAccessToken: boolean;
}

export interface AgentModelOption {
  id: string;
  label: string;
  description?: string;
  defaultEffort?: string;
  efforts: string[];
  source: string;
}

export interface AgentModelOptionGroup {
  provider: AIProvider;
  source: string;
  error?: string;
  models: AgentModelOption[];
  efforts: string[];
}

export interface AgentModelOptionsResponse {
  providers: AgentModelOptionGroup[];
}

export interface UpdateRoleDefinitionRequest {
  content: string;
}

export interface UpdateAgentSkillRequest {
  name: string;
  description: string;
  instructions: string;
  roles: AgentRuntimeRole[];
}

export interface ImportedProject {
  id: string;
  name: string;
  path: string;
  source: 'git' | 'local';
  remoteUrl?: string;
  branch?: string;
  importedAt: string;
}

export interface ProductCreationProfile {
  id: string;
  name: string;
  description: string;
  tools: string[];
}

export interface ProductBlueprintRequest {
  productName: string;
  idea: string;
  destinationParent: string;
  profileId: string;
  startingPoint: 'blank' | 'guided' | 'template';
  initializeGit: boolean;
}

export interface ProductBlueprint {
  schemaVersion: number;
  id: string;
  status: 'draft' | 'created';
  productName: string;
  idea: string;
  destination: string;
  profileId: string;
  profileName: string;
  startingPoint: string;
  initializeGit: boolean;
  summary: string;
  criteria: string[];
  components: Array<{ name: string; path: string; description: string }>;
  milestones: Array<{ title: string; roles: string[]; tasks: number }>;
  decisions: string[];
  preflight: { ready: boolean; warnings: string[]; tools: string[] };
  createdAt: string;
  updatedAt: string;
  source: string;
  warning?: string;
}

export interface ProductCreationResult {
  project: ImportedProject;
  backlogItemId: string;
}

export interface ProjectRemovalResult {
  project: ImportedProject;
  boardRemoved: boolean;
  insightsRemoved: number;
}

export interface GitRepository {
  id: number;
  name: string;
  fullName: string;
  cloneUrl: string;
  defaultBranch: string;
  private: boolean;
  archived: boolean;
}

export interface GitCredential {
  login: string;
  host: string;
  active: boolean;
  selected: boolean;
  source: string;
}

export interface ImportGitProjectRequest {
  repositoryUrl: string;
  branch: string;
  directoryName: string;
}

export interface FolderSelection {
  path: string;
}

export type SystemToolStatus = 'ready' | 'update_available' | 'not_installed' | 'check_failed';
export type SystemToolCheckOutcome = 'passed' | 'warning' | 'failed' | 'blocked';

export interface SystemToolCheck {
  id: string;
  label: string;
  detail: string;
  outcome: SystemToolCheckOutcome;
}

export interface SystemTool {
  id: string;
  name: string;
  description: string;
  icon: string;
  required: boolean;
  status: SystemToolStatus;
  installed: boolean;
  installedVersion?: string;
  latestVersion?: string;
  executablePath?: string;
  account?: string;
  action?: 'install' | 'update';
  actionAvailable: boolean;
  packageManager?: 'winget' | 'nvm' | 'external';
  message?: string;
  checks: SystemToolCheck[];
}

export interface ToolchainSnapshot {
  operational: boolean;
  actionsRecommended: number;
  packageManagerReady: boolean;
  checkedAt: string;
  tools: SystemTool[];
}

export interface BuildAction {
  id: string;
  label: string;
  description: string;
  executable: string;
  arguments: string[];
  workingDir: string;
  source: string;
  recommended: boolean;
  confidence: number;
}

export interface BuildRun {
  id: string;
  projectId: string;
  taskId?: string;
  actionId: string;
  actionLabel: string;
  command: string;
  status: 'queued' | 'running' | 'passed' | 'failed' | 'timeout';
  exitCode: number;
  reason?: string;
  logPath: string;
  startedAt: string;
  completedAt?: string;
  durationMs: number;
}

export interface BuildProfile {
  projectId: string;
  projectName?: string;
  projectPath?: string;
  status: 'not_detected' | 'not_configured' | 'ready';
  fingerprint?: string;
  detector?: string;
  summary?: string;
  warning?: string;
  configFiles: string[];
  actions: BuildAction[];
  selectedActionId?: string;
  detectedAt?: string;
  lastRun?: BuildRun;
}

export interface DeployAction {
  id: string;
  label: string;
  description: string;
  executable: string;
  arguments: string[];
  workingDir: string;
  source: string;
  recommended: boolean;
  confidence: number;
}

export interface DeployRun {
  id: string;
  projectId: string;
  actionId: string;
  actionLabel: string;
  command: string;
  status: 'queued' | 'running' | 'passed' | 'failed' | 'timeout';
  exitCode: number;
  reason?: string;
  logPath: string;
  startedAt: string;
  completedAt?: string;
  durationMs: number;
}

export interface DeployProfile {
  projectId: string;
  projectName?: string;
  projectPath?: string;
  status: 'not_detected' | 'not_configured' | 'ready';
  fingerprint?: string;
  detector?: string;
  summary?: string;
  warning?: string;
  configFiles: string[];
  actions: DeployAction[];
  selectedActionId?: string;
  detectedAt?: string;
  lastRun?: DeployRun;
}

export interface BackupPayload {
  version: number;
  createdAt: string;
  settings: any;
}

export interface BackupPreviewResponse {
  current: ProjectSettings;
  incoming: any;
}

export type StorageDatasourceKind = 'local-json' | 'sqlite';

export interface LocalJSONDatasourceConfig {
  directory: string;
}

export interface SQLiteDatasourceConfig {
  path: string;
}

export interface StorageDatasourceConfig {
  kind: StorageDatasourceKind;
  localJson: LocalJSONDatasourceConfig;
  sqlite: SQLiteDatasourceConfig;
}

export interface StorageDatasourceStatus {
  kind: StorageDatasourceKind;
  summary: string;
  status: 'ok' | 'unavailable';
  message?: string;
  lastCheckedAt: string;
  details?: Record<string, string>;
}

export interface StorageDatasourceMigrationCounts {
  boards: number;
  insights: number;
  events: number;
  notifications: number;
  buildProfiles: number;
  gitDeliveries?: number;
}

export interface StorageDatasourceMigrationResult {
  sourceKind: StorageDatasourceKind;
  targetKind: StorageDatasourceKind;
  applied: boolean;
  status: 'applied' | 'rolled-back';
  message?: string;
  counts: StorageDatasourceMigrationCounts;
  durationMs: number;
  activeDatasource: StorageDatasourceConfig;
}
