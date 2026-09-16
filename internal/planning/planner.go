package planning

import (
	"context"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

type Request struct {
	ProjectPath     string
	AdditionalPaths []string
	Plan            kanban.Plan
}

type Result struct {
	ThreadID  string
	Draft     kanban.PlanDraft
	Preflight *kanban.PlanPreflight
}

type Planner interface {
	Generate(context.Context, Request) (Result, error)
}
