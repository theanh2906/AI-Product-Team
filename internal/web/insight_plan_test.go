package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/planning"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

// countingPlanner records how many times Team Lead planning actually ran, so
// tests can assert a repeated "plan" action does not start a second run.
type countingPlanner struct {
	mu    sync.Mutex
	calls int
}

func (p *countingPlanner) Generate(context.Context, planning.Request) (planning.Result, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return planning.Result{
		ThreadID: "thread-plan-now",
		Draft: kanban.PlanDraft{
			Summary: "Implement the approved insight through one Developer task.",
			Tasks: []kanban.DraftTask{
				{Ref: "implement", Role: kanban.RoleDeveloper, Title: "Implement insight", Priority: kanban.PriorityHigh, Description: "Implement the approved scope.", AcceptanceCriteria: []string{"Behavior matches the insight"}},
			},
		},
	}, nil
}

func (p *countingPlanner) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func waitForAwaitingApproval(t *testing.T, boards *kanban.Service, projectID string) kanban.Board {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var board kanban.Board
	var err error
	for time.Now().Before(deadline) {
		board, err = boards.GetProjectBoard(context.Background(), projectID)
		if err == nil && len(board.Plans) == 1 && board.Plans[0].Status == kanban.PlanningAwaitingApproval {
			return board
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Team Lead plan did not reach awaiting_approval: %+v (%v)", board, err)
	return board
}

func TestUpdateFeatureSuggestionPlanActionIsIdempotent(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "feature-radar-plan")
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

	first := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/"+suggestion.ID, strings.NewReader(`{"action":"plan"}`))
	first.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("expected plan action 200, got %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstResponse struct {
		Result featureRadarResult `json:"result"`
		Board  kanban.Board       `json:"board"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil {
		t.Fatal(err)
	}
	if firstResponse.Result.Suggestions[0].Status != "planning" || firstResponse.Result.Suggestions[0].BacklogItemID == "" {
		t.Fatalf("expected the suggestion to move to planning with a backlog item id: %+v", firstResponse.Result.Suggestions[0])
	}
	if len(firstResponse.Board.Backlog) != 1 || firstResponse.Board.Backlog[0].Status != kanban.BacklogPlanning {
		t.Fatalf("expected exactly one planning backlog item: %+v", firstResponse.Board.Backlog)
	}
	if len(firstResponse.Board.Plans) != 1 {
		t.Fatalf("expected exactly one plan, got %+v", firstResponse.Board.Plans)
	}
	backlogItemID := firstResponse.Result.Suggestions[0].BacklogItemID

	waitForAwaitingApproval(t, boards, project.ID)
	if calls := planner.callCount(); calls != 1 {
		t.Fatalf("expected exactly one Team Lead run, got %d", calls)
	}

	second := httptest.NewRequest(http.MethodPatch, "/api/feature-radar/"+project.ID+"/suggestions/"+suggestion.ID, strings.NewReader(`{"action":"plan"}`))
	second.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected repeated plan action 200, got %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var secondResponse struct {
		Result featureRadarResult `json:"result"`
		Board  kanban.Board       `json:"board"`
	}
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &secondResponse); err != nil {
		t.Fatal(err)
	}
	if secondResponse.Result.Suggestions[0].BacklogItemID != backlogItemID {
		t.Fatalf("repeated plan action must reuse the same backlog item, got %+v", secondResponse.Result.Suggestions[0])
	}
	if len(secondResponse.Board.Backlog) != 1 || len(secondResponse.Board.Plans) != 1 {
		t.Fatalf("repeated plan action must not duplicate backlog items or plans: %+v", secondResponse.Board)
	}
	time.Sleep(50 * time.Millisecond)
	if calls := planner.callCount(); calls != 1 {
		t.Fatalf("repeated plan action must not start another Team Lead run, got %d calls", calls)
	}
}

func TestUpdateSourceFindingPlanActionIsIdempotent(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "source-scan-plan")
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
	finding := sourceScanFinding{
		ID: "finding-1", Severity: "high", Category: "reliability", Title: "Unbounded retry loop",
		Description: "The retry loop never backs off.", Evidence: "for {} without delay", File: "internal/example/retry.go", Line: 42,
		Recommendation: "Add exponential backoff.", Status: "suggested",
	}
	saved := sourceScanResult{ScanID: "SCAN-1", ProjectID: project.ID, ProjectName: project.Name, ScannedAt: time.Now().UTC(), Findings: []sourceScanFinding{finding}}
	if err := insightService.Save(context.Background(), project.ID, insights.KindBugScan, saved); err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRequest(http.MethodPatch, "/api/source-scans/"+project.ID+"/findings/"+finding.ID, strings.NewReader(`{"action":"plan"}`))
	first.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("expected plan action 200, got %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstResponse struct {
		Result sourceScanResult `json:"result"`
		Board  kanban.Board     `json:"board"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil {
		t.Fatal(err)
	}
	if firstResponse.Result.Findings[0].Status != "planning" || firstResponse.Result.Findings[0].BacklogItemID == "" {
		t.Fatalf("expected the finding to move to planning with a backlog item id: %+v", firstResponse.Result.Findings[0])
	}
	if len(firstResponse.Board.Backlog) != 1 || firstResponse.Board.Backlog[0].Status != kanban.BacklogPlanning {
		t.Fatalf("expected exactly one planning backlog item: %+v", firstResponse.Board.Backlog)
	}
	if len(firstResponse.Board.Plans) != 1 {
		t.Fatalf("expected exactly one plan, got %+v", firstResponse.Board.Plans)
	}
	backlogItemID := firstResponse.Result.Findings[0].BacklogItemID

	waitForAwaitingApproval(t, boards, project.ID)
	if calls := planner.callCount(); calls != 1 {
		t.Fatalf("expected exactly one Team Lead run, got %d", calls)
	}

	second := httptest.NewRequest(http.MethodPatch, "/api/source-scans/"+project.ID+"/findings/"+finding.ID, strings.NewReader(`{"action":"plan"}`))
	second.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected repeated plan action 200, got %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var secondResponse struct {
		Result sourceScanResult `json:"result"`
		Board  kanban.Board     `json:"board"`
	}
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &secondResponse); err != nil {
		t.Fatal(err)
	}
	if secondResponse.Result.Findings[0].BacklogItemID != backlogItemID {
		t.Fatalf("repeated plan action must reuse the same backlog item, got %+v", secondResponse.Result.Findings[0])
	}
	if len(secondResponse.Board.Backlog) != 1 || len(secondResponse.Board.Plans) != 1 {
		t.Fatalf("repeated plan action must not duplicate backlog items or plans: %+v", secondResponse.Board)
	}
	time.Sleep(50 * time.Millisecond)
	if calls := planner.callCount(); calls != 1 {
		t.Fatalf("repeated plan action must not start another Team Lead run, got %d calls", calls)
	}
}
