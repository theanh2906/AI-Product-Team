package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/deploy"
	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func seedBundle(t *testing.T, bundle RepositoryBundle) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := bundle.Boards.Create(ctx, kanban.Board{ID: "board-1", ProjectID: "project-1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed board: %v", err)
	}
	if err := bundle.Insights.Upsert(ctx, insights.Record{ID: "insight-1", ProjectID: "project-1", Kind: insights.KindFeatureRadar, UpdatedAt: now}); err != nil {
		t.Fatalf("seed insight: %v", err)
	}
	if err := bundle.Events.Append(observability.Event{ID: "event-1", Timestamp: now, Category: "ai", Name: "test"}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := bundle.Notifications.Create(ctx, notifications.Notification{ID: "notification-1", Level: notifications.LevelInfo, Title: "t", Message: "m", CreatedAt: now}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	if err := bundle.BuildProfiles.Upsert(ctx, buildverify.Profile{ProjectID: "project-1", Status: "ready"}); err != nil {
		t.Fatalf("seed build profile: %v", err)
	}
	if err := bundle.DeployProfiles.Upsert(ctx, deploy.Profile{ProjectID: "project-1", Status: deploy.StatusReady}); err != nil {
		t.Fatalf("seed deploy profile: %v", err)
	}
	if err := bundle.GitDeliveries.Upsert(ctx, gitdelivery.Delivery{ID: "delivery-1", ProjectID: "project-1", StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed git delivery: %v", err)
	}
}

func assertMigratedBundle(t *testing.T, bundle RepositoryBundle) {
	t.Helper()
	ctx := context.Background()
	if _, err := bundle.Boards.Get(ctx, "board-1"); err != nil {
		t.Fatalf("expected migrated board: %v", err)
	}
	if _, err := bundle.Insights.Get(ctx, "project-1", insights.KindFeatureRadar); err != nil {
		t.Fatalf("expected migrated insight: %v", err)
	}
	events, err := bundle.Events.List(observability.Query{})
	if err != nil || len(events) != 1 {
		t.Fatalf("expected 1 migrated event, got %d (%v)", len(events), err)
	}
	items, err := bundle.Notifications.List(ctx, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 migrated notification, got %d (%v)", len(items), err)
	}
	if _, err := bundle.BuildProfiles.Get(ctx, "project-1"); err != nil {
		t.Fatalf("expected migrated build profile: %v", err)
	}
	if _, err := bundle.DeployProfiles.Get(ctx, "project-1"); err != nil {
		t.Fatalf("expected migrated deploy profile: %v", err)
	}
	if _, err := bundle.GitDeliveries.Get(ctx, "delivery-1"); err != nil {
		t.Fatalf("expected migrated git delivery: %v", err)
	}
}

func TestMigrateRepositoryBundleJSONToSQLite(t *testing.T) {
	source, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: t.TempDir()}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	seedBundle(t, source)

	target, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite, SQLite: SQLiteConfig{Path: filepath.Join(t.TempDir(), "productcrew.sqlite")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	counts, err := MigrateRepositoryBundle(context.Background(), source, target)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if counts != (MigrationCounts{Boards: 1, Insights: 1, Events: 1, Notifications: 1, BuildProfiles: 1, DeployProfiles: 1, GitDeliveries: 1}) {
		t.Fatalf("unexpected migration counts: %+v", counts)
	}
	assertMigratedBundle(t, target)
}

func TestMigrateRepositoryBundleSQLiteToJSON(t *testing.T) {
	source, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite, SQLite: SQLiteConfig{Path: filepath.Join(t.TempDir(), "productcrew.sqlite")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	seedBundle(t, source)

	target, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: t.TempDir()}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	counts, err := MigrateRepositoryBundle(context.Background(), source, target)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if counts.Total() != 7 {
		t.Fatalf("expected 7 total migrated records, got %d", counts.Total())
	}
	assertMigratedBundle(t, target)
}

func TestMigrateRepositoryBundleLeavesSourceUnchangedOnTargetFailure(t *testing.T) {
	source, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceLocalJSON, LocalJSON: LocalJSONConfig{Directory: t.TempDir()}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	seedBundle(t, source)

	target, err := NewRepositoryBundle(DatasourceConfig{Kind: DatasourceSQLite, SQLite: SQLiteConfig{Path: filepath.Join(t.TempDir(), "productcrew.sqlite")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Pre-create a conflicting board in target so migrating the same board ID
	// fails partway through, simulating a mid-migration failure.
	if err := target.Boards.Create(context.Background(), kanban.Board{ID: "board-1", ProjectID: "project-1"}); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateRepositoryBundle(context.Background(), source, target); err == nil {
		t.Fatal("expected migration failure on conflicting board")
	}
	target.Close()

	// Source must be unchanged and still fully readable after the failed
	// migration attempt.
	assertMigratedBundle(t, source)
}
