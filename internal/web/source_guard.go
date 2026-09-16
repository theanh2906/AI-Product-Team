package web

import (
	"context"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/sourceguard"
)

func (s *server) beginTaskSourceGuard(ctx context.Context, project project, taskID, traceID, role string) (*sourceguard.Guard, error) {
	guard, err := sourceguard.Begin(ctx, project.executionPath(), project.additionalExecutionPaths(), taskID, role)
	if err != nil {
		return nil, fmt.Errorf("source recovery snapshot failed: %w", err)
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "safety", Message: "Source recovery snapshot created"})
	if s.observability != nil {
		s.observability.Record(observability.Event{
			Category: "safety", Name: "source_guard.snapshot_created", Message: "Source recovery snapshot created",
			CorrelationID: traceID, ProjectID: project.ID, EntityType: "task", EntityID: taskID,
			Agent: role, Stage: "safety", Outcome: "created",
		})
	}
	return guard, nil
}

func (s *server) finishTaskSourceGuard(ctx context.Context, guard *sourceguard.Guard, project project, taskID, traceID, role string) error {
	report, err := guard.VerifyAndRestore(ctx)
	if err == nil {
		agentstream.Emit(ctx, agentstream.Event{Kind: "safety", Message: "Source safety guard passed"})
		return nil
	}
	agentstream.Emit(ctx, agentstream.Event{Level: "error", Kind: "safety", Message: "Source safety guard restored suspicious rewrite", Detail: err.Error()})
	if s.observability != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "safety", Name: "source_guard.restored",
			Message: "Source safety guard restored suspicious source rewrite", CorrelationID: traceID,
			ProjectID: project.ID, EntityType: "task", EntityID: taskID, Agent: role, Stage: "safety",
			Outcome: "restored", Attributes: map[string]any{"recoveryDir": report.RecoveryDir, "restored": report.Restored},
		})
	}
	return fmt.Errorf("source safety guard blocked this task: %w", err)
}

func combineAgentAndGuardError(agentErr, guardErr error) error {
	if agentErr == nil {
		return guardErr
	}
	if guardErr == nil {
		return agentErr
	}
	return fmt.Errorf("%v; %w", agentErr, guardErr)
}
