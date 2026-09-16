package quality

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
	ExistingBugs      []ExistingBug
}

type ExistingBug struct {
	BacklogID string `json:"backlogId"`
	Key       string `json:"key"`
	Title     string `json:"title"`
}

type Bug struct {
	ExistingBacklogID string   `json:"existingBacklogId"`
	Severity          string   `json:"severity"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Evidence          string   `json:"evidence"`
	Steps             []string `json:"steps"`
	Expected          string   `json:"expected"`
	Actual            string   `json:"actual"`
	AffectedFiles     []string `json:"affectedFiles"`
}

type Outcome string

const (
	OutcomePassed  Outcome = "passed"
	OutcomeFailed  Outcome = "failed"
	OutcomeBlocked Outcome = "blocked"
)

type Result struct {
	ThreadID     string
	Outcome      Outcome
	Summary      string
	ChangedFiles []string
	Verification []string
	Bugs         []Bug
}

type QA interface {
	Verify(context.Context, Request) (Result, error)
}
