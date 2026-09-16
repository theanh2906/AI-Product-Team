package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestSQLiteBoardRepositoryImplementsDurableCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteBoardRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	board := kanban.Board{ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce", Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, CreatedAt: now, UpdatedAt: now}
	ctx := context.Background()
	if err := repository.Create(ctx, board); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, board); !errors.Is(err, kanban.ErrConflict) {
		t.Fatalf("expected conflict on duplicate id, got %v", err)
	}

	reopenedDB, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedDB.Close()
	reopened, err := NewSQLiteBoardRepository(reopenedDB)
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
	if err := reopened.Update(ctx, board); !errors.Is(err, kanban.ErrNotFound) {
		t.Fatalf("expected update of missing board to fail, got %v", err)
	}
	if err := reopened.Delete(ctx, "missing"); !errors.Is(err, kanban.ErrNotFound) {
		t.Fatalf("expected delete of missing board to fail, got %v", err)
	}
}

func TestNewSQLiteBoardRepositoryRejectsNilHandle(t *testing.T) {
	if _, err := NewSQLiteBoardRepository(nil); err == nil {
		t.Fatal("expected error for nil database handle")
	}
}
