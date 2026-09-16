package web

import (
	"errors"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
)

func TestTaskSessionLogBuffersAndPublishesTaskScopedEvents(t *testing.T) {
	manager := newTaskSessionLogManager()
	updates, unsubscribe := manager.subscribe("project-1", "task-1")
	defer unsubscribe()
	reporter := manager.start("project-1", "task-1", "codex", "developer")
	reporter.Report(agentstream.Event{Kind: "command", Message: "Command started", Detail: "go test ./..."})

	snapshot := manager.snapshot("project-1", "task-1")
	if snapshot.Status != "running" || len(snapshot.Entries) != 2 {
		t.Fatalf("unexpected running snapshot: %+v", snapshot)
	}
	if snapshot.Entries[1].Detail != "go test ./..." {
		t.Fatalf("expected command detail, got %+v", snapshot.Entries[1])
	}
	select {
	case <-updates:
	default:
		t.Fatal("expected a published session update")
	}

	manager.finish("project-1", "task-1", errors.New("runtime disconnected"))
	finished := manager.snapshot("project-1", "task-1")
	if finished.Status != "failed" || finished.Entries[len(finished.Entries)-1].Level != "error" {
		t.Fatalf("unexpected failed snapshot: %+v", finished)
	}
}

func TestTaskSessionLogIdentifiesPlanningSessions(t *testing.T) {
	manager := newTaskSessionLogManager()
	reporter := manager.startPlan("project-1", "plan-1", "claude-code")
	reporter.Report(agentstream.Event{Kind: "search", Message: "Researching implementation options"})

	snapshot := manager.snapshotPlan("project-1", "plan-1")
	if snapshot.PlanID != "plan-1" || snapshot.TaskID != "" {
		t.Fatalf("unexpected planning session identity: %+v", snapshot)
	}
	if snapshot.Agent != "team-lead" || snapshot.Provider != "claude-code" {
		t.Fatalf("unexpected planning session metadata: %+v", snapshot)
	}
}
