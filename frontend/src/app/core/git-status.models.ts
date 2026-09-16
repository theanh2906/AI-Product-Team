export type GitAvailability = 'ok' | 'unavailable' | 'not-a-repository';

export interface GitDirtyFile {
  path: string;
  changeType: string;
}

export interface GitStatus {
  status: GitAvailability;
  branch?: string;
  dirty: GitDirtyFile[];
}

export interface GitCommit {
  sha: string;
  shortSha: string;
  author: string;
  date: string;
  subject: string;
}

export interface GitCommitsResponse {
  status: GitAvailability;
  commits: GitCommit[];
}

export interface TaskGitReference {
  branch?: string;
  commitSha?: string;
  linkedAt: string;
  linkedBy?: string;
}

export interface TaskGitReferenceRequest {
  branch?: string;
  commitSha?: string;
  linkedBy?: string;
}

export type TaskGitBranchStatus = 'proposed' | 'created' | 'conflict';

export interface TaskGitBranchResponse {
  status: TaskGitBranchStatus;
  branchName: string;
}

export type GitDeliveryStatus = 'queued' | 'running' | 'recovering' | 'paused' | 'completed' | 'failed';
export type GitDeliveryStep = 'preflight' | 'commit' | 'push' | 'complete';

export interface GitDeliveryTicket {
  taskId: string;
  key: string;
  title: string;
  files: string[];
}

export interface GitDeliveryEvent {
  id: string;
  timestamp: string;
  level: string;
  step: GitDeliveryStep;
  message: string;
  detail?: string;
}

export interface GitDelivery {
  id: string;
  projectId: string;
  projectName: string;
  branch: string;
  remote: string;
  status: GitDeliveryStatus;
  step: GitDeliveryStep;
  headBefore: string;
  commitSha?: string;
  shortSha?: string;
  commitMessage: string;
  fingerprint: string;
  tickets: GitDeliveryTicket[];
  files: string[];
  events: GitDeliveryEvent[];
  recovery?: { status: string; provider?: string; threadId?: string; attempt: number; summary?: string };
  error?: string;
  stopRequested?: boolean;
  startedAt: string;
  updatedAt: string;
  completedAt?: string;
}

export interface GitDeliveryPreview {
  projectId: string;
  projectName: string;
  branch: string;
  remote: string;
  headSha: string;
  fingerprint: string;
  commitMessage: string;
  tickets: GitDeliveryTicket[];
  files: string[];
  stagedFiles: string[];
  unattributedFiles: string[];
  ready: boolean;
  blockers: string[];
  latestDelivery?: GitDelivery;
}
