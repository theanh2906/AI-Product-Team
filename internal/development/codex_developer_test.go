package development

import (
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestDeveloperPromptContainsApprovedInputsAndSafetyBoundary(t *testing.T) {
	prompt, err := developerPrompt(Request{
		Plan:      kanban.Plan{Summary: "Approved implementation plan", Request: kanban.WorkRequest{Title: "Saved filters"}},
		Task:      kanban.Task{Key: "DEV-001", Title: "[Developer] Implement saved filters", AcceptanceCriteria: []string{"Persists filters"}},
		Documents: []kanban.Document{{Title: "Technical plan", Content: "Use the existing settings adapter."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"DEV-001", "Persists filters", "Use the existing settings adapter.", "Do not commit", "repository-relative paths"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("developer prompt is missing %q", expected)
		}
	}
}

func TestBuildRepairPromptCarriesBuildFailureAndNodeRangeRule(t *testing.T) {
	prompt, err := buildRepairPrompt(BuildRepairRequest{
		Request: Request{
			Plan: kanban.Plan{Summary: "Approved implementation plan", Request: kanban.WorkRequest{Title: "Node engine compatibility"}},
			Task: kanban.Task{Key: "DEV-059", Title: "[Developer] Update Node engine", AcceptanceCriteria: []string{"Build passes on Node 18+"}},
		},
		PreviousResult: Result{ThreadID: "developer-thread", Summary: "Updated package metadata.", ChangedFiles: []string{"package.json"}},
		Verification:   kanban.BuildVerification{Command: "npm run build", Status: "failed", ExitCode: 1, Reason: "npm run build failed with exit code 1."},
		BuildLog:       "The current Node version is v22.22.1 but package.json requires ^18.0.0.",
		Attempt:        1,
		MaxAttempts:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"DEV-059", "npm run build", "v22.22.1", ">=18", "do not narrow a compatibility range"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("build repair prompt is missing %q", expected)
		}
	}
}

func TestCodexDeveloperUsesConfiguredInstructions(t *testing.T) {
	developer := NewCodexDeveloper(nil, func() (string, error) {
		return "# Developer\n\nCustom implementation contract.", nil
	})
	instructions, err := developer.instructions()
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "# Developer\n\nCustom implementation contract." {
		t.Fatalf("configured instructions were not used: %q", instructions)
	}
}
