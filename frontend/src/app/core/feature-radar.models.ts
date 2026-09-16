export interface FeatureEvidence {
  file: string;
  line: number;
  detail: string;
}

export interface FeatureSuggestion {
  id: string;
  category: string;
  title: string;
  problem: string;
  proposal: string;
  rationale: string;
  feasibility: number;
  confidence: 'high' | 'medium' | 'low';
  impact: 'high' | 'medium' | 'low';
  effort: 'small' | 'medium' | 'large';
  requiresUI: boolean;
  deliveryTarget: 'frontend' | 'backend' | 'fullstack';
  evidence: FeatureEvidence[];
  implementationOutline: string[];
  acceptanceCriteria: string[];
  risks: string[];
  status: 'suggested' | 'backlog' | 'planning' | 'ignore';
  backlogItemId?: string;
}

export interface FeatureRadarResult {
  analysisId: string;
  projectId: string;
  projectName: string;
  analyzedAt: string;
  durationMs: number;
  scopeNote: string;
  summary: string;
  suggestions: FeatureSuggestion[];
}

export interface FeatureRadarJob {
  analysisId: string;
  projectId: string;
  projectName: string;
  status: 'running' | 'completed' | 'failed';
  startedAt: string;
  completedAt?: string;
  result?: FeatureRadarResult;
  error?: string;
}

export interface UpdateFeatureSuggestionResponse {
  result: FeatureRadarResult;
}
