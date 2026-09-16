package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

func (s *server) getSystemTools(w http.ResponseWriter, r *http.Request) {
	refresh := strings.EqualFold(r.URL.Query().Get("refresh"), "true")
	writeJSON(w, http.StatusOK, s.toolchain.Snapshot(r.Context(), refresh))
}

func (s *server) runSystemToolAction(w http.ResponseWriter, r *http.Request) {
	toolID := r.PathValue("toolID")
	action := r.PathValue("action")
	// Tool setup belongs to the local service, so closing or reloading the browser
	// must not terminate an in-flight package-manager operation.
	operationContext, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	tool, err := s.toolchain.RunAction(operationContext, toolID, action)
	if err != nil {
		s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "toolchain", Title: "CLI setup needs attention", Message: err.Error(), Route: "/settings"})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	completedAction := "installed"
	if action == "update" {
		completedAction = "updated"
	}
	s.notify(notifications.Draft{Level: notifications.LevelSuccess, Kind: "toolchain", Title: tool.Name + " is ready", Message: "The CLI tool was " + completedAction + " successfully.", Route: "/settings"})
	writeJSON(w, http.StatusOK, tool)
}
