package observability

import "time"

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Event struct {
	ID            string         `json:"id"`
	Timestamp     time.Time      `json:"timestamp"`
	Level         Level          `json:"level"`
	Category      string         `json:"category"`
	Name          string         `json:"name"`
	Message       string         `json:"message"`
	CorrelationID string         `json:"correlationId,omitempty"`
	ProjectID     string         `json:"projectId,omitempty"`
	EntityType    string         `json:"entityType,omitempty"`
	EntityID      string         `json:"entityId,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Stage         string         `json:"stage,omitempty"`
	Outcome       string         `json:"outcome,omitempty"`
	DurationMS    int64          `json:"durationMs,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
}

type Query struct {
	ProjectID     string
	CorrelationID string
	Level         Level
	Category      string
	Name          string
	Since         time.Time
	Limit         int
}

type Repository interface {
	Append(Event) error
	List(Query) ([]Event, error)
	Path() string
}

type Metrics struct {
	TotalFeatures        int     `json:"totalFeatures"`
	TotalBugs            int     `json:"totalBugs"`
	BacklogItems         int     `json:"backlogItems"`
	ActivePlans          int     `json:"activePlans"`
	CompletedTasks       int     `json:"completedTasks"`
	PlanningApprovalRate float64 `json:"planningApprovalRate"`
	AverageTaskLeadHours float64 `json:"averageTaskLeadHours"`
	AIJobSuccessRate     float64 `json:"aiJobSuccessRate"`
	AverageAIJobSeconds  float64 `json:"averageAIJobSeconds"`
	ReworkCount          int     `json:"reworkCount"`
}

type DistributionItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value int    `json:"value"`
	Color string `json:"color,omitempty"`
}

type TrendPoint struct {
	Date     string `json:"date"`
	Features int    `json:"features"`
	Bugs     int    `json:"bugs"`
	Done     int    `json:"done"`
}

type Overview struct {
	GeneratedAt        time.Time          `json:"generatedAt"`
	RangeDays          int                `json:"rangeDays"`
	Metrics            Metrics            `json:"metrics"`
	FeatureFeasibility []DistributionItem `json:"featureFeasibility"`
	FeatureStatus      []DistributionItem `json:"featureStatus"`
	BugSeverity        []DistributionItem `json:"bugSeverity"`
	BugStatus          []DistributionItem `json:"bugStatus"`
	DeliveryTargets    []DistributionItem `json:"deliveryTargets"`
	AgentWorkload      []DistributionItem `json:"agentWorkload"`
	Trend              []TrendPoint       `json:"trend"`
	RecentEvents       []Event            `json:"recentEvents"`
}

// AISessionOverview is a read model built from durable AI lifecycle events.
// Token fields remain nil when a provider did not report authoritative usage.
type AISessionOverview struct {
	GeneratedAt   time.Time           `json:"generatedAt"`
	RangeDays     int                 `json:"rangeDays"`
	Metrics       AISessionMetrics    `json:"metrics"`
	Runs          []AISessionRun      `json:"runs"`
	Comparisons   []AIModelComparison `json:"comparisons"`
	ProviderUsage []AIProviderUsage   `json:"providerUsage"`
}

type AISessionMetrics struct {
	ActiveRuns       int     `json:"activeRuns"`
	CompletedRuns    int     `json:"completedRuns"`
	SuccessRate      float64 `json:"successRate"`
	MedianDurationMS int64   `json:"medianDurationMs"`
	TrackedTokens    int64   `json:"trackedTokens"`
	UsageTrackedRuns int     `json:"usageTrackedRuns"`
}

type AIUsage struct {
	InputTokens           *int64   `json:"inputTokens,omitempty"`
	CachedInputTokens     *int64   `json:"cachedInputTokens,omitempty"`
	CacheWriteInputTokens *int64   `json:"cacheWriteInputTokens,omitempty"`
	OutputTokens          *int64   `json:"outputTokens,omitempty"`
	ReasoningTokens       *int64   `json:"reasoningTokens,omitempty"`
	TotalTokens           *int64   `json:"totalTokens,omitempty"`
	CostUSD               *float64 `json:"costUsd,omitempty"`
	Confidence            string   `json:"confidence"`
}

type AISessionRun struct {
	ID            string     `json:"id"`
	CorrelationID string     `json:"correlationId"`
	ProjectID     string     `json:"projectId,omitempty"`
	ProjectName   string     `json:"projectName,omitempty"`
	EntityType    string     `json:"entityType,omitempty"`
	EntityID      string     `json:"entityId,omitempty"`
	EntityKey     string     `json:"entityKey,omitempty"`
	Title         string     `json:"title,omitempty"`
	Agent         string     `json:"agent,omitempty"`
	Stage         string     `json:"stage,omitempty"`
	Provider      string     `json:"provider,omitempty"`
	Model         string     `json:"model,omitempty"`
	Effort        string     `json:"effort,omitempty"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"startedAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	DurationMS    int64      `json:"durationMs"`
	Resumed       bool       `json:"resumed"`
	ThreadID      string     `json:"threadId,omitempty"`
	Error         string     `json:"error,omitempty"`
	Usage         AIUsage    `json:"usage"`
	Events        []Event    `json:"events"`
}

type AIModelComparison struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Runs             int     `json:"runs"`
	SuccessRate      float64 `json:"successRate"`
	MedianDurationMS int64   `json:"medianDurationMs"`
	TrackedTokens    int64   `json:"trackedTokens"`
	UsageTrackedRuns int     `json:"usageTrackedRuns"`
}

type AIProviderUsage struct {
	Provider             string     `json:"provider"`
	PrimaryModel         string     `json:"primaryModel"`
	Status               string     `json:"status"`
	Confidence           string     `json:"confidence"`
	Runs                 int        `json:"runs"`
	ActiveRuns           int        `json:"activeRuns"`
	CompletedRuns        int        `json:"completedRuns"`
	FailedRuns           int        `json:"failedRuns"`
	SuccessRate          float64    `json:"successRate"`
	MedianDurationMS     int64      `json:"medianDurationMs"`
	TrackedTokens        int64      `json:"trackedTokens"`
	UsageTrackedRuns     int        `json:"usageTrackedRuns"`
	CostUSD              float64    `json:"costUsd,omitempty"`
	LocalPressurePercent float64    `json:"localPressurePercent"`
	LastUsedAt           *time.Time `json:"lastUsedAt,omitempty"`
	LastError            string     `json:"lastError,omitempty"`
}
