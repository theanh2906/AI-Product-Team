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
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestAutomationBacklogCreateStatusAndExploreAPI(t *testing.T) {
	handler, project, _ := newRemoteControlTestServer(t)
	payload := []byte(`{
		"type":"todo",
		"source":"remote-cli",
		"title":"Document remote automation contract",
		"description":"Document the stable JSON contract used by remote ProductCrew agents.",
		"deliveryTarget":"backend",
		"acceptanceCriteria":["Remote agents can parse the documented response"]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/automation/backlog", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected ticket creation 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var created remoteTicketResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.APIVersion != remoteAPIVersion || created.Ticket.Type != kanban.BacklogTodo || created.Ticket.Key != "TODO-001" {
		t.Fatalf("unexpected created automation ticket: %+v", created)
	}

	explore := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/automation/explore/"+created.Ticket.Key, nil)
	exploreRecorder := httptest.NewRecorder()
	handler.ServeHTTP(exploreRecorder, explore)
	if exploreRecorder.Code != http.StatusOK {
		t.Fatalf("expected explore 200, got %d: %s", exploreRecorder.Code, exploreRecorder.Body.String())
	}
	var response remoteTicketResponse
	if err := json.Unmarshal(exploreRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.APIVersion != remoteAPIVersion || response.Project.ID != project.ID || response.Ticket.Key != created.Ticket.Key {
		t.Fatalf("unexpected remote ticket response: %+v", response)
	}
	if response.Ticket.Owner != "PM" || response.Ticket.Execution.State != "backlog" || response.Ticket.Tasks == nil || response.Ticket.Dependencies == nil || response.Ticket.Artifacts == nil {
		t.Fatalf("expected deterministic backlog execution state and an empty task array: %+v", response.Ticket)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/automation/status?type=todo&status=backlog", nil)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected status list 200, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse remoteTicketListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if listResponse.Summary.Total != 1 || len(listResponse.Tickets) != 1 {
		t.Fatalf("unexpected filtered status response: %+v", listResponse)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/automation/backlog", bytes.NewBufferString(`{"type":"unknown","title":"Invalid ticket","description":"This request has an invalid ticket type."}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(invalidRecorder.Body.Bytes(), []byte(`"code":"invalid_backlog_type"`)) || !bytes.Contains(invalidRecorder.Body.Bytes(), []byte(`type must be todo, feature, or bug`)) {
		t.Fatalf("expected a stable invalid type error, got %d: %s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}

func TestAutomationAutopilotPlansApprovesAndSchedulesSequence(t *testing.T) {
	handler, project, boards := newRemoteControlTestServer(t)
	_, item, err := boards.AddBacklogItem(context.Background(), project.ID, project.Name, kanban.BacklogDraft{
		Type: kanban.BacklogFeature, Source: "remote-cli", Title: "Remote automation bridge",
		Description:    "Implement and verify a local remote automation bridge for ProductCrew.",
		DeliveryTarget: "backend", AcceptanceCriteria: []string{"Remote automation is deterministic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"id":"` + item.Key + `","reviewer":"Remote integration test"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/automation/autopilot", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected Autopilot request 202, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var started remoteAutopilotResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if len(started.Results) != 1 || started.Results[0].Action != "planning_started" {
		t.Fatalf("unexpected Autopilot start response: %+v", started)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		board, getErr := boards.GetProjectBoard(context.Background(), project.ID)
		if getErr == nil {
			plan, found := findRemotePlan(board, started.Results[0].PlanID)
			if found && plan.Status == kanban.PlanningApproved && plan.Sequence != nil {
				if plan.Sequence.Status != kanban.PlanSequenceScheduled && plan.Sequence.Status != kanban.PlanSequenceRunning && plan.Sequence.Status != kanban.PlanSequenceBlocked {
					t.Fatalf("unexpected sequence status: %+v", plan.Sequence)
				}
				if len(plan.Reviews) != 1 || plan.Reviews[0].Reviewer != "Remote integration test" {
					t.Fatalf("Autopilot approval was not recorded: %+v", plan.Reviews)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Autopilot did not approve and schedule the generated plan")
}

func TestAutomationAutopilotAllStartsEveryEligibleBacklogItemDeterministically(t *testing.T) {
	handler, project, boards := newRemoteControlTestServer(t)
	_, first, err := boards.AddBacklogItem(context.Background(), project.ID, project.Name, kanban.BacklogDraft{
		Type: kanban.BacklogFeature, Source: "remote-cli", Title: "First eligible ticket",
		Description: "First backlog item eligible for Autopilot all.", DeliveryTarget: "backend",
		AcceptanceCriteria: []string{"Autopilot all starts this ticket"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := boards.AddBacklogItem(context.Background(), project.ID, project.Name, kanban.BacklogDraft{
		Type: kanban.BacklogBug, Source: "remote-cli", Title: "Second eligible ticket",
		Description: "Second backlog item eligible for Autopilot all.", DeliveryTarget: "backend", Severity: "low",
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"all":true,"reviewer":"Remote integration test"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/automation/autopilot", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected Autopilot --all 202, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var started remoteAutopilotResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if len(started.Results) != 2 {
		t.Fatalf("expected Autopilot --all to target both eligible tickets, got %+v", started.Results)
	}
	if started.Results[0].ID != first.ID || started.Results[1].ID != second.ID {
		t.Fatalf("expected Autopilot --all to process tickets in deterministic creation order, got %+v", started.Results)
	}
	for _, result := range started.Results {
		if result.Action != "planning_started" {
			t.Fatalf("expected every eligible ticket to start planning, got %+v", result)
		}
	}
}

func newRemoteControlTestServer(t *testing.T) (http.Handler, project, *kanban.Service) {
	t.Helper()
	baseDirectory, err := os.MkdirTemp("", "remote-control-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if removeErr := os.RemoveAll(baseDirectory); removeErr == nil || time.Now().After(deadline) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	dataDirectory := filepath.Join(baseDirectory, "data")
	projectDirectory := filepath.Join(baseDirectory, "remote-control")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	created, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})
	return handler, created, boards
}
