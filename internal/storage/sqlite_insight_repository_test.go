package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
)

func TestSQLiteInsightRepositoryImplementsDurableCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteInsightRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	record := insights.Record{ID: "insight-1", ProjectID: "project-1", Kind: insights.KindBugScan, Payload: []byte(`{"count":1}`), UpdatedAt: time.Now().UTC()}
	if err := repository.Upsert(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Payload = []byte(`{"count":2}`)
	if err := repository.Upsert(ctx, record); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, "project-1", insights.KindBugScan)
	if err != nil || string(stored.Payload) != `{"count":2}` {
		t.Fatalf("unexpected stored record: %+v %v", stored, err)
	}
	list, err := repository.ListProject(ctx, "project-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("unexpected list result: %+v %v", list, err)
	}
	if err := repository.Delete(ctx, "project-1", insights.KindBugScan); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, "project-1", insights.KindBugScan); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := repository.Delete(ctx, "project-1", insights.KindBugScan); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("expected not found deleting again, got %v", err)
	}

	if err := repository.Upsert(ctx, insights.Record{ID: "insight-2", ProjectID: "project-2", Kind: insights.KindFeatureRadar, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	removed, err := repository.DeleteProject(ctx, "project-2")
	if err != nil || removed != 1 {
		t.Fatalf("unexpected DeleteProject result: %d %v", removed, err)
	}
}
