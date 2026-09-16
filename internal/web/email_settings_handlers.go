package web

import (
	"net/http"

	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func (s *server) putEmailNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var request updateEmailNotificationSettingsRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateEmailNotificationSettings(request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) postEmailNotificationTest(w http.ResponseWriter, r *http.Request) {
	result, err := s.projectService.sendTestEmail(r.Context())
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if !result.Success {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "email", Name: "email.test_failed",
			Message: "Test email failed to send", CorrelationID: correlationID(r.Context()),
			Outcome: "failed", Attributes: map[string]any{"error": result.Message},
		})
	}
	writeJSON(w, http.StatusOK, result)
}
