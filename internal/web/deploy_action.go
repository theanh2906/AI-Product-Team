package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/deploy"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// deployRunEvent is one SSE payload for a deploy run: either a run status
// snapshot or a single streamed stdout/stderr line, kept separate from
// buildRunHub events so deploy execution never shares state with build
// verification.
type deployRunEvent struct {
	Run     *deploy.Run `json:"run,omitempty"`
	Level   string      `json:"level,omitempty"`
	Message string      `json:"message,omitempty"`
}

type deployRunHub struct {
	mu          sync.Mutex
	runs        map[string]deploy.Run
	subscribers map[string]map[chan deployRunEvent]struct{}
}

func newDeployRunHub() *deployRunHub {
	return &deployRunHub{runs: map[string]deploy.Run{}, subscribers: map[string]map[chan deployRunEvent]struct{}{}}
}

func (h *deployRunHub) publishRun(run deploy.Run) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runs[run.ID] = run
	h.broadcastLocked(run.ID, deployRunEvent{Run: &run})
}

func (h *deployRunHub) publishLog(runID, level, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcastLocked(runID, deployRunEvent{Level: level, Message: message})
}

func (h *deployRunHub) broadcastLocked(runID string, event deployRunEvent) {
	for channel := range h.subscribers[runID] {
		select {
		case channel <- event:
		default:
		}
	}
}

func (h *deployRunHub) snapshot(id string) (deploy.Run, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	return run, ok
}

func (h *deployRunHub) subscribe(id string) (<-chan deployRunEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := make(chan deployRunEvent, 32)
	if h.subscribers[id] == nil {
		h.subscribers[id] = map[chan deployRunEvent]struct{}{}
	}
	h.subscribers[id][channel] = struct{}{}
	return channel, func() { h.mu.Lock(); defer h.mu.Unlock(); delete(h.subscribers[id], channel) }
}

