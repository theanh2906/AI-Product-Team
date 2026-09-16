package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
)

func TestSQLiteBuildProfileRepositoryImplementsDurableCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteBuildProfileRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repository.Get(ctx, "project-1"); !errors.Is(err, buildverify.ErrNotFound) {
		t.Fatalf("expected not found for missing profile, got %v", err)
	}
	profile := buildverify.Profile{ProjectID: "project-1", Status: "detected", DetectedAt: time.Now().UTC()}
	if err := repository.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	profile.Status = "verified"
	if err := repository.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, "project-1")
	if err != nil || stored.Status != "verified" {
		t.Fatalf("unexpected stored profile: %+v %v", stored, err)
	}
}
