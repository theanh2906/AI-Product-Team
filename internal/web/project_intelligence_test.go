package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectStudyInventoryMapsSourceAndExcludesGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"README.md":                    "# Sample",
		"frontend/package.json":        `{"scripts":{"build":"ng build"}}`,
		"internal/server/main.go":      "package server",
		"design-qa-project-atlas.png":  "binary preview",
		"node_modules/pkg/index.js":    "generated",
		".productcrew/cache/data.json": "generated",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	inventory, err := scanProjectStudyInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.FilesScanned != 3 {
		t.Fatalf("expected 3 source files, got %d", inventory.FilesScanned)
	}
	if len(inventory.Folders) != 2 || inventory.Fingerprint == "" {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
	if strings.Contains(strings.Join(inventory.NotableFiles, "|"), "node_modules") {
		t.Fatalf("generated dependency content leaked into evidence: %+v", inventory.NotableFiles)
	}
}

func TestProjectStudyManagerPersistsResultAndDetectsStaleSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetProject := project{ID: "project-1", Name: "sample", Path: root, Source: "local", ImportedAt: time.Now()}
	runner := func(_ context.Context, _ project, _ projectStudyInventory, _ string, progress func(string, string, int)) (projectStudyAIOutput, error) {
		progress("analyzing", "Reading entry points", 55)
		return projectStudyAIOutput{Summary: "A small Go service", Components: []projectStudyComponent{{ID: "service", Name: "Service", Kind: "backend", Path: "src", Confidence: 92}}, RoleGuidance: []projectStudyRoleGuidance{{Role: "developer", Instructions: "Run go test ./...", Confidence: 90}}}, nil
	}
	manager := newProjectStudyManager(runner)
	_, err := manager.start(targetProject, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var latest projectStudyJob
	for time.Now().Before(deadline) {
		latest, _, err = manager.latest(targetProject)
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if latest.Status != "completed" || latest.Result == nil || latest.Result.Freshness != "fresh" {
		t.Fatalf("study did not complete: %+v", latest)
	}
	if latest.Result.DataEntities == nil || latest.Result.Sequences == nil || latest.Result.Workflows == nil {
		t.Fatalf("empty study collections must serialize as arrays: %+v", latest.Result)
	}
	if _, err := os.Stat(projectIntelligencePath(targetProject)); err != nil {
		t.Fatalf("study cache was not persisted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "new.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	latest, _, err = manager.latest(targetProject)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Result == nil || latest.Result.Freshness != "stale" {
		t.Fatalf("source change was not detected: %+v", latest.Result)
	}
}

func TestProjectStudyManagerPersistsEnrichmentErrorForCachedLatest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetProject := project{ID: "project-1", Name: "sample", Path: root, Source: "local", ImportedAt: time.Now()}
	runner := func(_ context.Context, _ project, _ projectStudyInventory, _ string, _ func(string, string, int)) (projectStudyAIOutput, error) {
		return projectStudyAIOutput{}, fmt.Errorf("Codex project study failed: selected model is unavailable")
	}
	manager := newProjectStudyManager(runner)
	if _, err := manager.start(targetProject, "trace-1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		latest, _, err := manager.latest(targetProject)
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	restarted := newProjectStudyManager(nil)
	latest, exists, err := restarted.latest(targetProject)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || latest.Status != "needs_attention" || latest.Result == nil {
		t.Fatalf("expected cached partial study, got exists=%v latest=%+v", exists, latest)
	}
	if !strings.Contains(latest.Error, "selected model is unavailable") {
		t.Fatalf("expected cached job error to include root cause, got %q", latest.Error)
	}
	if latest.Result.EnrichmentError != latest.Error {
		t.Fatalf("expected result enrichment error to hydrate job error, got result=%q job=%q", latest.Result.EnrichmentError, latest.Error)
	}
}

func TestApplyProjectStudyGuidanceDoesNotReplaceManualRoleInstructions(t *testing.T) {
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
	if _, err := service.updateProjectRoleDefinition(created.ID, "developer", "# Developer\n\nManual project instruction."); err != nil {
		t.Fatal(err)
	}
	if _, err := service.applyProjectLearnedGuidance(created.ID, roleDefinitions{Developer: "Learned build and test command."}); err != nil {
		t.Fatal(err)
	}
	effective, err := service.effectiveRoleProfileForProject(created.ID, "developer")
	if err != nil {
		t.Fatal(err)
	}
	learned := strings.Index(effective.Content, "Learned build and test command.")
	manual := strings.Index(effective.Content, "Manual project instruction.")
	if learned < 0 || manual <= learned {
		t.Fatalf("manual instructions must remain after learned guidance: %s", effective.Content)
	}
}
