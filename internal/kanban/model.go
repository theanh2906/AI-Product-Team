package kanban

import (
	"fmt"
	"strings"
	"time"
)

type AgentRole string

const (
	RoleDesigner  AgentRole = "designer"
	RoleDeveloper AgentRole = "developer"
	RoleQA        AgentRole = "qa"
)

func (r AgentRole) Valid() bool {
	return r == RoleDesigner || r == RoleDeveloper || r == RoleQA
}

func (r AgentRole) Label() string {
	switch r {
	case RoleDesigner:
		return "Designer"
	case RoleDeveloper:
		return "Developer"
	case RoleQA:
		return "QA"
	default:
		return "Unknown"
	}
}

func (r AgentRole) QueueColumn() Column {
	switch r {
	case RoleDesigner:
		return ColumnDesigner
	case RoleDeveloper:
		return ColumnDeveloper
	case RoleQA:
		return ColumnQA
	default:
		return ColumnPlanning
	}
}

type Column string

const (
	ColumnBacklog   Column = "backlog"
	ColumnPlanning  Column = "planning"
	ColumnDesigner  Column = "designer"
	ColumnDeveloper Column = "developer"
	ColumnQA        Column = "qa"
	ColumnDone      Column = "done"
)

func (c Column) Valid() bool {
	return c == ColumnBacklog || c == ColumnPlanning || c == ColumnDesigner || c == ColumnDeveloper || c == ColumnQA || c == ColumnDone
}

type BacklogItemType string

const (
	BacklogFeature BacklogItemType = "feature"
	BacklogBug     BacklogItemType = "bug"
	BacklogTodo    BacklogItemType = "todo"
)

func (t BacklogItemType) Valid() bool {
	return t == BacklogFeature || t == BacklogBug || t == BacklogTodo
}

type BacklogStatus string

const (
	BacklogOpen        BacklogStatus = "backlog"
	BacklogPlanning    BacklogStatus = "planning"
	BacklogDone        BacklogStatus = "done"
	BacklogNotFeasible BacklogStatus = "not_feasible"
)

type TaskStatus string

const (
	TaskPlanned      TaskStatus = "planned"
	TaskQueued       TaskStatus = "queued"
	TaskInProgress   TaskStatus = "in_progress"
	TaskVerifying    TaskStatus = "verifying"
	TaskDesignReview TaskStatus = "design_review"
	TaskBlocked      TaskStatus = "blocked"
	TaskCompleted    TaskStatus = "completed"
)

type PlanningStatus string

const (
	PlanningAnalyzing        PlanningStatus = "analyzing"
	PlanningAwaitingApproval PlanningStatus = "awaiting_approval"
	PlanningApproved         PlanningStatus = "approved"
	PlanningChangesRequested PlanningStatus = "changes_requested"
	PlanningFailed           PlanningStatus = "failed"
	PlanningCompleted        PlanningStatus = "completed"
	PlanningNotFeasible      PlanningStatus = "not_feasible"
)

type PlanSequenceStatus string

const (
	PlanSequenceScheduled PlanSequenceStatus = "scheduled"
	PlanSequenceRunning   PlanSequenceStatus = "running"
	PlanSequenceBlocked   PlanSequenceStatus = "blocked"
	PlanSequenceCompleted PlanSequenceStatus = "completed"
)

type QueueControlStatus string

const (
	QueueControlStopping QueueControlStatus = "stopping"
	QueueControlPaused   QueueControlStatus = "paused"
)

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

func (p Priority) Valid() bool {
	return p == PriorityCritical || p == PriorityHigh || p == PriorityMedium || p == PriorityLow
}

