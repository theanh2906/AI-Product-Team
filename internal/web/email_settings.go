package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/theanh2906/AI-Product-Team/internal/email"
)

var emailAddressPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// emailEventPreferences controls which high-signal ProductCrew event groups
// trigger an outbound notification email. All default to true so a fresh
// install stays high-signal without extra setup.
type emailEventPreferences struct {
	TaskCompleted           bool `json:"taskCompleted"`
	BlockedNeedsAttention   bool `json:"blockedNeedsAttention"`
	QAResults               bool `json:"qaResults"`
	BuildVerificationFailed bool `json:"buildVerificationFailed"`
	ApprovalRequired        bool `json:"approvalRequired"`
}

func (p emailEventPreferences) any() bool {
	return p.TaskCompleted || p.BlockedNeedsAttention || p.QAResults || p.BuildVerificationFailed || p.ApprovalRequired
}

func defaultEmailEventPreferences() emailEventPreferences {
	return emailEventPreferences{
		TaskCompleted:           true,
		BlockedNeedsAttention:   true,
		QAResults:               true,
		BuildVerificationFailed: true,
		ApprovalRequired:        true,
	}
}

// emailNotificationSettingsFile is the non-secret shape persisted to
// settings.json. The SMTP auth secret is never stored here; it lives only in
// the OS credential vault under smtpCredentialUser. SecretVersion increments
// whenever the stored secret changes so a config fingerprint can detect
// "changed since last passing test" without hashing the secret itself.
type emailNotificationSettingsFile struct {
	Enabled                 bool                  `json:"enabled"`
	DestinationEmail        string                `json:"destinationEmail"`
	SenderName              string                `json:"senderName,omitempty"`
	SenderAddress           string                `json:"senderAddress"`
	SMTPHost                string                `json:"smtpHost"`
	SMTPPort                int                   `json:"smtpPort"`
	SMTPSecurity            string                `json:"smtpSecurity"`
	SMTPUsername            string                `json:"smtpUsername,omitempty"`
	AppBaseURL              string                `json:"appBaseUrl,omitempty"`
	EventPreferences        emailEventPreferences `json:"eventPreferences"`
	SecretVersion           int                   `json:"secretVersion,omitempty"`
	LastTestResult          string                `json:"lastTestResult,omitempty"`
	LastTestAt              *time.Time            `json:"lastTestAt,omitempty"`
	LastTestError           string                `json:"lastTestError,omitempty"`
	LastTestHostPort        string                `json:"lastTestHostPort,omitempty"`
	TestedConfigFingerprint string                `json:"testedConfigFingerprint,omitempty"`
	LastSendFailureAt       *time.Time            `json:"lastSendFailureAt,omitempty"`
	LastSendFailureSummary  string                `json:"lastSendFailureSummary,omitempty"`
}

// emailNotificationSettings is the API response shape. It never includes the
// secret in any form - only secretConfigured, mirroring patConfigured.
type emailNotificationSettings struct {
	Enabled                bool                  `json:"enabled"`
	DestinationEmail       string                `json:"destinationEmail"`
	SenderName             string                `json:"senderName"`
	SenderAddress          string                `json:"senderAddress"`
	SMTPHost               string                `json:"smtpHost"`
	SMTPPort               int                   `json:"smtpPort"`
	SMTPSecurity           string                `json:"smtpSecurity"`
	SMTPUsername           string                `json:"smtpUsername"`
	SecretConfigured       bool                  `json:"secretConfigured"`
	AppBaseURL             string                `json:"appBaseUrl"`
	EventPreferences       emailEventPreferences `json:"eventPreferences"`
	ReadyToEnable          bool                  `json:"readyToEnable"`
	LastTestResult         string                `json:"lastTestResult"`
	LastTestAt             *time.Time            `json:"lastTestAt,omitempty"`
	LastTestError          string                `json:"lastTestError,omitempty"`
	LastTestHostPort       string                `json:"lastTestHostPort,omitempty"`
	LastSendFailureAt      *time.Time            `json:"lastSendFailureAt,omitempty"`
	LastSendFailureSummary string                `json:"lastSendFailureSummary,omitempty"`
}

