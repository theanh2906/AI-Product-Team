package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestNewRepositoryBundleDefaultsToLocalJSON(t *testing.T) {
	directory := t.TempDir()
	bundle, err := NewRepositoryBundle(DatasourceConfig{}, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if bundle.Kind != DatasourceLocalJSON {
		t.Fatalf("expected local-json default, got %q", bundle.Kind)
	}
	assertBoardRepositoryWorks(t, bundle.Boards)
}

func TestNewRepositoryBundleLocalJSONUsesConfiguredDirectory(t *testing.T) {
	configured := t.TempDir()
	bundle, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: configured}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if err := bundle.Boards.Create(context.Background(), kanban.Board{ID: "board-1", ProjectID: "project-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(configured, "boards.json")); err != nil {
		t.Fatalf("expected boards.json in configured directory: %v", err)
	}
}

func TestNewRepositoryBundleSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	bundle, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite, SQLite: SQLiteConfig{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if bundle.Kind != DatasourceSQLite {
		t.Fatalf("expected sqlite kind, got %q", bundle.Kind)
	}
	assertBoardRepositoryWorks(t, bundle.Boards)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sqlite database file to exist: %v", err)
	}
}

func TestNewRepositoryBundleUnavailableLocalJSONTarget(t *testing.T) {
	// A regular file in place of the data directory makes MkdirAll fail,
	// simulating an unavailable local-json target.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	unavailable := filepath.Join(blocker, "data")
	if _, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: unavailable}}, ""); err == nil {
		t.Fatal("expected error for unavailable local-json target")
	}
}

func TestNewRepositoryBundleUnavailableSQLiteTarget(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	unavailable := filepath.Join(blocker, "productcrew.sqlite")
	if _, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite, SQLite: SQLiteConfig{Path: unavailable}}, ""); err == nil {
		t.Fatal("expected error for unavailable sqlite target")
	}
}

func TestNewRepositoryBundleRejectsUnknownKind(t *testing.T) {
	if _, err := NewRepositoryBundle(DatasourceConfig{Kind: "postgres"}, t.TempDir()); err == nil {
		t.Fatal("expected error for unsupported datasource kind")
	}
}

func TestNewRepositoryBundleRejectsEmptyLocalJSONDirectory(t *testing.T) {
	if _, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON}, ""); err == nil {
		t.Fatal("expected error when no directory and no fallback are available")
	}
}

func TestNewRepositoryBundleRejectsEmptySQLitePath(t *testing.T) {
	if _, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite}, t.TempDir()); err == nil {
		t.Fatal("expected error when sqlite path is empty")
	}
}

func assertBoardRepositoryWorks(t *testing.T, repository kanban.BoardRepository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	board := kanban.Board{ID: "board-x", ProjectID: "project-x", CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(ctx, board); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, board.ID)
	if err != nil || stored.ID != board.ID {
		t.Fatalf("unexpected board round-trip: %+v %v", stored, err)
	}
}