type WorkRequest struct {
	ID                 string              `json:"id"`
	ArtifactPath       string              `json:"artifactPath,omitempty"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	WorkType           string              `json:"workType"`
	DeliveryTarget     string              `json:"deliveryTarget"`
	RequiresUI         bool                `json:"requiresUI"`
	AcceptanceCriteria []string            `json:"acceptanceCriteria"`
	Attachments        []RequestAttachment `json:"attachments"`
	Intake             *IntakeSubmission   `json:"intake,omitempty"`
	CreatedAt          time.Time           `json:"createdAt"`
}

type RequestAttachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"originalName"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type IntakeQuestionnaire struct {
	SchemaVersion int              `json:"schemaVersion"`
	Heading       string           `json:"heading"`
	Summary       string           `json:"summary"`
	WorkType      string           `json:"workType"`
	Source        string           `json:"source"`
	Warning       string           `json:"warning,omitempty"`
	Questions     []IntakeQuestion `json:"questions"`
}

type IntakeQuestion struct {
	ID            string         `json:"id"`
	Label         string         `json:"label"`
	HelpText      string         `json:"helpText"`
	Type          string         `json:"type"`
	Binding       string         `json:"binding"`
	Required      bool           `json:"required"`
	Options       []IntakeOption `json:"options"`
	DefaultValues []string       `json:"defaultValues"`
	Layout        IntakeLayout   `json:"layout"`
}

type IntakeOption struct {
	Value               string `json:"value"`
	Label               string `json:"label"`
	Description         string `json:"description"`
	AcceptanceCriterion string `json:"acceptanceCriterion,omitempty"`
}

type IntakeLayout struct {
	Span int `json:"span"`
}

type IntakeSubmission struct {
	Questionnaire IntakeQuestionnaire `json:"questionnaire"`
	Answers       map[string][]string `json:"answers"`
}

type Document struct {
	ID        string      `json:"id"`
	PlanID    string      `json:"planId"`
	Title     string      `json:"title"`
	Kind      string      `json:"kind"`
	Audience  []AgentRole `json:"audience"`
	Content   string      `json:"content"`
	Version   int         `json:"version"`
	CreatedAt time.Time   `json:"createdAt"`
}

type PlanReview struct {
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason,omitempty"`
	Reviewer  string    `json:"reviewer"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
}

type Plan struct {
	ID        string         `json:"id"`
	Request   WorkRequest    `json:"request"`
	Status    PlanningStatus `json:"status"`
	Revision  int            `json:"revision"`
	Summary   string         `json:"summary,omitempty"`
	Error     string         `json:"error,omitempty"`
	ThreadID  string         `json:"threadId,omitempty"`
	Preflight *PlanPreflight `json:"preflight,omitempty"`
	Documents []Document     `json:"documents"`
	Reviews   []PlanReview   `json:"reviews"`
	Sequence  *PlanSequence  `json:"sequence,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type PlanPreflight struct {
	Status        string     `json:"status"`
	Summary       string     `json:"summary"`
	Evidence      []string   `json:"evidence"`
	Verification  []string   `json:"verification"`
	RemainingWork []string   `json:"remainingWork,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type PlanSequence struct {
	Status        PlanSequenceStatus   `json:"status"`
	BlockedReason string               `json:"blockedReason,omitempty"`
	ScheduledAt   time.Time            `json:"scheduledAt"`
	RoleThreadIDs map[AgentRole]string `json:"roleThreadIds,omitempty"`
	StartedAt     *time.Time           `json:"startedAt,omitempty"`
	CompletedAt   *time.Time           `json:"completedAt,omitempty"`
}

type Task struct {
	ID                 string             `json:"id"`
	PlanID             string             `json:"planId"`
	Key                string             `json:"key"`
	Title              string             `json:"title"`
	Role               AgentRole          `json:"role"`
	Column             Column             `json:"column"`
	Status             TaskStatus         `json:"status"`
	Priority           Priority           `json:"priority"`
	Description        string             `json:"description"`
	AcceptanceCriteria []string           `json:"acceptanceCriteria"`
	DependencyIDs      []string           `json:"dependencyIds"`
	DocumentIDs        []string           `json:"documentIds"`
	BlockedReason      string             `json:"blockedReason,omitempty"`
	ThreadID           string             `json:"threadId,omitempty"`
	Execution          *TaskExecution     `json:"execution,omitempty"`
	RevisionHistory    []TaskRevision     `json:"revisionHistory,omitempty"`
	BuildVerification  *BuildVerification `json:"buildVerification,omitempty"`
	GitReference       *TaskGitReference  `json:"gitReference,omitempty"`
	SequenceOrder      int                `json:"sequenceOrder,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

// TaskGitReference is optional implementation evidence a user explicitly
// attaches to a task: a branch and/or commit SHA. It is never populated
// automatically from repository state.
type TaskGitReference struct {
	Branch    string    `json:"branch,omitempty"`
	CommitSHA string    `json:"commitSha,omitempty"`
	LinkedAt  time.Time `json:"linkedAt"`
	LinkedBy  string    `json:"linkedBy,omitempty"`
}

// TaskExecution is the durable delivery report produced by the assigned agent.
// It intentionally contains only user-facing results; detailed runtime events
// remain in the observability log.
type TaskExecution struct {
	Verdict           string             `json:"verdict,omitempty"`
	Summary           string             `json:"summary"`
	ChangedFiles      []string           `json:"changedFiles"`
	Verification      []string           `json:"verification"`
	RemainingRisks    []string           `json:"remainingRisks"`
	Findings          []TaskFinding      `json:"findings,omitempty"`
	Artifacts         []DesignArtifact   `json:"artifacts,omitempty"`
	BuildVerification *BuildVerification `json:"buildVerification,omitempty"`
	CompletedAt       time.Time          `json:"completedAt"`
}

type TaskRevision struct {
	Revision    int           `json:"revision"`
	Feedback    string        `json:"feedback,omitempty"`
	Reviewer    string        `json:"reviewer,omitempty"`
	RequestedAt time.Time     `json:"requestedAt"`
	ThreadID    string        `json:"threadId,omitempty"`
	Execution   TaskExecution `json:"execution"`
}

type BuildVerification struct {
	RunID       string     `json:"runId"`
	ActionID    string     `json:"actionId"`
	ActionLabel string     `json:"actionLabel"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	ExitCode    int        `json:"exitCode"`
	Reason      string     `json:"reason,omitempty"`
	LogPath     string     `json:"logPath,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	DurationMS  int64      `json:"durationMs"`
}

type DesignArtifact struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

type TaskFinding struct {
	Severity            string        `json:"severity"`
	Title               string        `json:"title"`
	Description         string        `json:"description"`
	Evidence            string        `json:"evidence"`
	Steps               []string      `json:"steps"`
	Expected            string        `json:"expected"`
	Actual              string        `json:"actual"`
	AffectedFiles       []string      `json:"affectedFiles"`
	LinkedBacklogID     string        `json:"linkedBacklogId,omitempty"`
	LinkedBacklogKey    string        `json:"linkedBacklogKey,omitempty"`
	LinkedBacklogTitle  string        `json:"linkedBacklogTitle,omitempty"`
	LinkedBacklogStatus BacklogStatus `json:"linkedBacklogStatus,omitempty"`
	LinkedBacklogPlanID string        `json:"linkedBacklogPlanId,omitempty"`
}

type BacklogItem struct {
	ID                 string              `json:"id"`
	RequestID          string              `json:"requestId"`
	ArtifactPath       string              `json:"artifactPath,omitempty"`
	Key                string              `json:"key"`
	Type               BacklogItemType     `json:"type"`
	Source             string              `json:"source"`
	SourceReference    string              `json:"sourceReference,omitempty"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	DeliveryTarget     string              `json:"deliveryTarget"`
	RequiresUI         bool                `json:"requiresUI"`
	AcceptanceCriteria []string            `json:"acceptanceCriteria"`
	Attachments        []RequestAttachment `json:"attachments"`
	Intake             *IntakeSubmission   `json:"intake,omitempty"`
	Feasibility        int                 `json:"feasibility,omitempty"`
	Severity           string              `json:"severity,omitempty"`
	Status             BacklogStatus       `json:"status"`
	PlanID             string              `json:"planId,omitempty"`
	RepeatReports      int                 `json:"repeatReports,omitempty"`
	LastReportedAt     *time.Time          `json:"lastReportedAt,omitempty"`
	WaitingQATaskIDs   []string            `json:"waitingQaTaskIds,omitempty"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
}

type Board struct {
	ID           string        `json:"id"`
	ProjectID    string        `json:"projectId"`
	ProjectName  string        `json:"projectName"`
	Plans        []Plan        `json:"plans"`
	Backlog      []BacklogItem `json:"backlog"`
	Tasks        []Task        `json:"tasks"`
	ActiveTaskID string        `json:"activeTaskId,omitempty"`
	QueueControl *QueueControl `json:"queueControl,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

type QueueControl struct {
	Status      QueueControlStatus `json:"status"`
	RequestedAt *time.Time         `json:"requestedAt,omitempty"`
	PausedAt    *time.Time         `json:"pausedAt,omitempty"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type BacklogDraft struct {
	RequestID          string
	ArtifactPath       string
	Type               BacklogItemType
	Source             string
	SourceReference    string
	Title              string
	Description        string
	DeliveryTarget     string
	RequiresUI         bool
	AcceptanceCriteria []string
	Attachments        []RequestAttachment
	Intake             *IntakeSubmission
	Feasibility        int
	Severity           string
}

type DraftDocument struct {
	Ref      string
	Title    string
	Kind     string
	Audience []AgentRole
	Content  string
}

type DraftTask struct {
	Ref                string
	Role               AgentRole
	Title              string
	Priority           Priority
	Description        string
	AcceptanceCriteria []string
	Dependencies       []string
	Documents          []string
}

type PlanDraft struct {
	Summary   string
	Documents []DraftDocument
	Tasks     []DraftTask
}

func FormatTaskTitle(role AgentRole, title string) string {
	title = strings.TrimSpace(title)
	for _, candidate := range []AgentRole{RoleDesigner, RoleDeveloper, RoleQA} {
		prefix := fmt.Sprintf("[%s]", candidate.Label())
		if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)) {
			title = strings.TrimSpace(title[len(prefix):])
			break
		}
	}
	if title == "" {
		title = "Untitled task"
	}
	return fmt.Sprintf("[%s] %s", role.Label(), title)
}