// applyEmailNotificationDefaults seeds the conservative high-signal default
// event preferences the first time email notifications are touched, and
// defaults the security mode when settings.json predates this field.
func applyEmailNotificationDefaults(value settingsFile) settingsFile {
	e := value.EmailNotifications
	untouched := !e.Enabled && e.SMTPHost == "" && e.DestinationEmail == "" && e.SenderAddress == "" && e.SMTPPort == 0
	if untouched && !e.EventPreferences.any() {
		e.EventPreferences = defaultEmailEventPreferences()
	}
	if strings.TrimSpace(e.SMTPSecurity) == "" {
		e.SMTPSecurity = string(email.SecurityStartTLS)
	}
	value.EmailNotifications = e
	return value
}

func emailConfigFingerprint(e emailNotificationSettingsFile) string {
	parts := strings.Join([]string{
		strings.TrimSpace(strings.ToLower(e.DestinationEmail)),
		strings.TrimSpace(e.SenderName),
		strings.TrimSpace(strings.ToLower(e.SenderAddress)),
		strings.TrimSpace(strings.ToLower(e.SMTPHost)),
		strconv.Itoa(e.SMTPPort),
		strings.TrimSpace(strings.ToLower(e.SMTPSecurity)),
		strings.TrimSpace(e.SMTPUsername),
		strconv.Itoa(e.SecretVersion),
	}, "|")
	sum := sha256.Sum256([]byte(parts))
	return hex.EncodeToString(sum[:])
}

func (s *projectService) emailNotificationSettingsResponse(stored emailNotificationSettingsFile) emailNotificationSettings {
	_, credentialErr := s.credentials.Get(smtpCredentialUser)
	secretConfigured := credentialErr == nil
	lastTestResult := stored.LastTestResult
	if lastTestResult == "" {
		lastTestResult = "never"
	}
	readyToEnable := secretConfigured && stored.LastTestResult == "passed" &&
		stored.TestedConfigFingerprint != "" && stored.TestedConfigFingerprint == emailConfigFingerprint(stored)
	return emailNotificationSettings{
		Enabled: stored.Enabled, DestinationEmail: stored.DestinationEmail,
		SenderName: stored.SenderName, SenderAddress: stored.SenderAddress,
		SMTPHost: stored.SMTPHost, SMTPPort: stored.SMTPPort, SMTPSecurity: stored.SMTPSecurity,
		SMTPUsername: stored.SMTPUsername, SecretConfigured: secretConfigured,
		AppBaseURL: stored.AppBaseURL, EventPreferences: stored.EventPreferences,
		ReadyToEnable: readyToEnable, LastTestResult: lastTestResult,
		LastTestAt: stored.LastTestAt, LastTestError: stored.LastTestError, LastTestHostPort: stored.LastTestHostPort,
		LastSendFailureAt: stored.LastSendFailureAt, LastSendFailureSummary: stored.LastSendFailureSummary,
	}
}

// updateEmailNotificationSettingsRequest carries the "Save delivery settings"
// and "Enable/disable notifications" actions from the Settings UI.
type updateEmailNotificationSettingsRequest struct {
	Enabled          bool                  `json:"enabled"`
	DestinationEmail string                `json:"destinationEmail"`
	SenderName       string                `json:"senderName"`
	SenderAddress    string                `json:"senderAddress"`
	SMTPHost         string                `json:"smtpHost"`
	SMTPPort         int                   `json:"smtpPort"`
	SMTPSecurity     string                `json:"smtpSecurity"`
	SMTPUsername     string                `json:"smtpUsername"`
	AppBaseURL       string                `json:"appBaseUrl"`
	EventPreferences emailEventPreferences `json:"eventPreferences"`
	Password         string                `json:"password"`
	ClearPassword    bool                  `json:"clearPassword"`
}

