package web

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// fakeSMTPServerMulti is fakeSMTPServer's multi-connection sibling: it keeps
// accepting sessions until the listener closes, for tests that need more
// than one send (e.g. a test-email followed by a live dispatch send).
func fakeSMTPServerMulti(t *testing.T) (host string, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveFakeSMTPSession(conn)
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func serveFakeSMTPSession(conn net.Conn) {
	defer conn.Close()
	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)
	_, _ = writer.WriteString("220 localhost ESMTP\r\n")
	_ = writer.Flush()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			_, _ = writer.WriteString("250 localhost\r\n")
		case strings.HasPrefix(command, "MAIL FROM"):
			_, _ = writer.WriteString("250 OK\r\n")
		case strings.HasPrefix(command, "RCPT TO"):
			_, _ = writer.WriteString("250 OK\r\n")
		case strings.HasPrefix(command, "DATA"):
			_, _ = writer.WriteString("354 End data with <CR><LF>.<CR><LF>\r\n")
			_ = writer.Flush()
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil || strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
			}
			_, _ = writer.WriteString("250 OK: queued\r\n")
		case strings.HasPrefix(command, "QUIT"):
			_, _ = writer.WriteString("221 Bye\r\n")
			_ = writer.Flush()
			return
		default:
			_, _ = writer.WriteString("500 unrecognized command\r\n")
		}
		_ = writer.Flush()
	}
}

func newEmailDispatchTestServer(t *testing.T) *server {
	t.Helper()
	directory := t.TempDir()
	projects, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	bundle := newRepositoryBundleOrPanic(projects)
	logger, _, err := observability.NewLogger(projects.directory)
	if err != nil {
		t.Fatal(err)
	}
	return &server{
		projectService: projects,
		observability:  observability.NewService(bundle.Events, logger),
	}
}

func enableEmailNotificationsForDispatch(t *testing.T, projects *projectService, host string, port int) {
	t.Helper()
	if _, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         host,
		SMTPPort:         port,
		SMTPSecurity:     "none",
		Password:         "irrelevant-since-no-auth",
		EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.sendTestEmail(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled:          true,
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         host,
		SMTPPort:         port,
		SMTPSecurity:     "none",
		EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEmailCategoryForNotificationMapsHighSignalKinds(t *testing.T) {
	cases := []struct {
		kind     string
		level    notifications.Level
		want     emailEventCategory
		wantSend bool
	}{
		{"developer_completed", notifications.LevelSuccess, emailEventTaskCompleted, true},
		{"designer_failed", notifications.LevelError, emailEventBlockedNeedsAttention, true},
		{"qa_bugs_found", notifications.LevelWarning, emailEventQAResults, true},
		{"planning_ready", notifications.LevelSuccess, emailEventApprovalRequired, true},
		{"build_verification", notifications.LevelError, emailEventBuildVerificationFailed, true},
		{"build_verification", notifications.LevelSuccess, "", false},
		{"toolchain", notifications.LevelSuccess, "", false},
	}
	for _, testCase := range cases {
		category, ok := emailCategoryForNotification(notifications.Draft{Kind: testCase.kind, Level: testCase.level})
		if ok != testCase.wantSend || category != testCase.want {
			t.Fatalf("kind=%s level=%s: got (%q, %v), want (%q, %v)", testCase.kind, testCase.level, category, ok, testCase.want, testCase.wantSend)
		}
	}
}

func TestDispatchEmailSkipsWhenNotificationsDisabled(t *testing.T) {
	srv := newEmailDispatchTestServer(t)
	srv.dispatchEmailForNotification(context.Background(), notifications.Draft{Kind: "developer_completed", Level: notifications.LevelSuccess, Title: "PROJ-1 implementation completed"}, notifications.Notification{})

	settings, err := srv.projectService.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastSendFailureSummary != "" {
		t.Fatal("expected no send attempt (and thus no failure) when email notifications are disabled")
	}
}

func TestDispatchEmailSkipsWhenEventPreferenceDisabled(t *testing.T) {
	srv := newEmailDispatchTestServer(t)
	host, port := fakeSMTPServerMulti(t)
	enableEmailNotificationsForDispatch(t, srv.projectService, host, port)

	prefs := defaultEmailEventPreferences()
	prefs.TaskCompleted = false
	if _, err := srv.projectService.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled:          true,
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         host,
		SMTPPort:         port,
		SMTPSecurity:     "none",
		EventPreferences: prefs,
	}); err != nil {
		t.Fatal(err)
	}

	// A second fake SMTP server is not started, so any attempted send would
	// fail to connect; asserting no failure is recorded proves the send was
	// skipped rather than attempted and swallowed.
	srv.dispatchEmailForNotification(context.Background(), notifications.Draft{Kind: "developer_completed", Level: notifications.LevelSuccess, Title: "PROJ-1 implementation completed"}, notifications.Notification{})

	settings, err := srv.projectService.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastSendFailureSummary != "" {
		t.Fatalf("expected no send attempt for a disabled event preference, got failure %q", settings.EmailNotifications.LastSendFailureSummary)
	}
}

