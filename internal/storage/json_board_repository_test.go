package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestJSONBoardRepositoryImplementsDurableCRUD(t *testing.T) {
	directory := t.TempDir()
	repository, err := NewJSONBoardRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	board := kanban.Board{ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce", Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, CreatedAt: now, UpdatedAt: now}
	ctx := context.Background()
	if err := repository.Create(ctx, board); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewJSONBoardRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.FindByProject(ctx, "project-1")
	if err != nil || stored.ID != board.ID {
		t.Fatalf("board did not survive repository recreation: %+v %v", stored, err)
	}
	stored.ProjectName = "Commerce Web"
	if err := reopened.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	items, err := reopened.List(ctx)
	if err != nil || len(items) != 1 || items[0].ProjectName != "Commerce Web" {
		t.Fatalf("unexpected list result: %+v %v", items, err)
	}
	if err := reopened.Delete(ctx, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, stored.ID); !errors.Is(err, kanban.ErrNotFound) {
		t.Fatalf("expected deleted board to be missing, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "boards.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Version != boardFileVersion {
		t.Fatalf("unexpected JSON envelope: %s %v", data, err)
	}
}

func TestJSONBoardRepositoryRecoversInterruptedReplacement(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := NewJSONBoardRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce",
		Plans:     []kanban.Plan{{ID: "plan-1", Request: kanban.WorkRequest{ID: "request-1"}}},
		Tasks:     []kanban.Task{{ID: "task-1", PlanID: "plan-1"}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := repository.Create(ctx, board); err != nil {
		t.Fatal(err)
	}
	boardPath := filepath.Join(directory, "boards.json")
	backupPath := boardPath + ".bak"
	if err := os.Rename(boardPath, backupPath); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewJSONBoardRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.FindByProject(ctx, board.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != board.ID {
		t.Fatalf("expected recovered board %q, got %q", board.ID, loaded.ID)
	}
	if loaded.Plans[0].Documents == nil || loaded.Plans[0].Reviews == nil ||
		loaded.Plans[0].Request.AcceptanceCriteria == nil || loaded.Tasks[0].AcceptanceCriteria == nil ||
		loaded.Tasks[0].DependencyIDs == nil || loaded.Tasks[0].DocumentIDs == nil {
		t.Fatalf("expected all persisted collections to normalize to empty arrays: %+v", loaded)
	}
	if _, err := os.Stat(boardPath); err != nil {
		t.Fatalf("expected backup to be restored: %v", err)
	}
}
