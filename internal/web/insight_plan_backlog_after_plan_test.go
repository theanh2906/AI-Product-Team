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

// TestUpdateFeatureSuggestionBacklogAfterPlanDoesNotRegressStatus reproduces a
// stale-tab / duplicate-request scenario: a suggestion is already moved to
// Team Lead planning via "plan", then a late "backlog" request for the same
// suggestion arrives (e.g. a second browser tab that had not yet refreshed).
// The insight status must not regress from "planning" back to "backlog"
// while the backing backlog item/plan remain in Team Lead planning.
func TestUpdateFeatureSuggestionBacklogAfterPlanDoesNotRegressStatus(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "feature-radar-plan-then-backlog")
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
	planner := &countingPlanner{}
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, planner)

	insightRepository, err := storage.NewJSONInsightRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	insightService := insights.NewService(insightRepository)
	suggestion := featureSuggestion{
		ID: "suggestion-1", Category: "workflow", Title: "Plan now handoff",
		Problem:  "The user must manually switch to the Work board to start planning.",
		Proposal: "Add a Plan now action from Feature Radar.", Rationale: "Composes existing backlog and planning primitives.",
		Feasibility: 80, DeliveryTarget: "fullstack", RequiresUI: true, Status: "suggested",
		AcceptanceCriteria: []string{"Plan now creates or reuses the backlog item"},
	}
	saved := featureRadarResult{AnalysisID: "RADAR-1", ProjectID: project.ID, ProjectName: project.Name, AnalyzedAt: time.Now().UTC(), Suggestions: []featureSuggestion{suggestion}}
	if err := insightService.Save(context.Background(), project.ID, insights.KindFeatureRadar, saved); err != nil {
		t.Fatal(err)
	}

	planRequest := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/"+suggestion.ID, strings.NewReader(`{"action":"plan"}`))
	planRequest.Header.Set("Content-Type", "application/json")
	planRecorder := httptest.NewRecorder()
	handler.ServeHTTP(planRecorder, planRequest)
	if planRecorder.Code != http.StatusOK {
		t.Fatalf("expected plan action 200, got %d: %s", planRecorder.Code, planRecorder.Body.String())
	}

	// A late "backlog" request for the same suggestion arrives after planning
	// has already started (e.g. a stale second tab).
	backlogRequest := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/"+suggestion.ID, strings.NewReader(`{"action":"backlog"}`))
	backlogRequest.Header.Set("Content-Type", "application/json")
	backlogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(backlogRecorder, backlogRequest)
	if backlogRecorder.Code != http.StatusOK {
		t.Fatalf("expected backlog action 200, got %d: %s", backlogRecorder.Code, backlogRecorder.Body.String())
	}

	var response struct {
		Result featureRadarResult `json:"result"`
		Board  kanban.Board       `json:"board"`
	}
	if err := json.Unmarshal(backlogRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	if len(response.Board.Backlog) != 1 || response.Board.Backlog[0].Status != kanban.BacklogPlanning {
		t.Fatalf("expected the backlog item to remain in planning, got %+v", response.Board.Backlog)
	}
	if response.Result.Suggestions[0].Status != "planning" {
		t.Fatalf("insight status regressed to %q after a late backlog request, even though the backlog item and plan are still in Team Lead planning: %+v", response.Result.Suggestions[0].Status, response.Result.Suggestions[0])
	}
}
