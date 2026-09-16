export interface ObservationEvent {
  id: string;
  timestamp: string;
  level: 'debug' | 'info' | 'warn' | 'error';
  category: string;
  name: string;
  message: string;
  correlationId?: string;
  projectId?: string;
  entityType?: string;
  entityId?: string;
  agent?: string;
  stage?: string;
  outcome?: string;
  durationMs?: number;
  attributes?: Record<string, unknown>;
}

export interface DistributionItem { key: string; label: string; value: number; color?: string; }
export interface TrendPoint { date: string; features: number; bugs: number; done: number; }
export interface ObservabilityMetrics {
  totalFeatures: number; totalBugs: number; backlogItems: number; activePlans: number;
  completedTasks: number; planningApprovalRate: number; averageTaskLeadHours: number;
  aiJobSuccessRate: number; averageAIJobSeconds: number; reworkCount: number;
}
export interface ObservabilityOverview {
  generatedAt: string; rangeDays: number; metrics: ObservabilityMetrics;
  featureFeasibility: DistributionItem[]; featureStatus: DistributionItem[];
  bugSeverity: DistributionItem[]; bugStatus: DistributionItem[];
  deliveryTargets: DistributionItem[]; agentWorkload: DistributionItem[];
  trend: TrendPoint[]; recentEvents: ObservationEvent[];
}

export interface AIUsage {
  inputTokens?: number; cachedInputTokens?: number; cacheWriteInputTokens?: number;
  outputTokens?: number; reasoningTokens?: number; totalTokens?: number; costUsd?: number;
  confidence: 'exact' | 'partial' | 'unavailable';
}
export interface AISessionRun {
  id: string; correlationId: string; projectId?: string; projectName?: string; entityType?: string; entityId?: string;
  entityKey?: string; title?: string; agent?: string; stage?: string; provider: string; model: string;
  effort?: string; status: 'queued' | 'running' | 'completed' | 'failed' | 'interrupted'; startedAt: string;
  completedAt?: string; durationMs: number; resumed: boolean; threadId?: string; error?: string;
  usage: AIUsage; events: ObservationEvent[];
}
export interface AISessionMetrics {
  activeRuns: number; completedRuns: number; successRate: number; medianDurationMs: number;
  trackedTokens: number; usageTrackedRuns: number;
}
export interface AIModelComparison {
  provider: string; model: string; runs: number; successRate: number; medianDurationMs: number;
  trackedTokens: number; usageTrackedRuns: number;
}
export interface AIProviderUsage {
  provider: string; primaryModel: string; status: 'active' | 'attention' | 'unavailable';
  confidence: 'exact' | 'partial' | 'unavailable'; runs: number; activeRuns: number; completedRuns: number;
  failedRuns: number; successRate: number; medianDurationMs: number; trackedTokens: number; usageTrackedRuns: number;
  costUsd?: number; localPressurePercent: number; lastUsedAt?: string; lastError?: string;
}
export interface AISessionOverview {
  generatedAt: string; rangeDays: number; metrics: AISessionMetrics; runs: AISessionRun[];
  comparisons: AIModelComparison[]; providerUsage?: AIProviderUsage[];
}