func validateEmailDeliveryFields(destinationEmail, senderAddress, host, security string, port int) error {
	if strings.TrimSpace(destinationEmail) == "" || !emailAddressPattern.MatchString(destinationEmail) {
		return fmt.Errorf("destination email must be a valid email address")
	}
	if strings.TrimSpace(senderAddress) == "" || !emailAddressPattern.MatchString(senderAddress) {
		return fmt.Errorf("sender address must be a valid email address")
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("SMTP host is required")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	switch email.Security(strings.ToLower(strings.TrimSpace(security))) {
	case email.SecurityNone, email.SecurityStartTLS, email.SecurityTLS:
	default:
		return fmt.Errorf("SMTP security must be one of none, starttls, or tls")
	}
	return nil
}

func (s *projectService) updateEmailNotificationSettings(request updateEmailNotificationSettingsRequest) (settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return settings{}, err
	}

	destinationEmail := strings.TrimSpace(request.DestinationEmail)
	senderAddress := strings.TrimSpace(request.SenderAddress)
	host := strings.TrimSpace(request.SMTPHost)
	security := strings.ToLower(strings.TrimSpace(request.SMTPSecurity))
	if err := validateEmailDeliveryFields(destinationEmail, senderAddress, host, security, request.SMTPPort); err != nil {
		return settings{}, err
	}

	current := stored.EmailNotifications
	current.DestinationEmail = destinationEmail
	current.SenderName = strings.TrimSpace(request.SenderName)
	current.SenderAddress = senderAddress
	current.SMTPHost = host
	current.SMTPPort = request.SMTPPort
	current.SMTPSecurity = security
	current.SMTPUsername = strings.TrimSpace(request.SMTPUsername)
	current.AppBaseURL = strings.TrimSpace(request.AppBaseURL)
	current.EventPreferences = request.EventPreferences

	if request.ClearPassword {
		if err := s.credentials.Delete(smtpCredentialUser); err != nil {
			return settings{}, fmt.Errorf("delete SMTP password from OS keyring: %w", err)
		}
		current.SecretVersion++
	} else if password := request.Password; password != "" {
		if err := s.credentials.Set(smtpCredentialUser, password); err != nil {
			return settings{}, fmt.Errorf("save SMTP password to OS keyring: %w", err)
		}
		current.SecretVersion++
	}

	if request.Enabled && !current.Enabled {
		_, credentialErr := s.credentials.Get(smtpCredentialUser)
		ready := credentialErr == nil && current.LastTestResult == "passed" &&
			current.TestedConfigFingerprint != "" && current.TestedConfigFingerprint == emailConfigFingerprint(current)
		if !ready {
			return settings{}, fmt.Errorf("send a passing test email after the most recent configuration change before enabling notifications")
		}
	}
	current.Enabled = request.Enabled

	stored.EmailNotifications = current
	if err := s.writeJSON("settings.json", stored); err != nil {
		return settings{}, err
	}
	return s.settingsResponse(stored)
}

// emailTestResult reports the outcome of a "Send test email" action, mirroring
// the checkStorageDatasource always-200 pattern: a failed send is a normal
// result the Settings UI displays inline, not an HTTP error.
type emailTestResult struct {
	Success bool                      `json:"success"`
	Message string                    `json:"message,omitempty"`
	Sent    emailNotificationSettings `json:"settings"`
}

