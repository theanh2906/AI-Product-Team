package web

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/email"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// emailEventCategory groups notification kinds into the five high-signal
// preference toggles from the Email Notifications spec.
type emailEventCategory string

const (
	emailEventTaskCompleted           emailEventCategory = "taskCompleted"
	emailEventBlockedNeedsAttention   emailEventCategory = "blockedNeedsAttention"
	emailEventQAResults               emailEventCategory = "qaResults"
	emailEventBuildVerificationFailed emailEventCategory = "buildVerificationFailed"
	emailEventApprovalRequired        emailEventCategory = "approvalRequired"
)

// notificationKindEmailCategory maps every notification.Draft.Kind emitted by
// planning/design/development/QA/build flows to the email preference that
// gates it. Kinds absent from this map (e.g. "toolchain") never send email.
var notificationKindEmailCategory = map[string]emailEventCategory{
	"planning_ready":                emailEventApprovalRequired,
	"planning_failed":               emailEventBlockedNeedsAttention,
	"planning_not_feasible":         emailEventBlockedNeedsAttention,
	"planning_skipped_already_done": emailEventTaskCompleted,
	"designer_completed":            emailEventTaskCompleted,
	"designer_failed":               emailEventBlockedNeedsAttention,
	"developer_completed":           emailEventTaskCompleted,
	"developer_failed":              emailEventBlockedNeedsAttention,
	"build_verification":            emailEventBuildVerificationFailed,
	"build_verification_failed":     emailEventBuildVerificationFailed,
	"qa_passed":                     emailEventQAResults,
	"qa_bugs_found":                 emailEventQAResults,
	"qa_verification_blocked":       emailEventQAResults,
	"qa_failed":                     emailEventBlockedNeedsAttention,
	"autopilot_failed":              emailEventBlockedNeedsAttention,
}

func (p emailEventPreferences) enabledFor(category emailEventCategory) bool {
	switch category {
	case emailEventTaskCompleted:
		return p.TaskCompleted
	case emailEventBlockedNeedsAttention:
		return p.BlockedNeedsAttention
	case emailEventQAResults:
		return p.QAResults
	case emailEventBuildVerificationFailed:
		return p.BuildVerificationFailed
	case emailEventApprovalRequired:
		return p.ApprovalRequired
	default:
		return false
	}
}

// emailCategoryForNotification resolves the preference category for a draft,
// or false when the kind is not one of the high-signal email events. The
// generic "build_verification" kind fires on both pass and fail, but only
// the failure case belongs to the buildVerificationFailed category.
func emailCategoryForNotification(draft notifications.Draft) (emailEventCategory, bool) {
	category, ok := notificationKindEmailCategory[draft.Kind]
	if !ok {
		return "", false
	}
	if draft.Kind == "build_verification" && draft.Level != notifications.LevelError && draft.Level != notifications.LevelWarning {
		return "", false
	}
	return category, true
}

const defaultEmailAppBaseURL = "http://127.0.0.1:8081"

// buildEmailLink mirrors the frontend's notification click-through routing
// (app-shell.ts navigateToNotificationTarget): route (default /work-items)
// plus projectId/taskId query params, taskId omitted for /project-atlas.
func buildEmailLink(baseURL, route, projectID, entityID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultEmailAppBaseURL
	}
	path := strings.TrimSpace(route)
	if path == "" {
		path = "/work-items"
	}
	if projectID == "" {
		return base + path
	}
	query := url.Values{}
	query.Set("projectId", projectID)
	if entityID != "" && path != "/project-atlas" {
		query.Set("taskId", entityID)
	}
	return base + path + "?" + query.Encode()
}

func buildEmailMessage(cfg emailNotificationSettingsFile, draft notifications.Draft) email.Message {
	project := draft.ProjectName
	if project == "" {
		project = "ProductCrew"
	}
	status := string(draft.Level)
	if status == "" {
		status = string(notifications.LevelInfo)
	}
	link := buildEmailLink(cfg.AppBaseURL, draft.Route, draft.ProjectID, draft.EntityID)
	body := fmt.Sprintf(
		"Project: %s\nTask/Plan: %s\nStatus: %s\nReason: %s\nLink: %s\n",
		project, draft.Title, status, draft.Message, link,
	)
	return email.Message{
		To: cfg.DestinationEmail, FromAddress: cfg.SenderAddress, FromName: cfg.SenderName,
		Subject: fmt.Sprintf("[ProductCrew] %s", draft.Title),
		Body:    body,
	}
}

// dispatchEmailForNotification best-effort emails a just-created notification
// when it belongs to an enabled high-signal category. It never returns an
// error: failures are logged to observability and recorded on settings for
// Settings-page surfacing, and must never interrupt the calling agent flow.
func (s *server) dispatchEmailForNotification(ctx context.Context, draft notifications.Draft, notification notifications.Notification) {
	category, ok := emailCategoryForNotification(draft)
	if !ok {
		return
	}

	cfg, password, err := s.projectService.emailDispatchConfig()
	if err != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "email", Name: "email.dispatch_config_failed",
			Message: "Could not read email notification settings", ProjectID: draft.ProjectID, EntityID: draft.EntityID,
			Outcome: "failed", Attributes: map[string]any{"error": err.Error(), "kind": draft.Kind},
		})
		return
	}
	if !cfg.Enabled || !cfg.EventPreferences.enabledFor(category) {
		return
	}

	transport := email.NewSMTPTransport(email.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Security: email.Security(cfg.SMTPSecurity),
		Username: cfg.SMTPUsername, Password: password,
	})
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if sendErr := transport.Send(sendCtx, buildEmailMessage(cfg, draft)); sendErr != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "email", Name: "email.notification_send_failed",
			Message: "Failed to send notification email", ProjectID: draft.ProjectID, EntityID: draft.EntityID,
			Outcome: "failed", Attributes: map[string]any{"error": sendErr.Error(), "kind": draft.Kind},
		})
		if recordErr := s.projectService.recordEmailSendFailure(sendErr.Error()); recordErr != nil {
			s.observability.Record(observability.Event{
				Level: observability.LevelError, Category: "email", Name: "email.dispatch_failure_persist_failed",
				Message: "Could not persist email send failure state", Outcome: "failed",
				Attributes: map[string]any{"error": recordErr.Error()},
			})
		}
		return
	}
	if clearErr := s.projectService.clearEmailSendFailure(); clearErr != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "email", Name: "email.dispatch_state_clear_failed",
			Message: "Could not clear prior email send failure state", Outcome: "failed",
			Attributes: map[string]any{"error": clearErr.Error()},
		})
	}
}