func (s *server) getDeployProfile(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	profile, err := s.deployProfiles.Get(r.Context(), projectID)
	if errors.Is(err, deploy.ErrNotFound) {
		writeJSON(w, http.StatusOK, deploy.Profile{ProjectID: projectID, Status: deploy.StatusNotDetected, Actions: []deploy.Action{}, ConfigFiles: []string{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) detectDeployProfile(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	started := time.Now()
	s.observability.Record(observability.Event{Category: "deploy", Name: "deploy.detection.started", Message: "Deploy action detection started", ProjectID: projectID, EntityType: "deploy_profile", EntityID: projectID, Stage: "detection", Outcome: "running"})
	s.agentMu.Lock()
	profile, err := s.deployDetector.Detect(r.Context(), project.ID, project.Name, project.Path)
	s.agentMu.Unlock()
	if err == nil {
		if previous, previousErr := s.deployProfiles.Get(r.Context(), projectID); previousErr == nil && containsDeployAction(profile.Actions, previous.SelectedActionID) {
			profile.SelectedActionID = previous.SelectedActionID
			profile.Status = deploy.StatusReady
		}
		err = s.deployProfiles.Upsert(r.Context(), profile)
	}
	if err != nil {
		s.observability.Record(observability.Event{Level: observability.LevelError, Category: "deploy", Name: "deploy.detection.failed", Message: "Deploy action detection failed", ProjectID: projectID, EntityType: "deploy_profile", EntityID: projectID, Stage: "detection", Outcome: "failed", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"error": err.Error()}})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.observability.Record(observability.Event{Category: "deploy", Name: "deploy.detection.completed", Message: "Deploy actions detected", ProjectID: projectID, EntityType: "deploy_profile", EntityID: projectID, Stage: "detection", Outcome: "success", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"actions": len(profile.Actions), "warning": profile.Warning}})
	writeJSON(w, http.StatusOK, profile)
}

type selectDeployActionRequest struct {
	ActionID string `json:"actionId"`
}

func (s *server) selectDeployAction(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	var request selectDeployActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	profile, err := s.deployProfiles.Get(r.Context(), projectID)
	if errors.Is(err, deploy.ErrNotFound) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Detect deploy actions before selecting one."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(request.ActionID) == "" {
		profile.SelectedActionID = ""
		profile.Status = deploy.StatusNotConfigured
	} else if !containsDeployAction(profile.Actions, request.ActionID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Selected deploy action is unavailable."})
		return
	} else {
		profile.SelectedActionID = request.ActionID
		profile.Status = deploy.StatusReady
	}
	if err := s.deployProfiles.Upsert(r.Context(), profile); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) startDeployRun(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	profile, err := s.deployProfiles.Get(r.Context(), projectID)
	if err != nil || strings.TrimSpace(profile.SelectedActionID) == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Choose a deploy action in Settings before running a deploy."})
		return
	}
	action, ok := deployAction(profile.Actions, profile.SelectedActionID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Selected deploy action is unavailable."})
		return
	}
	run := deploy.Run{ID: fmt.Sprintf("deploy-%d", time.Now().UnixNano()), ProjectID: projectID, ActionID: action.ID, ActionLabel: action.Label, Command: strings.Join(append([]string{action.Executable}, action.Arguments...), " "), Status: deploy.RunStatusQueued, StartedAt: time.Now().UTC(), ExitCode: -1}
	profile.LastRun = &run
	if err := s.deployProfiles.Upsert(r.Context(), profile); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.deployRuns.publishRun(run)
	s.observability.Record(observability.Event{Category: "deploy", Name: "deploy.execution.started", Message: "Deploy started", ProjectID: projectID, EntityType: "deploy_run", EntityID: run.ID, Stage: "execution", Outcome: "running", Attributes: map[string]any{"command": run.Command}})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		s.agentMu.Lock()
		defer s.agentMu.Unlock()
		run.Status = deploy.RunStatusRunning
		s.deployRuns.publishRun(run)
		profile.LastRun = &run
		_ = s.deployProfiles.Upsert(context.Background(), profile)
		result, runErr := s.deployRunner.Run(ctx, project.Path, projectID, action, func(level, message string) {
			s.deployRuns.publishLog(run.ID, level, message)
		})
		result.ID = run.ID
		s.deployRuns.publishRun(result)
		profile.LastRun = &result
		_ = s.deployProfiles.Upsert(context.Background(), profile)
		outcome := "success"
		name := "deploy.execution.passed"
		level := notifications.LevelSuccess
		if runErr != nil {
			outcome = "failed"
			name = "deploy.execution.failed"
			level = notifications.LevelError
		}
		s.observability.Record(observability.Event{Category: "deploy", Name: name, Message: result.Reason, ProjectID: projectID, EntityType: "deploy_run", EntityID: result.ID, Stage: "execution", Outcome: outcome, DurationMS: result.DurationMS, Attributes: map[string]any{"command": result.Command, "exitCode": result.ExitCode, "logPath": result.LogPath}})
		s.notify(notifications.Draft{Level: level, Kind: "deploy_execution", Title: project.Name + " deploy " + result.Status, Message: result.Reason, ProjectID: projectID, ProjectName: project.Name, EntityID: result.ID, Route: "/work-items"})
	}()
	writeJSON(w, http.StatusAccepted, run)
}

func (s *server) streamDeployRun(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming unavailable."})
		return
	}
	runID := r.PathValue("runID")
	updates, unsubscribe := s.deployRuns.subscribe(runID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if run, exists := s.deployRuns.snapshot(runID); exists {
		writeSSE(w, "run", run)
		flusher.Flush()
	}
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-updates:
			if event.Run != nil {
				writeSSE(w, "run", event.Run)
				flusher.Flush()
				if event.Run.Status != deploy.RunStatusQueued && event.Run.Status != deploy.RunStatusRunning {
					return
				}
				continue
			}
			writeSSE(w, "log", map[string]string{"level": event.Level, "message": event.Message})
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func deployAction(actions []deploy.Action, id string) (deploy.Action, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return deploy.Action{}, false
}

func containsDeployAction(actions []deploy.Action, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	_, ok := deployAction(actions, id)
	return ok
}
