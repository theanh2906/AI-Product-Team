package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func newDatasourceTestServer(t *testing.T) (*projectService, http.Handler) {
	t.Helper()
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	directory := t.TempDir()
	projects, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	server := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, newBoardService(projects), nil)
	return projects, server
}

func TestGetStorageDatasourceDefaultsToLocalJSON(t *testing.T) {
	_, server := newDatasourceTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/storage-datasource", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var status storageDatasourceStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Kind != storage.DatasourceLocalJSON {
		t.Fatalf("expected local-json default, got %q", status.Kind)
	}
	if status.Status != "ok" {
		t.Fatalf("expected the default datasource to report ok, got %q (%s)", status.Status, status.Message)
	}
}

func TestCheckStorageDatasourceReportsUnavailableTargetWithoutPersisting(t *testing.T) {
	projects, server := newDatasourceTestServer(t)

	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	unavailablePath := filepath.Join(blocker, "productcrew.sqlite")

	body, err := json.Marshal(storageDatasourceRequest{Kind: storage.DatasourceSQLite, SQLite: storage.SQLiteConfig{Path: unavailablePath}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var status storageDatasourceStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "unavailable" || status.Message == "" {
		t.Fatalf("expected an unavailable status with a message, got %+v", status)
	}

	current, err := projects.storageDatasourceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if current.Kind != storage.DatasourceLocalJSON {
		t.Fatalf("expected a non-destructive check not to change the active datasource, got %q", current.Kind)
	}
}

func TestSwitchStorageDatasourceRequiresConfirmation(t *testing.T) {
	_, server := newDatasourceTestServer(t)

	body, err := json.Marshal(switchStorageDatasourceRequest{Kind: storage.DatasourceSQLite, SQLite: storage.SQLiteConfig{Path: filepath.Join(t.TempDir(), "productcrew.sqlite")}, Confirm: false})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 without confirmation, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSwitchStorageDatasourceMigratesAndPersistsTarget(t *testing.T) {
	projects, server := newDatasourceTestServer(t)

	// Seed a board through the active local-json datasource before switching.
	createReq := httptest.NewRequest(http.MethodPost, "/api/projects/import/local", bytes.NewReader(mustJSON(t, importLocalRequest{Path: t.TempDir()})))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected project import to succeed, got %d: %s", createRec.Code, createRec.Body.String())
	}

	targetPath := filepath.Join(t.TempDir(), "productcrew.sqlite")
	body, err := json.Marshal(switchStorageDatasourceRequest{Kind: storage.DatasourceSQLite, SQLite: storage.SQLiteConfig{Path: targetPath}, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result storageDatasourceMigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.Status != "applied" {
		t.Fatalf("expected migration to apply, got %+v", result)
	}
	if result.ActiveDatasource.Kind != storage.DatasourceSQLite || result.ActiveDatasource.SQLite.Path != targetPath {
		t.Fatalf("expected sqlite to become active at %q, got %+v", targetPath, result.ActiveDatasource)
	}

	current, err := projects.storageDatasourceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if current.Kind != storage.DatasourceSQLite || current.SQLite.Path != targetPath {
		t.Fatalf("expected settings.json to persist the new datasource, got %+v", current)
	}
}

func TestSwitchStorageDatasourceKeepsPreviousActiveOnMigrationFailure(t *testing.T) {
	projects, server := newDatasourceTestServer(t)

	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	unavailablePath := filepath.Join(blocker, "productcrew.sqlite")

	body, err := json.Marshal(switchStorageDatasourceRequest{Kind: storage.DatasourceSQLite, SQLite: storage.SQLiteConfig{Path: unavailablePath}, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/storage-datasource/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for an unavailable target, got %d: %s", rec.Code, rec.Body.String())
	}

	current, err := projects.storageDatasourceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if current.Kind != storage.DatasourceLocalJSON {
		t.Fatalf("expected local-json to remain active after a failed switch, got %q", current.Kind)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
