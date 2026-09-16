package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func TestSQLiteEventRepositoryAppendAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteEventRepository(db, path)
	if err != nil {
		t.Fatal(err)
	}
	if repository.Path() != path {
		t.Fatalf("expected path %q, got %q", path, repository.Path())
	}
	now := time.Now().UTC()
	if err := repository.Append(observability.Event{ID: "e-1", Timestamp: now.Add(-time.Minute), ProjectID: "project-1", Category: "ai", Name: "ai.job.started", Level: observability.LevelInfo}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Append(observability.Event{ID: "e-2", Timestamp: now, ProjectID: "project-1", Category: "ai", Name: "ai.job.completed", Level: observability.LevelError}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Append(observability.Event{ID: "e-3", Timestamp: now, ProjectID: "project-2", Category: "ai", Name: "ai.job.completed", Level: observability.LevelInfo}); err != nil {
		t.Fatal(err)
	}

	all, err := repository.List(observability.Query{})
	if err != nil || len(all) != 3 {
		t.Fatalf("unexpected list result: %+v %v", all, err)
	}
	if all[0].ID != "e-2" && all[0].ID != "e-3" {
		t.Fatalf("expected newest events first, got %+v", all)
	}

	filtered, err := repository.List(observability.Query{ProjectID: "project-1", Level: observability.LevelError})
	if err != nil || len(filtered) != 1 || filtered[0].ID != "e-2" {
		t.Fatalf("unexpected filtered result: %+v %v", filtered, err)
	}

	since, err := repository.List(observability.Query{Since: now})
	if err != nil || len(since) != 2 {
		t.Fatalf("unexpected since-filtered result: %+v %v", since, err)
	}
}
