package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestFeatureRadarReusesSavedProjectResult(t *testing.T) {
	service := newTestInsightService(t)
	saved := featureRadarResult{
		AnalysisID: "RADAR-SAVED", ProjectID: "project-1", ProjectName: "sample",
		AnalyzedAt: time.Now().UTC(), Suggestions: []featureSuggestion{{ID: "suggestion-1", Title: "Saved", Status: "suggested"}},
	}
	if err := service.Save(context.Background(), saved.ProjectID, insights.KindFeatureRadar, saved); err != nil {
		t.Fatal(err)
	}
	runs := 0
	manager := newFeatureRadarManager(func(context.Context, project, featureRadarRequest, string) (featureRadarResult, error) {
		runs++
		return featureRadarResult{}, nil
	}, service)

	job, err := manager.start(project{ID: saved.ProjectID, Name: saved.ProjectName}, featureRadarRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.Result == nil || job.Result.AnalysisID != saved.AnalysisID {
		t.Fatalf("expected saved completed result, got %+v", job)
	}
	if runs != 0 {
		t.Fatalf("expected saved analysis to avoid a new AI run, got %d runs", runs)
	}
}

func TestFeatureRadarPersistsCompletedResultForRestart(t *testing.T) {
	service := newTestInsightService(t)
	manager := newFeatureRadarManager(func(_ context.Context, project project, _ featureRadarRequest, analysisID string) (featureRadarResult, error) {
		return featureRadarResult{AnalysisID: analysisID, ProjectID: project.ID, ProjectName: project.Name, AnalyzedAt: time.Now().UTC()}, nil
	}, service)
	job, err := manager.start(project{ID: "project-1", Name: "sample"}, featureRadarRequest{Reanalyze: true})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		restarted := newFeatureRadarManager(nil, service)
		latest, exists, latestErr := restarted.latest("project-1")
		if latestErr != nil {
			t.Fatal(latestErr)
		}
		if exists && latest.Status == "completed" && latest.AnalysisID == job.AnalysisID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("completed Feature Radar result was not restored from durable storage")
}

type updateFeatureSuggestionResponse struct {
	Result featureRadarResult `json:"result"`
	Board  kanban.Board       `json:"board"`
}

func TestUpdateFeatureSuggestionKeepsPlanningStatusOnStaleBacklogAction(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "radar")
	if err := ensureDirectory(projectDirectory); err != nil {
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
	boardRepository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(boardRepository)

	insightRepository, err := storage.NewJSONInsightRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightService := insights.NewService(insightRepository)
	saved := featureRadarResult{
		AnalysisID: "RADAR-1", ProjectID: project.ID, ProjectName: project.Name, AnalyzedAt: time.Now().UTC(),
		Suggestions: []featureSuggestion{{
			ID: "suggestion-1", Title: "Add saved search filters", Category: "enhancement",
			Problem: "Users retype the same search filters on every visit.", Proposal: "Persist and restore selected filters per user.",
			Rationale: "Reduces repeated setup and improves task completion time.", DeliveryTarget: "fullstack",
			AcceptanceCriteria: []string{"Filters persist across sessions"}, Status: "suggested",
		}},
	}
	if err := insightService.Save(context.Background(), project.ID, insights.KindFeatureRadar, saved); err != nil {
		t.Fatal(err)
	}

	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	patch := func(action string) updateFeatureSuggestionResponse {
		t.Helper()
		body := strings.NewReader(`{"action":"` + action + `"}`)
		request := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/suggestion-1", body)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("action %q: expected 200, got %d: %s", action, recorder.Code, recorder.Body.String())
		}
		var response updateFeatureSuggestionResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("action %q: failed to decode response: %v", action, err)
		}
		return response
	}

	planned := patch("plan")
	if planned.Result.Suggestions[0].Status != "planning" {
		t.Fatalf("expected plan action to report status=planning, got %+v", planned.Result.Suggestions[0])
	}
	backlogItemID := planned.Result.Suggestions[0].BacklogItemID
	if backlogItemID == "" {
		t.Fatal("expected plan action to assign a backlog item id")
	}
	if len(planned.Board.Backlog) != 1 || planned.Board.Backlog[0].Status != kanban.BacklogPlanning {
		t.Fatalf("expected exactly one backlog item in planning, got %+v", planned.Board.Backlog)
	}
	if len(planned.Board.Plans) != 1 {
		t.Fatalf("expected exactly one plan, got %+v", planned.Board.Plans)
	}

	staleBacklog := patch("backlog")
	if staleBacklog.Result.Suggestions[0].Status != "planning" {
		t.Fatalf("expected stale backlog action to keep status=planning, got %+v", staleBacklog.Result.Suggestions[0])
	}
	if staleBacklog.Result.Suggestions[0].BacklogItemID != backlogItemID {
		t.Fatalf("expected backlog item id to remain unchanged, got %q want %q", staleBacklog.Result.Suggestions[0].BacklogItemID, backlogItemID)
	}
	if len(staleBacklog.Board.Backlog) != 1 || staleBacklog.Board.Backlog[0].Status != kanban.BacklogPlanning {
		t.Fatalf("expected no duplicate backlog item, got %+v", staleBacklog.Board.Backlog)
	}
	if len(staleBacklog.Board.Plans) != 1 {
		t.Fatalf("expected no duplicate plan, got %+v", staleBacklog.Board.Plans)
	}
}

func TestUpdateFeatureSuggestionCreatesBacklogItemForFreshBacklogAction(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "radar")
	if err := ensureDirectory(projectDirectory); err != nil {
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
	boardRepository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(boardRepository)

	insightRepository, err := storage.NewJSONInsightRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightService := insights.NewService(insightRepository)
	saved := featureRadarResult{
		AnalysisID: "RADAR-2", ProjectID: project.ID, ProjectName: project.Name, AnalyzedAt: time.Now().UTC(),
		Suggestions: []featureSuggestion{{
			ID: "suggestion-2", Title: "Add export to CSV", Category: "enhancement",
			Problem: "Users cannot export report data.", Proposal: "Add a CSV export button to the reports page.",
			Rationale: "Enables offline analysis of report data.", DeliveryTarget: "fullstack",
			AcceptanceCriteria: []string{"Exported CSV matches on-screen data"}, Status: "suggested",
		}},
	}
	if err := insightService.Save(context.Background(), project.ID, insights.KindFeatureRadar, saved); err != nil {
		t.Fatal(err)
	}

	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	body := strings.NewReader(`{"action":"backlog"}`)
	request := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/suggestion-2", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response updateFeatureSuggestionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Result.Suggestions[0].Status != "backlog" {
		t.Fatalf("expected fresh backlog action to report status=backlog, got %+v", response.Result.Suggestions[0])
	}
	if len(response.Board.Backlog) != 1 || response.Board.Backlog[0].Status != kanban.BacklogOpen {
		t.Fatalf("expected exactly one open backlog item, got %+v", response.Board.Backlog)
	}
}
