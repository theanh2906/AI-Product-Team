package design

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

type Designer interface {
	Design(context.Context, Request) (Result, error)
}