func TestDispatchEmailSendsForEnabledCategory(t *testing.T) {
	srv := newEmailDispatchTestServer(t)
	host, port := fakeSMTPServerMulti(t)
	enableEmailNotificationsForDispatch(t, srv.projectService, host, port)

	srv.dispatchEmailForNotification(context.Background(), notifications.Draft{
		Kind: "developer_completed", Level: notifications.LevelSuccess,
		Title: "PROJ-1 implementation completed", Message: "All acceptance criteria met",
		ProjectID: "project-1", ProjectName: "Commerce", EntityID: "task-1", Route: "/work-items",
	}, notifications.Notification{})

	settings, err := srv.projectService.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastSendFailureSummary != "" {
		t.Fatalf("expected the send against the fake SMTP server to succeed, got failure %q", settings.EmailNotifications.LastSendFailureSummary)
	}
}

func TestDispatchEmailRecordsFailureWithoutBlocking(t *testing.T) {
	srv := newEmailDispatchTestServer(t)
	host, port := fakeSMTPServerMulti(t)
	enableEmailNotificationsForDispatch(t, srv.projectService, host, port)

	// Repoint at a port nothing listens on, without disabling notifications
	// (staying enabled->enabled skips the passing-test gate), so the next
	// dispatch attempt fails to connect.
	if _, err := srv.projectService.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled: true, DestinationEmail: "ops@example.com", SenderAddress: "noreply@example.com",
		SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPSecurity: "none", EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}

	srv.dispatchEmailForNotification(context.Background(), notifications.Draft{
		Kind: "developer_failed", Level: notifications.LevelError,
		Title: "Developer needs attention", Message: "Build failed", ProjectID: "project-1", EntityID: "task-1", Route: "/work-items",
	}, notifications.Notification{})

	settings, err := srv.projectService.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastSendFailureSummary == "" {
		t.Fatal("expected a send failure to be recorded for an unreachable SMTP host")
	}
	if settings.EmailNotifications.LastSendFailureAt == nil {
		t.Fatal("expected a send failure timestamp to be recorded")
	}

	// A subsequent successful send against the original working server
	// clears the recorded failure state.
	if _, err := srv.projectService.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled: true, DestinationEmail: "ops@example.com", SenderAddress: "noreply@example.com",
		SMTPHost: host, SMTPPort: port, SMTPSecurity: "none", EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}
	srv.dispatchEmailForNotification(context.Background(), notifications.Draft{
		Kind: "developer_failed", Level: notifications.LevelError,
		Title: "Developer needs attention", Message: "Build failed", ProjectID: "project-1", EntityID: "task-1", Route: "/work-items",
	}, notifications.Notification{})

	settings, err = srv.projectService.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastSendFailureSummary != "" {
		t.Fatal("expected the recorded failure to be cleared after a subsequent successful send")
	}
}

func TestBuildEmailLinkMatchesFrontendRouting(t *testing.T) {
	link := buildEmailLink("", "/work-items", "project-1", "task-1")
	want := defaultEmailAppBaseURL + "/work-items?projectId=project-1&taskId=task-1"
	if link != want {
		t.Fatalf("got %q, want %q", link, want)
	}

	atlasLink := buildEmailLink("https://example.com/", "/project-atlas", "project-1", "task-1")
	if atlasLink != "https://example.com/project-atlas?projectId=project-1" {
		t.Fatalf("expected taskId to be omitted for /project-atlas, got %q", atlasLink)
	}
}
