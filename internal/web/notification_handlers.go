package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func (s *server) getNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.notifications.List(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for index := range items {
		s.enrichNotificationProject(&items[index])
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	value, err := s.notifications.MarkRead(r.Context(), r.PathValue("notificationID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := s.notifications.MarkAllRead(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) clearReadNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.notifications.DeleteRead(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) streamNotifications(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming is not supported."})
		return
	}
	updates, unsubscribe := s.notificationEvents.subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case value := <-updates:
			writeSSE(w, "notification", value)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *server) notify(draft notifications.Draft) {
	if draft.ProjectName == "" && draft.ProjectID != "" {
		if project, err := s.projectService.findProject(draft.ProjectID); err == nil {
			draft.ProjectName = project.Name
		}
	}
	value, err := s.notifications.Create(context.Background(), draft)
	if err != nil {
		s.observability.Record(observability.Event{Level: observability.LevelError, Category: "notification", Name: "notification.persist_failed", Message: "Could not persist notification", ProjectID: draft.ProjectID, EntityID: draft.EntityID, Outcome: "failed", Attributes: map[string]any{"error": err.Error(), "kind": draft.Kind}})
		return
	}
	s.notificationEvents.publish(value)
	go s.dispatchEmailForNotification(context.Background(), draft, value)
}

func (s *server) enrichNotificationProject(value *notifications.Notification) {
	if value.ProjectName != "" || value.ProjectID == "" {
		return
	}
	if project, err := s.projectService.findProject(value.ProjectID); err == nil {
		value.ProjectName = project.Name
	}
}
