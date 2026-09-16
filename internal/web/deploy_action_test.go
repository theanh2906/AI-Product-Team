package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/deploy"
)

func newDeployTestHandler(t *testing.T, projectDirectory string) (http.Handler, project) {
	t.Helper()
	dataDirectory := t.TempDir()
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, newBoardService(projects), fakeTeamLeadPlanner{})
	return handler, proj
}

func doJSON(t *testing.T, handler http.Handler, method, path, body string) (int, map[string]any, []byte) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	raw := recorder.Body.Bytes()
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return recorder.Code, decoded, raw
}

func TestGetDeployProfileDefaultsToNotDetected(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(projectDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	handler, proj := newDeployTestHandler(t, projectDirectory)
	code, body, _ := doJSON(t, handler, http.MethodGet, "/api/projects/"+proj.ID+"/deploy-action", "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != deploy.StatusNotDetected {
		t.Fatalf("expected not_detected status, got %v", body["status"])
	}
}

func writePackageJSONWithDeployScript(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"sample","scripts":{"deploy":"echo deployed","build":"echo build"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectDeployProfileFindsCandidatesWithoutAutoSelecting(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	writePackageJSONWithDeployScript(t, projectDirectory)
	handler, proj := newDeployTestHandler(t, projectDirectory)

	code, body, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/detect", "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != deploy.StatusNotConfigured {
		t.Fatalf("expected detection to leave status not_configured, got %v", body["status"])
	}
	if body["selectedActionId"] != nil && body["selectedActionId"] != "" {
		t.Fatalf("expected detection to never auto-select an action, got %v", body["selectedActionId"])
	}
	actions, ok := body["actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("expected at least one detected deploy candidate, got %v", body["actions"])
	}
}

func TestStartDeployRunConflictWithoutConfiguredAction(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	writePackageJSONWithDeployScript(t, projectDirectory)
	handler, proj := newDeployTestHandler(t, projectDirectory)

	if code, _, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/detect", ""); code != http.StatusOK {
		t.Fatalf("detect failed with %d", code)
	}
	code, body, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/runs", "{}")
	if code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict without a configured deploy action, got %d: %v", code, body)
	}
}

func TestSelectDeployActionRejectsUnknownCandidate(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	writePackageJSONWithDeployScript(t, projectDirectory)
	handler, proj := newDeployTestHandler(t, projectDirectory)

	if code, _, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/detect", ""); code != http.StatusOK {
		t.Fatalf("detect failed with %d", code)
	}
	code, body, _ := doJSON(t, handler, http.MethodPut, "/api/projects/"+proj.ID+"/deploy-action/action", `{"actionId":"does-not-exist"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown action id, got %d: %v", code, body)
	}
}

func TestSelectAndRunDeployActionSucceeds(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	writePackageJSONWithDeployScript(t, projectDirectory)
	handler, proj := newDeployTestHandler(t, projectDirectory)

	_, detected, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/detect", "")
	actions, _ := detected["actions"].([]any)
	if len(actions) == 0 {
		t.Fatal("expected detected actions")
	}
	first := actions[0].(map[string]any)
	actionID := first["id"].(string)

	code, selected, _ := doJSON(t, handler, http.MethodPut, "/api/projects/"+proj.ID+"/deploy-action/action", `{"actionId":"`+actionID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200 selecting a detected action, got %d: %v", code, selected)
	}
	if selected["status"] != deploy.StatusReady {
		t.Fatalf("expected status ready after explicit selection, got %v", selected["status"])
	}
	if selected["selectedActionId"] != actionID {
		t.Fatalf("expected selectedActionId %q, got %v", actionID, selected["selectedActionId"])
	}

	code, run, raw := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/runs", "{}")
	if code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted starting a configured deploy run, got %d: %s", code, raw)
	}
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("expected run id, got %v", run)
	}

	deadline := time.Now().Add(20 * time.Second)
	var finalProfile map[string]any
	for time.Now().Before(deadline) {
		_, profile, _ := doJSON(t, handler, http.MethodGet, "/api/projects/"+proj.ID+"/deploy-action", "")
		lastRun, ok := profile["lastRun"].(map[string]any)
		if ok && lastRun["id"] == runID {
			if status, _ := lastRun["status"].(string); status == deploy.RunStatusPassed || status == deploy.RunStatusFailed {
				finalProfile = profile
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if finalProfile == nil {
		t.Fatal("deploy run did not reach a terminal status in time")
	}
	lastRun := finalProfile["lastRun"].(map[string]any)
	if lastRun["status"] != deploy.RunStatusPassed {
		t.Fatalf("expected deploy run to pass, got %+v", lastRun)
	}

	streamCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	streamRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+proj.ID+"/deploy-action/runs/"+runID+"/events", nil).WithContext(streamCtx)
	streamRecorder := httptest.NewRecorder()
	handler.ServeHTTP(streamRecorder, streamRequest)
	if !strings.Contains(streamRecorder.Body.String(), "event: run") {
		t.Fatalf("expected the SSE stream to include a run status event, got %q", streamRecorder.Body.String())
	}
	if !strings.Contains(streamRecorder.Body.String(), deploy.RunStatusPassed) {
		t.Fatalf("expected the SSE stream to expose the passed status, got %q", streamRecorder.Body.String())
	}
}

func TestDeployRunHubBroadcastsStatusAndLogEvents(t *testing.T) {
	hub := newDeployRunHub()
	updates, unsubscribe := hub.subscribe("run-1")
	defer unsubscribe()

	hub.publishRun(deploy.Run{ID: "run-1", Status: deploy.RunStatusRunning})
	hub.publishLog("run-1", "info", "$ npm run deploy")
	hub.publishLog("run-1", "error", "warning: something")
	hub.publishRun(deploy.Run{ID: "run-1", Status: deploy.RunStatusPassed})

	var events []deployRunEvent
	deadline := time.Now().Add(2 * time.Second)
	for len(events) < 4 && time.Now().Before(deadline) {
		select {
		case event := <-updates:
			events = append(events, event)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Run == nil || events[0].Run.Status != deploy.RunStatusRunning {
		t.Fatalf("expected first event to be the running status, got %+v", events[0])
	}
	if events[1].Run != nil || events[1].Message != "$ npm run deploy" {
		t.Fatalf("expected second event to be a stdout line, got %+v", events[1])
	}
	if events[2].Run != nil || events[2].Level != "error" {
		t.Fatalf("expected third event to be a stderr line, got %+v", events[2])
	}
	if events[3].Run == nil || events[3].Run.Status != deploy.RunStatusPassed {
		t.Fatalf("expected fourth event to be the passed status, got %+v", events[3])
	}
}

func TestDeployConfigurationDoesNotAffectBuildVerificationProfile(t *testing.T) {
	projectDirectory := filepath.Join(t.TempDir(), "app")
	writePackageJSONWithDeployScript(t, projectDirectory)
	handler, proj := newDeployTestHandler(t, projectDirectory)

	// Establish a build verification profile first, mirroring an already
	// configured build action, then configure and run deploy independently.
	code, buildProfile, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/build-verification/detect", "")
	if code != http.StatusOK {
		t.Fatalf("build detect failed with %d: %v", code, buildProfile)
	}
	buildActions, _ := buildProfile["actions"].([]any)
	if len(buildActions) == 0 {
		t.Fatal("expected at least one detected build candidate")
	}
	buildActionID := buildActions[0].(map[string]any)["id"].(string)
	if code, selected, _ := doJSON(t, handler, http.MethodPut, "/api/projects/"+proj.ID+"/build-verification/action", `{"actionId":"`+buildActionID+`"}`); code != http.StatusOK {
		t.Fatalf("selecting build action failed with %d: %v", code, selected)
	}

	_, deployDetected, _ := doJSON(t, handler, http.MethodPost, "/api/projects/"+proj.ID+"/deploy-action/detect", "")
	deployActions, _ := deployDetected["actions"].([]any)
	if len(deployActions) == 0 {
		t.Fatal("expected at least one detected deploy candidate")
	}
	deployActionID := deployActions[0].(map[string]any)["id"].(string)
	if code, selected, _ := doJSON(t, handler, http.MethodPut, "/api/projects/"+proj.ID+"/deploy-action/action", `{"actionId":"`+deployActionID+`"}`); code != http.StatusOK {
		t.Fatalf("selecting deploy action failed with %d: %v", code, selected)
	}

	_, finalBuildProfile, _ := doJSON(t, handler, http.MethodGet, "/api/projects/"+proj.ID+"/build-verification", "")
	if finalBuildProfile["selectedActionId"] != buildActionID {
		t.Fatalf("expected build verification selection to remain %q untouched by deploy configuration, got %v", buildActionID, finalBuildProfile["selectedActionId"])
	}
	if finalBuildProfile["status"] != "ready" {
		t.Fatalf("expected build verification profile to remain ready, got %v", finalBuildProfile["status"])
	}
}
