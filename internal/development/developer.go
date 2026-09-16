package development

import (
	"context"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

type Request struct {
	ProjectPath       string
	AdditionalPaths   []string
	Plan              kanban.Plan
	Task              kanban.Task
	Documents         []kanban.Document
	DependencyReports []kanban.TaskExecution
}

type Result struct {
	ThreadID       string
	Summary        string
	ChangedFiles   []string
	Verification   []string
	RemainingRisks []string
}

type BuildRepairRequest struct {
	Request        Request
	PreviousResult Result
	Verification   kanban.BuildVerification
	BuildLog       string
	Attempt        int
	MaxAttempts    int
}

type GitRepairRequest struct {
	Request        Request
	PreviousResult Result
	DeliveryID     string
	Step           string
	GitError       string
	AllowedFiles   []string
	Attempt        int
}

type Developer interface {
	Implement(context.Context, Request) (Result, error)
	RepairBuild(context.Context, BuildRepairRequest) (Result, error)
	RepairGitDelivery(context.Context, GitRepairRequest) (Result, error)
}
