package web

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
	"github.com/theanh2906/AI-Product-Team/internal/copilot"
)

type memoryCredentialStore struct {
	value  string
	values map[string]string
}

func (s *memoryCredentialStore) Get(key string) (string, error) {
	if key == credentialUser && s.value != "" {
		return s.value, nil
	}
	if value, ok := s.values[key]; ok && value != "" {
		return value, nil
	}
	return "", keyring.ErrNotFound
}

func (s *memoryCredentialStore) Set(key, value string) error {
	if key == credentialUser {
		s.value = value
		return nil
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *memoryCredentialStore) Delete(key string) error {
	if key == credentialUser {
		s.value = ""
		return nil
	}
	delete(s.values, key)
	return nil
}

func TestSettingsKeepPATOutOfJSON(t *testing.T) {
	directory := t.TempDir()
	clonePath := filepath.Join(directory, "workspaces")
	credentials := &memoryCredentialStore{}
	service, err := newProjectService(directory, credentials)
	if err != nil {
		t.Fatal(err)
	}

	settings, err := service.updateSettings(updateSettingsRequest{
		ClonePath:           clonePath,
		GitProvider:         "github",
		GitUsername:         "octocat",
		PersonalAccessToken: "secret-token-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PATConfigured || credentials.value != "secret-token-value" {
		t.Fatal("expected PAT to be stored in the credential store")
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token-value") {
		t.Fatal("settings.json must never contain the PAT")
	}
}

func TestThemePersistsWithoutOverwritingOtherSettings(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	clonePath := filepath.Join(directory, "workspaces")
	if _, err := service.updateSettings(updateSettingsRequest{
		ClonePath: clonePath, GitProvider: "gitlab", GitUsername: "ben",
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.updateTheme("dark")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Theme != "dark" || updated.ClonePath != clonePath || updated.GitProvider != "gitlab" {
		t.Fatalf("unexpected settings after theme update: %+v", updated)
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"theme": "dark"`) {
		t.Fatalf("theme was not persisted: %s", data)
	}
}

func TestAIPlatformDefaultsAndPersistence(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	initial, err := service.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if initial.AIProvider != "codex" || initial.AIFallbackProvider != "claude-code" || initial.AIFallbackEnabled ||
		initial.CodexEffort != "high" || initial.ClaudeCodeModel != claude.DefaultModel || initial.ClaudeCodeEffort != claude.DefaultEffort ||
		initial.GitHubCopilotModel != copilot.DefaultModel || initial.GitHubCopilotEffort != "" {
		t.Fatalf("unexpected default AI platform settings: %+v", initial)
	}

	clonePath := filepath.Join(directory, "workspaces")
	updated, err := service.updateSettings(updateSettingsRequest{
		ClonePath:           clonePath,
		AIProvider:          "codex",
		AIFallbackProvider:  "claude-code",
		AIFallbackEnabled:   true,
		CodexModel:          "gpt-5.3-codex-spark",
		CodexEffort:         "high",
		ClaudeCodeModel:     "opus",
		ClaudeCodeEffort:    "xhigh",
		GitHubCopilotModel:  "auto",
		GitHubCopilotEffort: "max",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AIProvider != "codex" || updated.AIFallbackProvider != "claude-code" || !updated.AIFallbackEnabled ||
		updated.CodexModel != "gpt-5.3-codex-spark" || updated.CodexEffort != "high" ||
		updated.ClaudeCodeModel != "opus" || updated.ClaudeCodeEffort != "xhigh" ||
		updated.GitHubCopilotModel != "auto" || updated.GitHubCopilotEffort != "" {
		t.Fatalf("AI platform settings were not persisted: %+v", updated)
	}

	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"aiProvider": "codex"`) ||
		!strings.Contains(string(data), `"aiFallbackProvider": "claude-code"`) ||
		!strings.Contains(string(data), `"codexModel": "gpt-5.3-codex-spark"`) ||
		!strings.Contains(string(data), `"codexEffort": "high"`) ||
		!strings.Contains(string(data), `"claudeCodeModel": "opus"`) ||
		!strings.Contains(string(data), `"claudeCodeEffort": "xhigh"`) ||
		!strings.Contains(string(data), `"githubCopilotModel": "auto"`) ||
		!strings.Contains(string(data), `"githubCopilotEffort": ""`) {
		t.Fatalf("AI platform settings missing from settings.json: %s", data)
	}
}

func TestAIPlatformSupportsGitHubCopilotProvider(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	clonePath := filepath.Join(directory, "workspaces")

	updated, err := service.updateSettings(updateSettingsRequest{
		ClonePath:           clonePath,
		AIProvider:          "github-copilot",
		AIFallbackProvider:  "claude-code",
		AIFallbackEnabled:   true,
		GitHubCopilotModel:  "auto",
		GitHubCopilotEffort: "minimal",
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.AIProvider != "github-copilot" || updated.GitHubCopilotModel != "auto" || updated.GitHubCopilotEffort != "" {
		t.Fatalf("GitHub Copilot settings were not persisted: %+v", updated)
	}
	if provider := service.selectedAIProvider(nil); provider != "github-copilot" {
		t.Fatalf("expected GitHub Copilot to be selected, got %q", provider)
	}
}

func TestAIPlatformPersistsCopilotEffortForSpecificModel(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.updateSettings(updateSettingsRequest{
		ClonePath:           filepath.Join(directory, "workspaces"),
		AIProvider:          "github-copilot",
		GitHubCopilotModel:  "gpt-5.6-sol",
		GitHubCopilotEffort: "max",
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.GitHubCopilotModel != "gpt-5.6-sol" || updated.GitHubCopilotEffort != "max" {
		t.Fatalf("specific GitHub Copilot model effort was not persisted: %+v", updated)
	}
}

func TestAIPlatformCanUseGitHubCopilotFallbackWhenCodexUnavailable(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.updateSettings(updateSettingsRequest{
		ClonePath:          filepath.Join(directory, "workspaces"),
		AIProvider:         "codex",
		AIFallbackProvider: "github-copilot",
		AIFallbackEnabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if provider := service.selectedAIProvider(nil); provider != "github-copilot" {
		t.Fatalf("expected GitHub Copilot fallback, got %q", provider)
	}
}

func TestUpdateSettingsCreatesMissingCloneFolder(t *testing.T) {
	directory := t.TempDir()
	clonePath := filepath.Join(directory, "missing", "workspaces")
	service, err := newProjectService(filepath.Join(directory, "config"), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.updateSettings(updateSettingsRequest{ClonePath: clonePath}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(clonePath)
	if err != nil {
		t.Fatalf("expected clone folder to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a folder", clonePath)
	}
}

func TestUpdateSettingsRejectsInvalidCloneFolderFormat(t *testing.T) {
	service, err := newProjectService(t.TempDir(), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.updateSettings(updateSettingsRequest{ClonePath: `relative\\folder`})
	if err == nil || !strings.Contains(err.Error(), "valid absolute folder path") {
		t.Fatalf("expected folder path validation error, got %v", err)
	}
}

func TestRoleDefinitionsDefaultAndPersistInSettingsJSON(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	initial, err := service.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(initial.RoleDefinitions.TeamLead, "# Team Lead") || initial.RoleDefinitions.Developer == "" ||
		!strings.HasPrefix(initial.RoleDefinitions.BugScanner, "# Bug Scanner") || !strings.HasPrefix(initial.RoleDefinitions.FeatureRadar, "# Feature Radar") {
		t.Fatalf("expected default Markdown definitions, got %+v", initial.RoleDefinitions)
	}

	updated, err := service.updateRoleDefinition("designer", "# Designer\n\nUse the approved design system.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.RoleDefinitions.Designer != "# Designer\n\nUse the approved design system." {
		t.Fatalf("designer definition was not updated: %q", updated.RoleDefinitions.Designer)
	}
	updated, err = service.updateRoleDefinition("feature-radar", "# Feature Radar\n\nUse the configured discovery skill.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.RoleDefinitions.FeatureRadar != "# Feature Radar\n\nUse the configured discovery skill." {
		t.Fatalf("Feature Radar definition was not updated: %q", updated.RoleDefinitions.FeatureRadar)
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"roleDefinitions"`) || !strings.Contains(string(data), "Use the approved design system.") {
		t.Fatalf("role definitions were not persisted: %s", data)
	}
}

func TestWorkingStandardSkillsAndEffectiveProfilePersist(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	initial, err := service.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if initial.WorkingStandard == "" || len(initial.AgentSkills) < 4 || initial.AgentProfileVersion < 1 {
		t.Fatalf("expected default agent studio configuration, got %+v", initial)
	}

	updated, err := service.updateWorkingStandard("# Shared standard\n\nInspect the repository first.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.AgentProfileVersion <= initial.AgentProfileVersion {
		t.Fatalf("expected profile version to increase, got %d", updated.AgentProfileVersion)
	}

	skill := updated.AgentSkills[0]
	skillUpdated, err := service.updateAgentSkill(skill.ID, updateAgentSkillRequest{
		Name: skill.Name, Description: skill.Description, Instructions: "Custom evidence workflow.", Roles: []string{"developer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skillUpdated.AgentSkills[0].Roles) != 1 || skillUpdated.AgentSkills[0].Roles[0] != "developer" {
		t.Fatalf("skill routing was not persisted: %+v", skillUpdated.AgentSkills[0])
	}

	profile, err := service.effectiveRoleProfile("developer")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Inspect the repository first.", "# Developer", "Custom evidence workflow."} {
		if !strings.Contains(profile.Content, expected) {
			t.Fatalf("effective Developer profile is missing %q", expected)
		}
	}
	skillUpdated, err = service.updateAgentSkill(skill.ID, updateAgentSkillRequest{
		Name: skill.Name, Description: skill.Description, Instructions: "Custom Feature Radar discovery workflow.", Roles: []string{"feature-radar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	radarProfile, err := service.effectiveRoleProfile("feature-radar")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Inspect the repository first.", "# Feature Radar", "Custom Feature Radar discovery workflow."} {
		if !strings.Contains(radarProfile.Content, expected) {
			t.Fatalf("effective Feature Radar profile is missing %q", expected)
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"workingStandard"`) || !strings.Contains(string(data), `"agentSkills"`) {
		t.Fatalf("agent studio settings were not persisted: %s", data)
	}
}

func TestProjectAgentStudioProfilesAreScopedPerProject(t *testing.T) {
	directory := t.TempDir()
	projectPath := filepath.Join(directory, "sample-project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := newProjectService(filepath.Join(directory, "config"), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.importLocal(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	initial, err := service.getProjectAgentStudio(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !initial.Inherited || !strings.HasPrefix(initial.WorkingStandard, "# ProductCrew Working Standard") {
		t.Fatalf("expected project profile to inherit defaults, got %+v", initial)
	}

	updated, err := service.updateProjectWorkingStandard(created.ID, "# Project Standard\n\nUse this repository's Makefile before claiming success.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Inherited || updated.WorkingStandard != "# Project Standard\n\nUse this repository's Makefile before claiming success." {
		t.Fatalf("project profile was not saved as scoped content: %+v", updated)
	}
	profilePath := filepath.Join(projectPath, ".productcrew", "agent-studio", "profile.json")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("expected project profile at %s: %v", profilePath, err)
	}
	if !strings.Contains(string(data), "Project Standard") {
		t.Fatalf("project profile content was not persisted: %s", data)
	}

	if _, err := service.updateProjectRoleDefinition(created.ID, "developer", "# Developer\n\nRun project-specific verification."); err != nil {
		t.Fatal(err)
	}
	effective, err := service.effectiveRoleInstructionsForProjectPath("developer", projectPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Project Standard", "Run project-specific verification."} {
		if !strings.Contains(effective, expected) {
			t.Fatalf("project effective profile is missing %q: %s", expected, effective)
		}
	}

	global, err := service.effectiveRoleInstructions("developer")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(global, "Project Standard") {
		t.Fatalf("global profile should not include project-scoped instructions: %s", global)
	}
}

func TestImportLocalProjectPersistsMetadata(t *testing.T) {
	directory := t.TempDir()
	projectPath := filepath.Join(directory, "sample-project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := newProjectService(filepath.Join(directory, "config"), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.importLocal(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "sample-project" || created.Source != "local" {
		t.Fatalf("unexpected project metadata: %+v", created)
	}
	projects, err := service.listProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Path != projectPath {
		t.Fatalf("unexpected projects: %+v", projects)
	}
	_, err = service.importLocal(projectPath)
	if err == nil || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("expected duplicate import error, got %v", err)
	}
}

func TestRemoveProjectDeletesOnlyMetadataAndPreservesSourceFolder(t *testing.T) {
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first-project")
	secondPath := filepath.Join(directory, "second-project")
	if err := os.MkdirAll(firstPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondPath, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := newProjectService(filepath.Join(directory, "config"), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.importLocal(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.importLocal(secondPath)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := service.removeProject(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != first.ID || removed.Path != firstPath {
		t.Fatalf("unexpected removed project: %+v", removed)
	}
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("source folder must be preserved: %v", err)
	}
	projects, err := service.listProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != second.ID || projects[0].Path != secondPath {
		t.Fatalf("expected only unrelated project metadata to remain, got %+v", projects)
	}
	if _, err := service.removeProject(first.ID); !errors.Is(err, errProjectNotFound) {
		t.Fatalf("expected missing project error on repeat removal, got %v", err)
	}
}

func TestGitImportRejectsUnsafeURL(t *testing.T) {
	service, err := newProjectService(t.TempDir(), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.importGit(importGitRequest{RepositoryURL: "file:///sensitive/path"})
	if err == nil || !strings.Contains(err.Error(), "valid HTTPS or SSH") {
		t.Fatalf("expected URL validation error, got %v", err)
	}
}

func TestMemoryCredentialStoreMatchesKeyringNotFound(t *testing.T) {
	_, err := (&memoryCredentialStore{}).Get(credentialUser)
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected keyring not found, got %v", err)
	}
}

func TestSettingsBackupAndRestore(t *testing.T) {
	directory := t.TempDir()
	clonePath := filepath.Join(directory, "workspaces")
	credentials := &memoryCredentialStore{value: "super-secret-pat"}
	service, err := newProjectService(directory, credentials)
	if err != nil {
		t.Fatal(err)
	}

	// Update some settings first to have custom values to backup
	_, err = service.updateSettings(updateSettingsRequest{
		ClonePath:          clonePath,
		GitProvider:        "github",
		GitUsername:        "test-user",
		AIProvider:         "claude-code",
		AIFallbackProvider: "github-copilot",
		AIFallbackEnabled:  true,
		ClaudeCodeModel:    "claude-3-5-sonnet",
		ClaudeCodeEffort:   "medium",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test Export Settings
	payload, err := service.exportSettings()
	if err != nil {
		t.Fatal(err)
	}

	if payload.Version != 1 {
		t.Errorf("expected version 1, got %d", payload.Version)
	}
	if payload.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	// Verify CreatedAt is close to current time
	if time.Since(payload.CreatedAt) > 10*time.Second {
		t.Errorf("expected CreatedAt to be recent, got %v", payload.CreatedAt)
	}
	if payload.Settings.GitUsername != "test-user" {
		t.Errorf("expected GitUsername 'test-user', got %q", payload.Settings.GitUsername)
	}
	if payload.Settings.ClaudeCodeModel != "claude-3-5-sonnet" {
		t.Errorf("expected ClaudeCodeModel 'claude-3-5-sonnet', got %q", payload.Settings.ClaudeCodeModel)
	}

	// 2. Test Secret Omission
	// Encode payload to JSON bytes and assert no secrets exist
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret-pat") {
		t.Fatal("exported payload must never contain the PAT")
	}

	// 3. Test Preview Settings
	preview, err := service.previewSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Current.GitUsername != "test-user" {
		t.Errorf("expected current username 'test-user', got %q", preview.Current.GitUsername)
	}
	if preview.Incoming.GitUsername != "test-user" {
		t.Errorf("expected incoming username 'test-user', got %q", preview.Incoming.GitUsername)
	}

	// Test Preview Version Validation (> 1 should fail)
	badPayload := payload
	badPayload.Version = 2
	_, err = service.previewSettings(badPayload)
	if err == nil {
		t.Fatal("expected error previewing version 2 backup, got nil")
	}

	// Test Preview Version Validation (== 0 should fail)
	badPayload.Version = 0
	_, err = service.previewSettings(badPayload)
	if err == nil {
		t.Fatal("expected error previewing version 0 backup, got nil")
	}

	// 4. Test Restore Settings
	// Change something in payload to restore
	restorePayload := payload
	restorePayload.Settings.GitUsername = "restored-user"
	restorePayload.Settings.Theme = "dark"

	restoredSettings, err := service.restoreSettings(restorePayload)
	if err != nil {
		t.Fatal(err)
	}

	if restoredSettings.GitUsername != "restored-user" {
		t.Errorf("expected GitUsername 'restored-user', got %q", restoredSettings.GitUsername)
	}
	if restoredSettings.Theme != "dark" {
		t.Errorf("expected Theme 'dark', got %q", restoredSettings.Theme)
	}

	// Verify it was persisted on disk by reading settings again
	freshSettings, err := service.getSettings()
	if err != nil {
		t.Fatal(err)
	}
	if freshSettings.GitUsername != "restored-user" {
		t.Errorf("expected GitUsername 'restored-user' on disk, got %q", freshSettings.GitUsername)
	}

	// Test Restore Version Validation (> 1 should fail)
	badPayload.Version = 2
	_, err = service.restoreSettings(badPayload)
	if err == nil {
		t.Fatal("expected error restoring version 2 backup, got nil")
	}

	// Test Restore Validation with empty ClonePath
	badPayload = payload
	badPayload.Settings.ClonePath = ""
	_, err = service.restoreSettings(badPayload)
	if err == nil {
		t.Fatal("expected error restoring with empty ClonePath, got nil")
	}
}
