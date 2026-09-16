export type FindingSeverity = 'critical' | 'high' | 'medium' | 'low';

export interface SourceCodeLine {
  number: number;
  content: string;
  highlighted: boolean;
}

export interface SourceScanRequest {
  projectId: string;
  depth: 'focused' | 'balanced' | 'deep';
  focusAreas: string[];
}

export interface SourceScanFinding {
  id: string;
  severity: FindingSeverity;
  category: string;
  title: string;
  description: string;
  evidence: string;
  file: string;
  line: number;
  codeExcerpt?: SourceCodeLine[];
  recommendation: string;
  status: 'suggested' | 'backlog' | 'planning' | 'ignore';
  backlogItemId?: string;
}

export interface SourceScanResult {
  scanId: string;
  projectId: string;
  projectName: string;
  scannedAt: string;
  durationMs: number;
  filesNote: string;
  summary: string;
  findings: SourceScanFinding[];
}

export interface SourceScanJob {
  scanId: string;
  projectId: string;
  projectName: string;
  status: 'running' | 'completed' | 'failed';
  startedAt: string;
  completedAt?: string;
  result?: SourceScanResult;
  error?: string;
}

export interface UpdateFindingResponse {
  result: SourceScanResult;
}
