package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func newEmailTestServer(t *testing.T) (*projectService, http.Handler) {
	t.Helper()
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	directory := t.TempDir()
	projects, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	server := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, newBoardService(projects), nil)
	return projects, server
}

// fakeSMTPServer accepts a single plain-text (no TLS/AUTH) SMTP session on an
// ephemeral localhost port so tests exercise the real SMTP transport without
// needing an external mail provider.
func fakeSMTPServer(t *testing.T) (host string, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
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
	}()

	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func TestEmailSettingsDefaultsToHighSignalPreferences(t *testing.T) {
	projects, _ := newEmailTestServer(t)
	settings, err := projects.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	prefs := settings.EmailNotifications.EventPreferences
	if !prefs.TaskCompleted || !prefs.BlockedNeedsAttention || !prefs.QAResults || !prefs.BuildVerificationFailed || !prefs.ApprovalRequired {
		t.Fatalf("expected all event preferences to default to true, got %+v", prefs)
	}
	if settings.EmailNotifications.SecretConfigured {
		t.Fatal("expected a fresh install to report no configured secret")
	}
	if settings.EmailNotifications.LastTestResult != "never" {
		t.Fatalf("expected lastTestResult to be 'never', got %q", settings.EmailNotifications.LastTestResult)
	}
	if settings.EmailNotifications.ReadyToEnable {
		t.Fatal("expected a fresh install not to be ready to enable")
	}
}

func TestUpdateEmailSettingsKeepsPasswordOutOfJSON(t *testing.T) {
	projects, _ := newEmailTestServer(t)

	updated, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "smtp.example.com",
		SMTPPort:         587,
		SMTPSecurity:     "starttls",
		SMTPUsername:     "smtp-user",
		Password:         "super-secret-password",
		EventPreferences: defaultEmailEventPreferences(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.EmailNotifications.SecretConfigured {
		t.Fatal("expected secretConfigured to be true after saving a password")
	}

	data, err := os.ReadFile(filepath.Join(projects.directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret-password") {
		t.Fatal("settings.json must never contain the SMTP password")
	}
	raw, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-password") {
		t.Fatal("API response must never contain the SMTP password")
	}
}

func TestEnableEmailNotificationsRequiresPassingTestSinceLastChange(t *testing.T) {
	projects, _ := newEmailTestServer(t)

	saved, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "smtp.example.com",
		SMTPPort:         587,
		SMTPSecurity:     "starttls",
		EventPreferences: defaultEmailEventPreferences(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.EmailNotifications.ReadyToEnable {
		t.Fatal("expected not ready to enable before any test email has passed")
	}

	_, err = projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled:          true,
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "smtp.example.com",
		SMTPPort:         587,
		SMTPSecurity:     "starttls",
		EventPreferences: defaultEmailEventPreferences(),
	})
	if err == nil {
		t.Fatal("expected enabling notifications without a passing test to fail")
	}
}

func TestSendTestEmailPersistsFailureWithoutBlocking(t *testing.T) {
	projects, _ := newEmailTestServer(t)

	if _, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "127.0.0.1",
		SMTPPort:         1, // nothing listens on port 1
		SMTPSecurity:     "none",
		EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := projects.sendTestEmail(context.Background())
	if err != nil {
		t.Fatalf("expected a reachable-but-failing send not to error, got %v", err)
	}
	if result.Success {
		t.Fatal("expected the test send to fail against an unreachable host")
	}
	if result.Message == "" {
		t.Fatal("expected a failure message to be surfaced")
	}

	settings, err := projects.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.EmailNotifications.LastTestResult != "failed" {
		t.Fatalf("expected lastTestResult to be persisted as 'failed', got %q", settings.EmailNotifications.LastTestResult)
	}
	if settings.EmailNotifications.LastTestError == "" {
		t.Fatal("expected lastTestError to be persisted")
	}
}

func TestSendTestEmailPassesAndUnlocksEnable(t *testing.T) {
	projects, _ := newEmailTestServer(t)
	host, port := fakeSMTPServer(t)

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

	result, err := projects.sendTestEmail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected the test send to succeed, got message %q", result.Message)
	}
	if !result.Sent.ReadyToEnable {
		t.Fatal("expected readyToEnable to be true immediately after a passing test")
	}

	enabled, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled:          true,
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         host,
		SMTPPort:         port,
		SMTPSecurity:     "none",
		EventPreferences: defaultEmailEventPreferences(),
	})
	if err != nil {
		t.Fatalf("expected enabling to succeed after a passing test, got %v", err)
	}
	if !enabled.EmailNotifications.Enabled {
		t.Fatal("expected notifications to be enabled")
	}

	// Changing a delivery field after a passing test re-locks readiness.
	changed, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		Enabled:          false,
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         host,
		SMTPPort:         port + 1,
		SMTPSecurity:     "none",
		EventPreferences: defaultEmailEventPreferences(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.EmailNotifications.ReadyToEnable {
		t.Fatal("expected changing SMTP port to invalidate the passing test fingerprint")
	}
}

func TestPutEmailSettingsHandlerRejectsInvalidDestination(t *testing.T) {
	_, server := newEmailTestServer(t)

	body := mustJSON(t, updateEmailNotificationSettingsRequest{
		DestinationEmail: "not-an-email",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "smtp.example.com",
		SMTPPort:         587,
		SMTPSecurity:     "starttls",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/settings/email", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for an invalid destination email, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostEmailTestHandlerReturns200OnFailedSend(t *testing.T) {
	projects, server := newEmailTestServer(t)
	if _, err := projects.updateEmailNotificationSettings(updateEmailNotificationSettingsRequest{
		DestinationEmail: "ops@example.com",
		SenderAddress:    "noreply@example.com",
		SMTPHost:         "127.0.0.1",
		SMTPPort:         1,
		SMTPSecurity:     "none",
		EventPreferences: defaultEmailEventPreferences(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/email/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even for a failed send, got %d: %s", rec.Code, rec.Body.String())
	}
	var result emailTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected the handler to report a failed send")
	}
}

func TestEmailConfigFingerprintChangesWithSecretVersion(t *testing.T) {
	base := emailNotificationSettingsFile{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPSecurity: "starttls"}
	before := emailConfigFingerprint(base)
	base.SecretVersion++
	after := emailConfigFingerprint(base)
	if before == after {
		t.Fatal("expected the fingerprint to change when the secret version changes")
	}
}
