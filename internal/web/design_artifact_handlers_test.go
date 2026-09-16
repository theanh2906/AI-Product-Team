package web

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/designartifact"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestGetTaskDesignArtifactServesOnlyPersistedTaskArtifact(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := designartifact.TaskDirectory(project.Path, "task-design")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	previewPath := filepath.Join(directory, "overview.png")
	preview, err := os.Create(previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(preview, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	_ = preview.Close()
	relative, _ := filepath.Rel(project.Path, previewPath)
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-design", ProjectID: project.ID, ProjectName: project.Name, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Backlog: []kanban.BacklogItem{}, Plans: []kanban.Plan{},
		Tasks: []kanban.Task{{
			ID: "task-design", Status: kanban.TaskCompleted, Role: kanban.RoleDesigner, Column: kanban.ColumnDone,
			Execution: &kanban.TaskExecution{Artifacts: []kanban.DesignArtifact{{
				ID: "artifact-preview", Title: "Overview", Kind: "mockup", RelativePath: filepath.ToSlash(relative), MediaType: "image/png",
			}}},
		}},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), projectService: projects}
	request := httptest.NewRequest(http.MethodGet, "/artifact", nil)
	request.SetPathValue("projectID", project.ID)
	request.SetPathValue("taskID", "task-design")
	request.SetPathValue("artifactID", "artifact-preview")
	recorder := httptest.NewRecorder()

	server.getTaskDesignArtifact(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || recorder.Body.Len() == 0 {
		t.Fatalf("unexpected artifact response: status=%d type=%q size=%d", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.Len())
	}
}

func TestGetTaskDesignSourceServesOnlyReportedSandboxedHTML(t *testing.T) {
	server, project, directory := newDesignSourceTestServer(t, []string{
		".productcrew/design-artifacts/task-design/overview.html",
	})
	if err := os.WriteFile(filepath.Join(directory, "overview.html"), []byte("<!doctype html><script>document.body.textContent='ready'</script>"), 0o600); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/design-source?path=.productcrew/design-artifacts/task-design/overview.html", nil)
	request.SetPathValue("projectID", project.ID)
	request.SetPathValue("taskID", "task-design")
	recorder := httptest.NewRecorder()

	server.getTaskDesignSource(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected source response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if policy := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "sandbox allow-scripts") || !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("design source is not sandboxed: %q", policy)
	}
	if !strings.Contains(recorder.Body.String(), "document.body.textContent") {
		t.Fatalf("expected persisted HTML body, got %q", recorder.Body.String())
	}
}

func TestGetTaskDesignSourceRejectsUnreportedAndOutsideFiles(t *testing.T) {
	server, project, directory := newDesignSourceTestServer(t, []string{
		".productcrew/design-artifacts/task-design/overview.html",
	})
	if err := os.WriteFile(filepath.Join(directory, "unreported.html"), []byte("unreported"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Path, "outside.html"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, requestedPath := range []string{
		".productcrew/design-artifacts/task-design/unreported.html",
		"outside.html",
		".productcrew/design-artifacts/task-design/../../outside.html",
	} {
		request := httptest.NewRequest(http.MethodGet, "/design-source?path="+requestedPath, nil)
		request.SetPathValue("projectID", project.ID)
		request.SetPathValue("taskID", "task-design")
		recorder := httptest.NewRecorder()

		server.getTaskDesignSource(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected %q to be rejected, got status=%d body=%q", requestedPath, recorder.Code, recorder.Body.String())
		}
	}
}

func newDesignSourceTestServer(t *testing.T, changedFiles []string) (*server, project, string) {
	t.Helper()
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := designartifact.TaskDirectory(project.Path, "task-design")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-design", ProjectID: project.ID, ProjectName: project.Name, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Backlog: []kanban.BacklogItem{}, Plans: []kanban.Plan{},
		Tasks: []kanban.Task{{
			ID: "task-design", Status: kanban.TaskCompleted, Role: kanban.RoleDesigner, Column: kanban.ColumnDone,
			Execution: &kanban.TaskExecution{ChangedFiles: changedFiles, CompletedAt: time.Now()},
		}},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	return &server{boards: kanban.NewService(repository), projectService: projects}, project, directory
}
