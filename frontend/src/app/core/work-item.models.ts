import { TaskGitReference } from './git-status.models';

export type AgentRole = 'designer' | 'developer' | 'qa';
export type BoardColumn = 'backlog' | 'planning' | 'designer' | 'developer' | 'qa' | 'done';
export type TaskStatus = 'planned' | 'queued' | 'in_progress' | 'verifying' | 'design_review' | 'blocked' | 'completed';
export type PlanningStatus =
  | 'analyzing'
  | 'awaiting_approval'
  | 'approved'
  | 'changes_requested'
  | 'failed'
  | 'completed'
  | 'not_feasible';
export type TaskPriority = 'critical' | 'high' | 'medium' | 'low';

export interface WorkRequest {
  id: string;
  artifactPath?: string;
  title: string;
  description: string;
  workType: string;
  deliveryTarget: string;
  requiresUI: boolean;
  acceptanceCriteria: string[];
  attachments: RequestAttachment[];
  intake?: IntakeSubmission;
  createdAt: string;
}

export interface RequestAttachment {
  id: string;
  originalName: string;
  relativePath: string;
  mediaType: string;
  size: number;
  sha256: string;
}

export interface PlanDocument {
  id: string;
  planId: string;
  title: string;
  kind: string;
  audience: AgentRole[];
  content: string;
  version: number;
  createdAt: string;
}

export interface PlanReview {
  decision: 'approve' | 'deny';
  reason?: string;
  reviewer: string;
  revision: number;
  createdAt: string;
}

export interface BoardPlan {
  id: string;
  request: WorkRequest;
  status: PlanningStatus;
  revision: number;
  summary?: string;
  error?: string;
  threadId?: string;
  preflight?: PlanPreflight;
  documents: PlanDocument[];
  reviews: PlanReview[];
  sequence?: PlanSequence;
  createdAt: string;
  updatedAt: string;
}

export interface PlanPreflight {
  status: 'needs_work' | 'already_implemented' | 'not_feasible';
  summary: string;
  evidence: string[];
  verification: string[];
  remainingWork?: string[];
  completedAt?: string;
}

export interface PlanSequence {
  status: 'scheduled' | 'running' | 'blocked' | 'completed';
  blockedReason?: string;
  scheduledAt: string;
  startedAt?: string;
  completedAt?: string;
}

export interface QueueControl {
  status: 'stopping' | 'paused';
  requestedAt?: string;
  pausedAt?: string;
  updatedAt: string;
}

export interface BoardTask {
  id: string;
  planId: string;
  key: string;
  title: string;
  role: AgentRole;
  column: BoardColumn;
  status: TaskStatus;
  priority: TaskPriority;
  description: string;
  acceptanceCriteria: string[];
  dependencyIds: string[];
  documentIds: string[];
  blockedReason?: string;
  threadId?: string;
  execution?: TaskExecution;
  revisionHistory?: TaskRevision[];
  buildVerification?: BuildVerification;
  gitReference?: TaskGitReference;
  sequenceOrder?: number;
  createdAt: string;
  updatedAt: string;
}

export interface TaskExecution {
  verdict?: 'passed' | 'failed' | 'blocked' | 'ignored';
  summary: string;
  changedFiles: string[];
  verification: string[];
  remainingRisks: string[];
  findings?: TaskFinding[];
  artifacts?: DesignArtifact[];
  buildVerification?: BuildVerification;
  completedAt: string;
}

export interface TaskRevision {
  revision: number;
  feedback?: string;
  reviewer?: string;
  requestedAt: string;
  threadId?: string;
  execution: TaskExecution;
}

export interface BuildVerification {
  runId: string;
  actionId: string;
  actionLabel: string;
  command: string;
  status: 'running' | 'passed' | 'failed' | 'timeout';
  exitCode: number;
  reason?: string;
  logPath?: string;
  startedAt: string;
  completedAt?: string;
  durationMs: number;
}

export interface DesignArtifact {
  id: string;
  title: string;
  kind: string;
  relativePath: string;
  mediaType: string;
  width?: number;
  height?: number;
}

export interface TaskSessionEntry {
  id: string;
  timestamp: string;
  level: 'info' | 'warning' | 'error';
  kind: string;
  message: string;
  detail?: string;
}

export interface TaskSessionLog {
  projectId: string;
  taskId?: string;
  planId?: string;
  provider?: string;
  agent?: string;
  status: 'idle' | 'running' | 'completed' | 'failed';
  startedAt?: string;
  updatedAt: string;
  entries: TaskSessionEntry[];
}

export interface TaskFinding {
  severity: string;
  title: string;
  description: string;
  evidence: string;
  steps: string[];
  expected: string;
  actual: string;
  affectedFiles: string[];
  linkedBacklogId?: string;
  linkedBacklogKey?: string;
  linkedBacklogTitle?: string;
  linkedBacklogStatus?: BacklogItem['status'];
  linkedBacklogPlanId?: string;
}

export interface ProjectBoard {
  id: string;
  projectId: string;
  projectName: string;
  backlog: BacklogItem[];
  plans: BoardPlan[];
  tasks: BoardTask[];
  activeTaskId?: string;
  queueControl?: QueueControl;
  createdAt: string;
  updatedAt: string;
}

export interface BacklogItem {
  id: string;
  requestId: string;
  artifactPath?: string;
  key: string;
  type: 'feature' | 'bug' | 'todo';
  source: string;
  sourceReference?: string;
  title: string;
  description: string;
  deliveryTarget: string;
  requiresUI: boolean;
  acceptanceCriteria: string[];
  attachments: RequestAttachment[];
  intake?: IntakeSubmission;
  feasibility?: number;
  severity?: string;
  status: 'backlog' | 'planning' | 'done' | 'not_feasible';
  planId?: string;
  repeatReports?: number;
  lastReportedAt?: string;
  waitingQaTaskIds?: string[];
  createdAt: string;
  updatedAt: string;
}

export interface CreatePlanRequest {
  title: string;
  description: string;
  workType: string;
  deliveryTarget: string;
  requiresUI: boolean;
  acceptanceCriteria: string[];
}

export interface CreateBacklogRequest extends CreatePlanRequest {
  type: 'feature' | 'bug' | 'todo';
  source: string;
  intake?: IntakeSubmission;
}

export type IntakeControlType =
  | 'toggle'
  | 'radio'
  | 'dropdown'
  | 'checkbox'
  | 'scale'
  | 'short_text'
  | 'info';

export interface IntakeOption {
  value: string;
  label: string;
  description: string;
  acceptanceCriterion?: string;
}

export interface IntakeQuestion {
  id: string;
  label: string;
  helpText: string;
  type: IntakeControlType;
  binding: 'context' | 'requiresUI' | 'deliveryTarget' | 'acceptanceCriteria';
  required: boolean;
  options: IntakeOption[];
  defaultValues: string[];
  layout: { span: 6 | 12 };
}

export interface IntakeQuestionnaire {
  schemaVersion: number;
  heading: string;
  summary: string;
  workType: 'feature' | 'bug' | 'todo';
  source: string;
  warning?: string;
  questions: IntakeQuestion[];
}

export interface IntakeSubmission {
  questionnaire: IntakeQuestionnaire;
  answers: Record<string, string[]>;
}

export interface ReviewPlanRequest {
  decision: 'approve' | 'deny';
  reason: string;
  reviewer: string;
  runSequence?: boolean;
}
