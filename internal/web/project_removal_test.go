package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestDeleteProjectAPIRemovesMetadataBoardAndInsightsWithoutDeletingSource(t *testing.T) {
	dataDirectory := t.TempDir()
	firstPath := filepath.Join(t.TempDir(), "first-project")
	secondPath := filepath.Join(t.TempDir(), "second-project")
	if err := os.MkdirAll(firstPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondPath, 0o755); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := projects.importLocal(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := projects.importLocal(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	boardRepository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(boardRepository)
	now := time.Now().UTC()
	if err := boardRepository.Create(context.Background(), kanban.Board{ID: "board-1", ProjectID: first.ID, ProjectName: first.Name, Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, Backlog: []kanban.BacklogItem{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := boardRepository.Create(context.Background(), kanban.Board{ID: "board-2", ProjectID: second.ID, ProjectName: second.Name, Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, Backlog: []kanban.BacklogItem{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	insightRepository, err := storage.NewJSONInsightRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightService := insights.NewService(insightRepository)
	if err := insightService.Save(context.Background(), first.ID, insights.KindFeatureRadar, map[string]string{"summary": "remove feature"}); err != nil {
		t.Fatal(err)
	}
	if err := insightService.Save(context.Background(), first.ID, insights.KindBugScan, map[string]string{"summary": "remove scan"}); err != nil {
		t.Fatal(err)
	}
	if err := insightService.Save(context.Background(), second.ID, insights.KindFeatureRadar, map[string]string{"summary": "keep feature"}); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, nil)

	request := httptest.NewRequest(http.MethodDelete, "/api/projects/"+first.ID, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var result removeProjectResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Project.ID != first.ID || !result.BoardRemoved || result.InsightsRemoved != 2 {
		t.Fatalf("unexpected delete result: %+v", result)
	}
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("source folder must be preserved: %v", err)
	}
	remainingProjects, err := projects.listProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(remainingProjects) != 1 || remainingProjects[0].ID != second.ID {
		t.Fatalf("expected only unrelated project metadata to remain, got %+v", remainingProjects)
	}
	if _, err := boards.GetProjectBoard(context.Background(), first.ID); !errors.Is(err, kanban.ErrNotFound) {
		t.Fatalf("expected removed board to be missing, got %v", err)
	}
	if _, err := boards.GetProjectBoard(context.Background(), second.ID); err != nil {
		t.Fatalf("unrelated board must remain: %v", err)
	}
	if records, err := insightRepository.ListProject(context.Background(), first.ID); err != nil || len(records) != 0 {
		t.Fatalf("expected project insights to be removed, got %+v %v", records, err)
	}
	if records, err := insightRepository.ListProject(context.Background(), second.ID); err != nil || len(records) != 1 {
		t.Fatalf("unrelated insights must remain, got %+v %v", records, err)
	}
}

func TestDeleteProjectAPISucceedsWhenBoardAndInsightsAreMissing(t *testing.T) {
	dataDirectory := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "project-without-artifacts")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	server := newProjectRemovalTestServer(t, dataDirectory, projects)

	recorder := performProjectDelete(server, project.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected delete without artifacts to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if projects, err := projects.listProjects(); err != nil || len(projects) != 0 {
		t.Fatalf("expected project metadata to be removed, got %+v %v", projects, err)
	}
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("source folder must be preserved: %v", err)
	}
}

func TestDeleteProjectAPIRejectsActiveBoardWorkWithoutMutatingData(t *testing.T) {
	cases := []struct {
		name  string
		board kanban.Board
	}{
		{
			name:  "planning analyzing",
			board: kanban.Board{ID: "board-1", Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningAnalyzing}}, Tasks: []kanban.Task{}, Backlog: []kanban.BacklogItem{}},
		},
		{
			name:  "active task id",
			board: kanban.Board{ID: "board-1", ActiveTaskID: "task-1", Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, Backlog: []kanban.BacklogItem{}},
		},
		{
			name:  "task in progress",
			board: kanban.Board{ID: "board-1", Plans: []kanban.Plan{}, Tasks: []kanban.Task{{ID: "task-1", Status: kanban.TaskInProgress}}, Backlog: []kanban.BacklogItem{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDirectory := t.TempDir()
			projects, project := createImportedProject(t, dataDirectory)
			server := newProjectRemovalTestServer(t, dataDirectory, projects)
			tc.board.ProjectID = project.ID
			tc.board.ProjectName = project.Name
			tc.board.CreatedAt = time.Now().UTC()
			tc.board.UpdatedAt = tc.board.CreatedAt
			if _, err := server.boards.DeleteProjectBoard(context.Background(), project.ID); err != nil {
				t.Fatal(err)
			}
			boardRepository, err := storage.NewJSONBoardRepository(dataDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if err := boardRepository.Create(context.Background(), tc.board); err != nil {
				t.Fatal(err)
			}
			if err := server.insights.Save(context.Background(), project.ID, insights.KindFeatureRadar, map[string]string{"summary": "keep"}); err != nil {
				t.Fatal(err)
			}

			recorder := performProjectDelete(server, project.ID)
			if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), projectRemovalActiveWorkMessage) {
				t.Fatalf("expected active work conflict, got %d: %s", recorder.Code, recorder.Body.String())
			}
			assertProjectRemovalDidNotMutate(t, server, projects, project.ID)
		})
	}
}

func TestDeleteProjectAPIRejectsRunningProjectScopedJobs(t *testing.T) {
	cases := []struct {
		name string
		run  func(*server, project, <-chan struct{})
	}{
		{
			name: "source scan",
			run: func(server *server, imported project, release <-chan struct{}) {
				server.sourceScans = newSourceScanManager(func(_ context.Context, scanned project, _ sourceScanRequest, scanID string) (sourceScanResult, error) {
					<-release
					return sourceScanResult{ScanID: scanID, ProjectID: scanned.ID, ProjectName: scanned.Name}, nil
				}, server.insights)
				server.sourceScans.start(imported, sourceScanRequest{})
			},
		},
		{
			name: "feature radar",
			run: func(server *server, imported project, release <-chan struct{}) {
				server.featureRadar = newFeatureRadarManager(func(_ context.Context, analyzed project, _ featureRadarRequest, analysisID string) (featureRadarResult, error) {
					<-release
					return featureRadarResult{AnalysisID: analysisID, ProjectID: analyzed.ID, ProjectName: analyzed.Name, AnalyzedAt: time.Now().UTC()}, nil
				}, server.insights)
				if _, err := server.featureRadar.start(imported, featureRadarRequest{Reanalyze: true}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDirectory := t.TempDir()
			projects, project := createImportedProject(t, dataDirectory)
			server := newProjectRemovalTestServer(t, dataDirectory, projects)
			seedInactiveProjectArtifacts(t, server, project)
			release := make(chan struct{})
			tc.run(server, project, release)

			recorder := performProjectDelete(server, project.ID)
			if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), projectRemovalActiveWorkMessage) {
				t.Fatalf("expected running job conflict, got %d: %s", recorder.Code, recorder.Body.String())
			}
			assertProjectRemovalDidNotMutate(t, server, projects, project.ID)
			close(release)
			waitForProjectJobsToStop(t, server, project.ID)
		})
	}
}

func TestDeleteProjectAPIReturnsNotFound(t *testing.T) {
	dataDirectory := t.TempDir()
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	server := newProjectRemovalTestServer(t, dataDirectory, projects)

	recorder := performProjectDelete(server, "missing-project")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected missing project 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func newProjectRemovalTestServer(t *testing.T, dataDirectory string, projects *projectService) *server {
	t.Helper()
	boardRepository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightRepository, err := storage.NewJSONInsightRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightService := insights.NewService(insightRepository)
	return &server{
		projectService: projects,
		boards:         kanban.NewService(boardRepository),
		insights:       insightService,
		sourceScans:    newSourceScanManager(nil, insightService),
		featureRadar:   newFeatureRadarManager(nil, insightService),
	}
}

func createImportedProject(t *testing.T, dataDirectory string) (*projectService, project) {
	t.Helper()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	return projects, project
}

func performProjectDelete(server *server, projectID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID, bytes.NewReader(nil))
	request.SetPathValue("projectID", projectID)
	recorder := httptest.NewRecorder()
	server.deleteProject(recorder, request)
	return recorder
}

func seedInactiveProjectArtifacts(t *testing.T, server *server, project project) {
	t.Helper()
	now := time.Now().UTC()
	board := kanban.Board{
		ID: "board-" + project.ID, ProjectID: project.ID, ProjectName: project.Name,
		Plans: []kanban.Plan{}, Tasks: []kanban.Task{}, Backlog: []kanban.BacklogItem{},
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := server.boards.DeleteProjectBoard(context.Background(), project.ID); err != nil {
		t.Fatal(err)
	}
	boardRepository, err := storage.NewJSONBoardRepository(server.projectService.directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := boardRepository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	if err := server.insights.Save(context.Background(), project.ID, insights.KindBugScan, map[string]string{"summary": "keep"}); err != nil {
		t.Fatal(err)
	}
}

func waitForProjectJobsToStop(t *testing.T, server *server, projectID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !server.sourceScans.projectRunning(projectID) && !server.featureRadar.projectRunning(projectID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("project jobs did not stop before cleanup")
}

func assertProjectRemovalDidNotMutate(t *testing.T, server *server, projects *projectService, projectID string) {
	t.Helper()
	if _, err := projects.findProject(projectID); err != nil {
		t.Fatalf("project metadata was mutated: %v", err)
	}
	if _, err := server.boards.GetProjectBoard(context.Background(), projectID); err != nil {
		t.Fatalf("project board was mutated: %v", err)
	}
	if records, err := server.insights.ListProject(context.Background(), projectID); err != nil || len(records) != 1 {
		t.Fatalf("project insights were mutated, got %+v %v", records, err)
	}
}
