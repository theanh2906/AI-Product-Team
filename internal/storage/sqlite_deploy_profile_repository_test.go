package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/deploy"
)

func TestSQLiteDeployProfileRepositoryImplementsDurableCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteDeployProfileRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repository.Get(ctx, "project-1"); !errors.Is(err, deploy.ErrNotFound) {
		t.Fatalf("expected not found for missing profile, got %v", err)
	}
	profile := deploy.Profile{ProjectID: "project-1", Status: deploy.StatusNotConfigured, DetectedAt: time.Now().UTC()}
	if err := repository.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	profile.Status = deploy.StatusReady
	profile.SelectedActionID = "deploy-abc123"
	if err := repository.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, "project-1")
	if err != nil || stored.Status != deploy.StatusReady || stored.SelectedActionID != "deploy-abc123" {
		t.Fatalf("unexpected stored profile: %+v %v", stored, err)
	}
	all, err := repository.ListAll(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("unexpected ListAll result: %+v %v", all, err)
	}
}
