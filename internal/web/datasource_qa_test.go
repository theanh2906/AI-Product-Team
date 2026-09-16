package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestQA_ExistingInstallWithoutDatasourceLoadsLocalJSON(t *testing.T) {
	directory := t.TempDir()
	now := time.Now().UTC()

	// Seed existing legacy local JSON data files without any storageDatasource in settings.json
	boardRepo, err := storage.NewJSONBoardRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := boardRepo.Create(context.Background(), kanban.Board{ID: "legacy-board-1", ProjectID: "p-1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	projects, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := projects.storageDatasourceSummary()
	if err != nil {
		t.Fatalf("expected summary to succeed: %v", err)
	}
	if summary.Kind != storage.DatasourceLocalJSON {
		t.Fatalf("expected local-json, got %q", summary.Kind)
	}
	if summary.Status != "ok" {
		t.Fatalf("expected status ok, got %q", summary.Status)
	}

	// Verify legacy board is readable
	bundle, err := storage.NewRepositoryBundle(storage.DatasourceConfig{}, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	board, err := bundle.Boards.Get(context.Background(), "legacy-board-1")
	if err != nil || board.ID != "legacy-board-1" {
		t.Fatalf("expected legacy board to be preserved: %+v %v", board, err)
	}
}

func TestQA_UnavailableActiveDatasourceDegradesSafelyWithoutSilentFallback(t *testing.T) {
	directory := t.TempDir()

	// Point settings.json to an unavailable SQLite database path
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	unavailablePath := filepath.Join(blocker, "unreachable.db")

	projects, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	// Directly persist the sqlite configuration as active
	stored, err := projects.readSettings()
	if err != nil {
		t.Fatal(err)
	}
	stored.StorageDatasource = storage.DatasourceConfig{
		Kind:   storage.DatasourceSQLite,
		SQLite: storage.SQLiteConfig{Path: unavailablePath},
	}
	if err := projects.writeJSON("settings.json", stored); err != nil {
		t.Fatal(err)
	}

	// 1. Status check should report unavailable with descriptive error message
	status := projects.checkStorageDatasource(stored.StorageDatasource)
	if status.Status != "unavailable" {
		t.Fatalf("expected unavailable status, got %q", status.Status)
	}
	if status.Message == "" {
		t.Fatal("expected descriptive message for unavailable datasource")
	}

	// 2. Summary endpoint should also reflect unavailable status
	summary, err := projects.storageDatasourceSummary()
	if err != nil {
		t.Fatalf("expected summary not to error out: %v", err)
	}
	if summary.Status != "unavailable" {
		t.Fatalf("expected summary status unavailable, got %q", summary.Status)
	}

	// 3. Opening repository bundle should fail explicitly, not fall back to JSON
	_, err = storage.NewRepositoryBundle(stored.StorageDatasource, directory)
	if err == nil {
		t.Fatal("expected opening unavailable datasource to return explicit error")
	}

	// 4. Ensure no silent local JSON fallback files were created
	if _, err := os.Stat(filepath.Join(directory, "boards.json")); !os.IsNotExist(err) {
		t.Fatalf("expected boards.json NOT to be created as fallback: %v", err)
	}
}

func TestQA_RoundTripMigrationPreservesAllEntities(t *testing.T) {
	projects, server := newDatasourceTestServer(t)
	now := time.Now().UTC()

	// 1. Seed all 5 durable entities in the initial local-json store
	sourceBundle, err := storage.NewRepositoryBundle(storage.DatasourceConfig{}, projects.directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceBundle.Boards.Create(context.Background(), kanban.Board{ID: "board-rt", ProjectID: "proj-rt", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := sourceBundle.Insights.Upsert(context.Background(), insights.Record{ID: "insight-rt", ProjectID: "proj-rt", Kind: insights.KindFeatureRadar, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := sourceBundle.Events.Append(observability.Event{ID: "event-rt", Timestamp: now, Category: "planning", Name: "started"}); err != nil {
		t.Fatal(err)
	}
	if err := sourceBundle.Notifications.Create(context.Background(), notifications.Notification{ID: "notif-rt", Level: notifications.LevelInfo, Title: "RT", Message: "Msg", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := sourceBundle.BuildProfiles.Upsert(context.Background(), buildverify.Profile{ProjectID: "proj-rt", Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	sourceBundle.Close()

	// 2. Migrate from Local JSON -> SQLite
	sqlitePath := filepath.Join(t.TempDir(), "roundtrip.sqlite")
	switchReq := switchStorageDatasourceRequest{
		Kind:    storage.DatasourceSQLite,
		SQLite:  storage.SQLiteConfig{Path: sqlitePath},
		Confirm: true,
	}
	reqBody, _ := json.Marshal(switchReq)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("JSON -> SQLite switch failed (%d): %s", rec.Code, rec.Body.String())
	}
	var res1 storageDatasourceMigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res1); err != nil {
		t.Fatal(err)
	}
	if !res1.Applied || res1.Counts.Total() != 5 {
		t.Fatalf("unexpected JSON -> SQLite migration result: %+v", res1)
	}

	// 3. Migrate from SQLite -> New Local JSON directory
	newJSONDir := filepath.Join(t.TempDir(), "json-target")
	switchBackReq := switchStorageDatasourceRequest{
		Kind:      storage.DatasourceLocalJSON,
		LocalJSON: storage.LocalJSONConfig{Directory: newJSONDir},
		Confirm:   true,
	}
	reqBodyBack, _ := json.Marshal(switchBackReq)
	reqBack := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(reqBodyBack))
	reqBack.Header.Set("Content-Type", "application/json")
	recBack := httptest.NewRecorder()
	server.ServeHTTP(recBack, reqBack)

	if recBack.Code != http.StatusOK {
		t.Fatalf("SQLite -> JSON switch failed (%d): %s", recBack.Code, recBack.Body.String())
	}
	var res2 storageDatasourceMigrationResult
	if err := json.Unmarshal(recBack.Body.Bytes(), &res2); err != nil {
		t.Fatal(err)
	}
	if !res2.Applied || res2.Counts.Total() != 5 {
		t.Fatalf("unexpected SQLite -> JSON migration result: %+v", res2)
	}

	// 4. Verify all records exist in target local-json store
	targetBundle, err := storage.NewRepositoryBundle(storage.DatasourceConfig{
		Kind:      storage.DatasourceLocalJSON,
		LocalJSON: storage.LocalJSONConfig{Directory: newJSONDir},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer targetBundle.Close()

	if _, err := targetBundle.Boards.Get(context.Background(), "board-rt"); err != nil {
		t.Fatalf("missing board-rt: %v", err)
	}
	if _, err := targetBundle.Insights.Get(context.Background(), "proj-rt", insights.KindFeatureRadar); err != nil {
		t.Fatalf("missing insight-rt: %v", err)
	}
	events, err := targetBundle.Events.List(observability.Query{})
	if err != nil || len(events) != 1 {
		t.Fatalf("expected 1 event, got %d (%v)", len(events), err)
	}
	notifs, err := targetBundle.Notifications.List(context.Background(), 0)
	if err != nil || len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d (%v)", len(notifs), err)
	}
	if _, err := targetBundle.BuildProfiles.Get(context.Background(), "proj-rt"); err != nil {
		t.Fatalf("missing build profile: %v", err)
	}
}

func TestQA_SwitchToAlreadyActiveDatasourceRejected(t *testing.T) {
	_, server := newDatasourceTestServer(t)

	// Attempt to switch to local-json with empty directory (which matches default active)
	body, _ := json.Marshal(switchStorageDatasourceRequest{
		Kind:    storage.DatasourceLocalJSON,
		Confirm: true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for already active datasource, got %d: %s", rec.Code, rec.Body.String())
	}
}
