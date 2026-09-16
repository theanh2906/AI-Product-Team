package quality

import (
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestQAPromptEnforcesIndependentDefinitionOfDone(t *testing.T) {
	prompt, err := qaPrompt(Request{
		Plan:      kanban.Plan{Summary: "Approved plan", Request: kanban.WorkRequest{Title: "Saved filters"}},
		Task:      kanban.Task{Key: "QA-001", AcceptanceCriteria: []string{"Matches approved design"}},
		Documents: []kanban.Document{{Title: "Design specification", Content: "Keyboard and empty states are defined."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"QA-001", "Matches approved design", "Keyboard and empty states", "never fix production code", "concrete, reproducible", "focused regression mode", `outcome="blocked"`, "verification limitation is never a product bug", "report all in-scope defects"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("QA prompt is missing %q", expected)
		}
	}
}

func TestValidateQAOutputSeparatesProductFailureFromVerificationBlocker(t *testing.T) {
	if err := validateQAOutput(qaOutput{Outcome: OutcomePassed}); err != nil {
		t.Fatalf("passed output rejected: %v", err)
	}
	if err := validateQAOutput(qaOutput{Outcome: OutcomeBlocked}); err != nil {
		t.Fatalf("blocked output without bugs rejected: %v", err)
	}
	if err := validateQAOutput(qaOutput{Outcome: OutcomeFailed, Bugs: []Bug{{Title: "Scoped regression"}}}); err != nil {
		t.Fatalf("failed output with an actionable bug rejected: %v", err)
	}
	if err := validateQAOutput(qaOutput{Outcome: OutcomeFailed}); err == nil {
		t.Fatal("failed output without a product bug must be rejected")
	}
	if err := validateQAOutput(qaOutput{Outcome: OutcomeBlocked, Bugs: []Bug{{Title: "Environment issue"}}}); err == nil {
		t.Fatal("blocked output must not create product bugs")
	}
}

func TestCodexQAUsesConfiguredInstructions(t *testing.T) {
	qa := NewCodexQA(nil, func() (string, error) { return "# QA\n\nCustom verification contract.", nil })
	instructions, err := qa.instructions()
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "# QA\n\nCustom verification contract." {
		t.Fatalf("configured instructions were not used: %q", instructions)
	}
}
