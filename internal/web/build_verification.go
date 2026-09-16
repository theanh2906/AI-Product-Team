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

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

type buildRunHub struct {
	mu          sync.Mutex
	runs        map[string]buildverify.Run
	subscribers map[string]map[chan buildverify.Run]struct{}
}

func newBuildRunHub() *buildRunHub {
	return &buildRunHub{runs: map[string]buildverify.Run{}, subscribers: map[string]map[chan buildverify.Run]struct{}{}}
}
func (h *buildRunHub) publish(run buildverify.Run) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runs[run.ID] = run
	for channel := range h.subscribers[run.ID] {
		select {
		case channel <- run:
		default:
		}
	}
}
func (h *buildRunHub) snapshot(id string) (buildverify.Run, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	return run, ok
}
func (h *buildRunHub) subscribe(id string) (<-chan buildverify.Run, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := make(chan buildverify.Run, 4)
	if h.subscribers[id] == nil {
		h.subscribers[id] = map[chan buildverify.Run]struct{}{}
	}
	h.subscribers[id][channel] = struct{}{}
	return channel, func() { h.mu.Lock(); defer h.mu.Unlock(); delete(h.subscribers[id], channel) }
}

func (s *server) getBuildProfile(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	profile, err := s.buildProfiles.Get(r.Context(), projectID)
	if errors.Is(err, buildverify.ErrNotFound) {
		writeJSON(w, http.StatusOK, buildverify.Profile{ProjectID: projectID, Status: "not_detected", Actions: []buildverify.Action{}, ConfigFiles: []string{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) detectBuildProfile(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	started := time.Now()
	s.observability.Record(observability.Event{Category: "build", Name: "build.detection.started", Message: "Build action detection started", ProjectID: projectID, EntityType: "build_profile", EntityID: projectID, Stage: "detection", Outcome: "running"})
	s.agentMu.Lock()
	profile, err := s.buildDetector.Detect(r.Context(), project.ID, project.Name, project.Path)
	s.agentMu.Unlock()
	if err == nil {
		if previous, previousErr := s.buildProfiles.Get(r.Context(), projectID); previousErr == nil && containsBuildAction(profile.Actions, previous.SelectedActionID) {
			profile.SelectedActionID = previous.SelectedActionID
		}
		err = s.buildProfiles.Upsert(r.Context(), profile)
	}
	if err != nil {
		s.observability.Record(observability.Event{Level: observability.LevelError, Category: "build", Name: "build.detection.failed", Message: "Build action detection failed", ProjectID: projectID, EntityType: "build_profile", EntityID: projectID, Stage: "detection", Outcome: "failed", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"error": err.Error()}})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.observability.Record(observability.Event{Category: "build", Name: "build.detection.completed", Message: "Build actions detected", ProjectID: projectID, EntityType: "build_profile", EntityID: projectID, Stage: "detection", Outcome: "success", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"actions": len(profile.Actions), "warning": profile.Warning}})
	writeJSON(w, http.StatusOK, profile)
}

type selectBuildActionRequest struct {
	ActionID string `json:"actionId"`
}

func (s *server) selectBuildAction(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	var request selectBuildActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	profile, err := s.buildProfiles.Get(r.Context(), projectID)
	if errors.Is(err, buildverify.ErrNotFound) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Detect build actions before selecting one."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(request.ActionID) == "" {
		profile.SelectedActionID = ""
	} else if !containsBuildAction(profile.Actions, request.ActionID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Selected build action is unavailable."})
		return
	} else {
		profile.SelectedActionID = request.ActionID
	}
	if err := s.buildProfiles.Upsert(r.Context(), profile); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

type startBuildRunRequest struct {
	ActionID string `json:"actionId"`
}

func (s *server) startBuildRun(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	var request startBuildRunRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	profile, err := s.buildProfiles.Get(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Detect build actions before running verification."})
		return
	}
	actionID := strings.TrimSpace(request.ActionID)
	if actionID == "" {
		actionID = profile.SelectedActionID
	}
	if actionID == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Choose a build action in Settings before running verification."})
		return
	}
	action, ok := buildAction(profile.Actions, actionID)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Selected build action is unavailable."})
		return
	}
	run := buildverify.Run{ID: fmt.Sprintf("build-%d", time.Now().UnixNano()), ProjectID: projectID, ActionID: action.ID, ActionLabel: action.Label, Command: strings.Join(append([]string{action.Executable}, action.Arguments...), " "), Status: "queued", StartedAt: time.Now().UTC(), ExitCode: -1}
	profile.LastRun = &run
	if err := s.buildProfiles.Upsert(r.Context(), profile); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.buildRuns.publish(run)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		s.agentMu.Lock()
		defer s.agentMu.Unlock()
		run.Status = "running"
		s.buildRuns.publish(run)
		profile.LastRun = &run
		_ = s.buildProfiles.Upsert(context.Background(), profile)
		result, runErr := s.buildRunner.Run(ctx, project.Path, projectID, "", action, nil)
		result.ID = run.ID
		s.buildRuns.publish(result)
		profile.LastRun = &result
		_ = s.buildProfiles.Upsert(context.Background(), profile)
		outcome := "success"
		name := "build.execution.passed"
		level := notifications.LevelSuccess
		if runErr != nil {
			outcome = "failed"
			name = "build.execution.failed"
			level = notifications.LevelError
		}
		s.observability.Record(observability.Event{Category: "build", Name: name, Message: result.Reason, ProjectID: projectID, EntityType: "build_run", EntityID: result.ID, Stage: "verification", Outcome: outcome, DurationMS: result.DurationMS, Attributes: map[string]any{"command": result.Command, "exitCode": result.ExitCode, "logPath": result.LogPath}})
		s.notify(notifications.Draft{Level: level, Kind: "build_verification", Title: project.Name + " build verification " + result.Status, Message: result.Reason, ProjectID: projectID, ProjectName: project.Name, EntityID: result.ID, Route: "/work-items"})
	}()
	writeJSON(w, http.StatusAccepted, run)
}

func (s *server) streamBuildRun(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming unavailable."})
		return
	}
	runID := r.PathValue("runID")
	updates, unsubscribe := s.buildRuns.subscribe(runID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if run, exists := s.buildRuns.snapshot(runID); exists {
		writeSSE(w, "run", run)
		flusher.Flush()
	}
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case run := <-updates:
			writeSSE(w, "run", run)
			flusher.Flush()
			if run.Status != "queued" && run.Status != "running" {
				return
			}
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func buildAction(actions []buildverify.Action, id string) (buildverify.Action, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return buildverify.Action{}, false
}

func containsBuildAction(actions []buildverify.Action, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	_, ok := buildAction(actions, id)
	return ok
}
