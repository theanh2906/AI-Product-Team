package planning

import (
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestTranslatePlanPreservesStructuredRoleContracts(t *testing.T) {
	output := codexPlanOutput{Summary: "A complete implementation-ready planning summary."}
	document := struct {
		Ref      string   `json:"ref"`
		Title    string   `json:"title"`
		Kind     string   `json:"kind"`
		Audience []string `json:"audience"`
		Content  string   `json:"content"`
	}{Ref: "spec", Title: "Feature specification", Kind: "product-spec", Audience: []string{"developer", "qa"}, Content: "Scope"}
	output.Documents = append(output.Documents, document)
	task := struct {
		Ref                string   `json:"ref"`
		Role               string   `json:"role"`
		Title              string   `json:"title"`
		Priority           string   `json:"priority"`
		Description        string   `json:"description"`
		AcceptanceCriteria []string `json:"acceptanceCriteria"`
		Dependencies       []string `json:"dependencies"`
		Documents          []string `json:"documents"`
	}{Ref: "implementation", Role: "developer", Title: "Implement feature", Priority: "high", Description: "Implement scope", AcceptanceCriteria: []string{"Works"}, Documents: []string{"spec"}}
	output.Tasks = append(output.Tasks, task)

	draft, err := translatePlan(output)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Tasks[0].Role != kanban.RoleDeveloper || draft.Documents[0].Audience[1] != kanban.RoleQA {
		t.Fatalf("role contract was not preserved: %+v", draft)
	}
}

func TestTranslatePreflightSupportsAlreadyImplementedSkip(t *testing.T) {
	output := codexPlanOutput{Summary: "The requested fix is already present, so no downstream work is needed."}
	output.Preflight.Status = "already_implemented"
	output.Preflight.Summary = "Source inspection found the requested fix and regression coverage already present."
	output.Preflight.Evidence = []string{"settings save normalizes empty TeamCity build type lists"}
	output.Preflight.Verification = []string{"focused regression test covers empty-list save behavior"}

	preflight, err := translatePreflight(output)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Status != "already_implemented" || len(preflight.Evidence) != 1 || len(preflight.Verification) != 1 {
		t.Fatalf("preflight verification was not translated: %+v", preflight)
	}
}

func TestTranslatePreflightSupportsNotFeasibleSkip(t *testing.T) {
	output := codexPlanOutput{Summary: "The requested change cannot be implemented as stated."}
	output.Preflight.Status = "not_feasible"
	output.Preflight.Summary = "The repository lacks the required platform capability and there is no safe fallback path."
	output.Preflight.Evidence = []string{"No Tauri command or permission surface can satisfy the requested OS integration"}
	output.Preflight.Verification = []string{"Inspected command registration and settings schema"}
	output.Preflight.RemainingWork = []string{"Re-scope the request to an explicit user-approved local command"}

	preflight, err := translatePreflight(output)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Status != "not_feasible" || len(preflight.RemainingWork) != 1 {
		t.Fatalf("not feasible preflight was not translated: %+v", preflight)
	}
}

func TestPlanningPromptIncludesPMRevisionFeedback(t *testing.T) {
	prompt, err := planningPrompt(kanban.Plan{
		Request: kanban.WorkRequest{Title: "Saved filters", Description: "Persist filters"},
		Reviews: []kanban.PlanReview{{Decision: "deny", Revision: 2, Reason: "Clarify migration behavior"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Clarify migration behavior") || !strings.Contains(prompt, "complete replacement plan") {
		t.Fatalf("revision feedback missing from prompt: %s", prompt)
	}
}

func TestPlanningPromptRequiresRepositoryPreflight(t *testing.T) {
	prompt, err := planningPrompt(kanban.Plan{
		Request: kanban.WorkRequest{Title: "Saved filters", Description: "Persist filters"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "preflight verification") || !strings.Contains(prompt, "already_implemented") || !strings.Contains(prompt, "not_feasible") {
		t.Fatalf("preflight instruction missing from prompt: %s", prompt)
	}
}

func TestCodexPlannerUsesConfiguredTeamLeadInstructions(t *testing.T) {
	planner := NewCodexPlannerWithInstructions(nil, func() (string, error) {
		return "# Team Lead\n\nCustom planning contract.", nil
	})
	instructions, err := planner.instructions()
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "# Team Lead\n\nCustom planning contract." {
		t.Fatalf("configured instructions were not used: %q", instructions)
	}
}
