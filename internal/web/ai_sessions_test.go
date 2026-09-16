package web

import (
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func TestBuildAISessionOverviewGroupsLifecycleAndUsage(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	started := now.Add(-2 * time.Minute)
	events := []observability.Event{
		{ID: "usage", Timestamp: now.Add(-time.Minute), Category: "ai", Name: "ai.job.usage", CorrelationID: "run-1", ProjectID: "project-1", EntityType: "task", EntityID: "task-1", Agent: "developer", Attributes: map[string]any{"inputTokens": float64(120), "outputTokens": float64(30), "totalTokens": float64(150), "usageConfidence": "exact"}},
		{ID: "done", Timestamp: now.Add(-time.Minute), Category: "ai", Name: "ai.job.completed", CorrelationID: "run-1", ProjectID: "project-1", EntityType: "task", EntityID: "task-1", Agent: "developer", DurationMS: 60000},
		{ID: "start", Timestamp: started, Category: "ai", Name: "ai.job.started", CorrelationID: "run-1", ProjectID: "project-1", EntityType: "task", EntityID: "task-1", Agent: "developer", Stage: "implementation", Attributes: map[string]any{"provider": "codex", "model": "gpt-test", "reasoningEffort": "high"}},
	}
	boards := []kanban.Board{{ProjectID: "project-1", ProjectName: "Example", Tasks: []kanban.Task{{ID: "task-1", Key: "DEV-001", Title: "Build monitor"}}}}

	result := buildAISessionOverview(events, boards, 30, now)
	if len(result.Runs) != 1 {
		t.Fatalf("expected one run, got %d", len(result.Runs))
	}
	run := result.Runs[0]
	if run.Status != "completed" || run.EntityKey != "DEV-001" || run.Provider != "codex" || run.Model != "gpt-test" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.Usage.TotalTokens == nil || *run.Usage.TotalTokens != 150 || run.Usage.Confidence != "exact" {
		t.Fatalf("unexpected usage: %+v", run.Usage)
	}
	if result.Metrics.SuccessRate != 100 || result.Metrics.TrackedTokens != 150 || len(result.Comparisons) != 1 {
		t.Fatalf("unexpected metrics: %+v", result.Metrics)
	}
	provider := findProviderUsage(result.ProviderUsage, "codex")
	if provider == nil {
		t.Fatalf("expected codex provider usage summary, got %+v", result.ProviderUsage)
	}
	if provider.Provider != "codex" || provider.PrimaryModel != "gpt-test" || provider.Status != "unavailable" || provider.Confidence != "exact" {
		t.Fatalf("unexpected provider usage summary: %+v", provider)
	}
	if provider.TrackedTokens != 150 || provider.UsageTrackedRuns != 1 || provider.LocalPressurePercent != 100 {
		t.Fatalf("unexpected provider usage values: %+v", provider)
	}
}

func TestBuildAISessionOverviewKeepsUnreportedUsageUnavailable(t *testing.T) {
	now := time.Now().UTC()
	result := buildAISessionOverview([]observability.Event{{ID: "start", Timestamp: now.Add(-time.Second), Category: "ai", Name: "ai.job.started", CorrelationID: "run-2", Agent: "qa"}}, nil, 7, now)
	if len(result.Runs) != 1 || result.Runs[0].Usage.Confidence != "unavailable" || result.Metrics.UsageTrackedRuns != 0 {
		t.Fatalf("unexpected overview: %+v", result)
	}
	provider := findProviderUsage(result.ProviderUsage, "unknown")
	if provider == nil || provider.Confidence != "unavailable" {
		t.Fatalf("unexpected provider usage summary: %+v", result.ProviderUsage)
	}
}

func TestBuildAISessionOverviewMarksProviderAttentionForFailures(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	events := []observability.Event{
		{ID: "failed", Timestamp: now.Add(-time.Minute), Category: "ai", Name: "ai.job.failed", CorrelationID: "run-3", ProjectID: "project-1", EntityType: "task", EntityID: "task-1", Agent: "qa", DurationMS: 120000, Attributes: map[string]any{"error": "quota exhausted"}},
		{ID: "start", Timestamp: now.Add(-3 * time.Minute), Category: "ai", Name: "ai.job.started", CorrelationID: "run-3", ProjectID: "project-1", EntityType: "task", EntityID: "task-1", Agent: "qa", Attributes: map[string]any{"provider": "github-copilot", "model": "claude-opus-5"}},
	}

	result := buildAISessionOverview(events, nil, 30, now)
	provider := findProviderUsage(result.ProviderUsage, "github-copilot")
	if provider == nil {
		t.Fatalf("expected GitHub Copilot provider usage summary, got %+v", result.ProviderUsage)
	}
	if provider.Status != "attention" || provider.FailedRuns != 1 || provider.LastError != "quota exhausted" {
		t.Fatalf("unexpected failed provider summary: %+v", provider)
	}
}

func findProviderUsage(providers []observability.AIProviderUsage, provider string) *observability.AIProviderUsage {
	for index := range providers {
		if providers[index].Provider == provider {
			return &providers[index]
		}
	}
	return nil
}