// sendTestEmail validates the current delivery configuration, sends one
// message through the configured SMTP transport, and persists the outcome so
// the Settings UI can render Health without a second round trip. It never
// returns an error for a reachable-but-rejected send; only for configuration
// that cannot be attempted at all (missing fields/secret).
func (s *projectService) sendTestEmail(ctx context.Context) (emailTestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return emailTestResult{}, err
	}
	current := stored.EmailNotifications
	if err := validateEmailDeliveryFields(current.DestinationEmail, current.SenderAddress, current.SMTPHost, current.SMTPSecurity, current.SMTPPort); err != nil {
		return emailTestResult{}, err
	}

	password, credentialErr := s.credentials.Get(smtpCredentialUser)
	if credentialErr != nil && !errors.Is(credentialErr, keyring.ErrNotFound) {
		return emailTestResult{}, fmt.Errorf("read SMTP password from OS keyring: %w", credentialErr)
	}
	if strings.TrimSpace(current.SMTPUsername) != "" && errors.Is(credentialErr, keyring.ErrNotFound) {
		return emailTestResult{}, fmt.Errorf("SMTP password is required before sending a test email")
	}

	transport := email.NewSMTPTransport(email.SMTPConfig{
		Host: current.SMTPHost, Port: current.SMTPPort, Security: email.Security(current.SMTPSecurity),
		Username: current.SMTPUsername, Password: password,
	})
	sendErr := transport.Send(ctx, email.Message{
		To: current.DestinationEmail, FromAddress: current.SenderAddress, FromName: current.SenderName,
		Subject: "ProductCrew test email",
		Body:    "This is a test email from ProductCrew confirming your email notification delivery settings work.",
	})

	now := time.Now().UTC()
	current.LastTestAt = &now
	current.LastTestHostPort = fmt.Sprintf("%s:%d", current.SMTPHost, current.SMTPPort)
	result := emailTestResult{}
	if sendErr != nil {
		current.LastTestResult = "failed"
		current.LastTestError = truncateForNotice(sendErr.Error(), 500)
		result.Success = false
		result.Message = current.LastTestError
	} else {
		current.LastTestResult = "passed"
		current.LastTestError = ""
		current.TestedConfigFingerprint = emailConfigFingerprint(current)
		result.Success = true
		result.Message = "Test email sent successfully"
	}

	stored.EmailNotifications = current
	if err := s.writeJSON("settings.json", stored); err != nil {
		return emailTestResult{}, err
	}

	response, err := s.settingsResponse(stored)
	if err != nil {
		return emailTestResult{}, err
	}
	result.Sent = response.EmailNotifications
	return result, nil
}

// emailDispatchConfig returns the current email notification settings plus
// the SMTP secret for the email dispatch coordinator. It only resolves the
// secret when notifications are enabled, so a disabled/unconfigured install
// never touches the OS keyring on every event.
func (s *projectService) emailDispatchConfig() (emailNotificationSettingsFile, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return emailNotificationSettingsFile{}, "", err
	}
	cfg := stored.EmailNotifications
	if !cfg.Enabled {
		return cfg, "", nil
	}
	password, credentialErr := s.credentials.Get(smtpCredentialUser)
	if credentialErr != nil && !errors.Is(credentialErr, keyring.ErrNotFound) {
		return emailNotificationSettingsFile{}, "", fmt.Errorf("read SMTP password from OS keyring: %w", credentialErr)
	}
	return cfg, password, nil
}

// recordEmailSendFailure persists the latest best-effort send failure so it
// can be surfaced in Settings without interrupting the caller's flow.
func (s *projectService) recordEmailSendFailure(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	stored.EmailNotifications.LastSendFailureAt = &now
	stored.EmailNotifications.LastSendFailureSummary = truncateForNotice(reason, 500)
	return s.writeJSON("settings.json", stored)
}

// clearEmailSendFailure clears a previously recorded send failure after a
// subsequent notification email succeeds.
func (s *projectService) clearEmailSendFailure() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.readSettings()
	if err != nil {
		return err
	}
	if stored.EmailNotifications.LastSendFailureAt == nil && stored.EmailNotifications.LastSendFailureSummary == "" {
		return nil
	}
	stored.EmailNotifications.LastSendFailureAt = nil
	stored.EmailNotifications.LastSendFailureSummary = ""
	return s.writeJSON("settings.json", stored)
}

func truncateForNotice(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}
