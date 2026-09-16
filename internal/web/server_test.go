package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWorkItemRouteServesAngularSPA(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/work-items", nil)
	recorder := httptest.NewRecorder()

	assets := fstest.MapFS{
		"index.html": {Data: []byte(`<app-root></app-root><script src="main-test.js"></script><title>Configure work item</title>`)},
	}
	newServer(assets).ServeHTTP(recorder, req)

	res := recorder.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("expected the SPA shell not to be cached, got %q", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff protection, got %q", got)
	}
	page := string(body)
	for _, expected := range []string{"<app-root>", "Configure work item", "main-"} {
		if !strings.Contains(page, expected) {
			t.Errorf("expected Angular index to contain %q", expected)
		}
	}
}

func TestMissingFrontendAssetDoesNotFallBackToSPA(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": {Data: []byte(`<app-root></app-root>`)},
	}
	req := httptest.NewRequest(http.MethodGet, "/styles-stale.css", nil)
	recorder := httptest.NewRecorder()

	newServer(assets).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected a stale asset request to return 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "<app-root>") {
		t.Fatal("expected a missing asset not to receive the SPA index")
	}
}

func TestDirectIndexRequestIsNotCached(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": {Data: []byte(`<app-root></app-root>`)},
	}
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	recorder := httptest.NewRecorder()

	newServer(assets).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("expected the file server's canonical index redirect, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("expected direct index requests not to be cached, got %q", got)
	}
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "ready") {
		t.Fatalf("unexpected health response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestBackupRestoreHandlers(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	directory := t.TempDir()
	credentials := &memoryCredentialStore{value: "token-value"}
	projects, err := newProjectService(directory, credentials)
	if err != nil {
		t.Fatal(err)
	}

	server := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, newBoardService(projects), nil)

	// 1. GET /api/settings/backup/export
	reqExport := httptest.NewRequest(http.MethodGet, "/api/settings/backup/export", nil)
	recExport := httptest.NewRecorder()
	server.ServeHTTP(recExport, reqExport)

	if recExport.Code != http.StatusOK {
		t.Fatalf("expected export 200, got %d: %s", recExport.Code, recExport.Body.String())
	}

	cd := recExport.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "productcrew-settings-backup.json") {
		t.Errorf("expected Content-Disposition attachment with backup filename, got %q", cd)
	}

	var payload BackupPayload
	if err := json.Unmarshal(recExport.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal exported payload: %v", err)
	}

	if payload.Version != 1 {
		t.Errorf("expected exported payload version 1, got %d", payload.Version)
	}

	// Ensure secret is omitted
	if strings.Contains(recExport.Body.String(), "token-value") {
		t.Error("secret was not omitted from exported payload")
	}

	// 2. POST /api/settings/backup/preview (successful)
	previewBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	reqPreview := httptest.NewRequest(http.MethodPost, "/api/settings/backup/preview", bytes.NewReader(previewBody))
	reqPreview.Header.Set("Content-Type", "application/json")
	recPreview := httptest.NewRecorder()
	server.ServeHTTP(recPreview, reqPreview)

	if recPreview.Code != http.StatusOK {
		t.Fatalf("expected preview 200, got %d: %s", recPreview.Code, recPreview.Body.String())
	}

	var preview BackupPreviewResponse
	if err := json.Unmarshal(recPreview.Body.Bytes(), &preview); err != nil {
		t.Fatalf("failed to unmarshal preview: %v", err)
	}

	// 3. POST /api/settings/backup/preview (failure for version > 1)
	badPayload := payload
	badPayload.Version = 2
	badPreviewBody, err := json.Marshal(badPayload)
	if err != nil {
		t.Fatal(err)
	}

	reqBadPreview := httptest.NewRequest(http.MethodPost, "/api/settings/backup/preview", bytes.NewReader(badPreviewBody))
	reqBadPreview.Header.Set("Content-Type", "application/json")
	recBadPreview := httptest.NewRecorder()
	server.ServeHTTP(recBadPreview, reqBadPreview)

	if recBadPreview.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected preview failure 422, got %d: %s", recBadPreview.Code, recBadPreview.Body.String())
	}
	if !strings.Contains(recBadPreview.Body.String(), "unsupported backup version 2") {
		t.Errorf("expected detailed error message, got %s", recBadPreview.Body.String())
	}

	// 4. POST /api/settings/backup/restore (successful)
	payload.Settings.Theme = "dark"
	restoreBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	reqRestore := httptest.NewRequest(http.MethodPost, "/api/settings/backup/restore", bytes.NewReader(restoreBody))
	reqRestore.Header.Set("Content-Type", "application/json")
	recRestore := httptest.NewRecorder()
	server.ServeHTTP(recRestore, reqRestore)

	if recRestore.Code != http.StatusOK {
		t.Fatalf("expected restore 200, got %d: %s", recRestore.Code, recRestore.Body.String())
	}

	var restored settings
	if err := json.Unmarshal(recRestore.Body.Bytes(), &restored); err != nil {
		t.Fatalf("failed to unmarshal restored: %v", err)
	}
	if restored.Theme != "dark" {
		t.Errorf("expected restored Theme to be 'dark', got %q", restored.Theme)
	}
}
