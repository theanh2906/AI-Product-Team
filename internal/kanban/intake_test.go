package kanban

import (
	"context"
	"testing"
)

func TestNormalizeIntakeAppliesBoundBindings(t *testing.T) {
	questionnaire := testIntakeQuestionnaire()
	normalized, err := NormalizeIntake(IntakeSubmission{
		Questionnaire: questionnaire,
		Answers: map[string][]string{
			"user-experience":  {"false"},
			"delivery-surface": {"backend"},
			"quality-bar":      {"core-flow", "tests"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeIntake() error = %v", err)
	}
	if normalized.WorkType != "feature" || normalized.DeliveryTarget != "backend" || normalized.RequiresUI {
		t.Fatalf("NormalizeIntake() = %#v", normalized)
	}
	if len(normalized.AcceptanceCriteria) != 2 {
		t.Fatalf("AcceptanceCriteria = %#v", normalized.AcceptanceCriteria)
	}
}

func TestNormalizeIntakeRejectsUnknownOption(t *testing.T) {
	_, err := NormalizeIntake(IntakeSubmission{
		Questionnaire: testIntakeQuestionnaire(),
		Answers: map[string][]string{
			"user-experience":  {"true"},
			"delivery-surface": {"mobile"},
			"quality-bar":      {"core-flow"},
		},
	})
	if err == nil {
		t.Fatal("NormalizeIntake() expected an error")
	}
}

func TestIntakeSnapshotSurvivesBacklogToPlanning(t *testing.T) {
	service := NewService(&memoryBoardRepository{})
	submission := &IntakeSubmission{
		Questionnaire: testIntakeQuestionnaire(),
		Answers: map[string][]string{
			"user-experience": {"true"},
			"delivery-surface": {"fullstack"},
			"quality-bar": {"core-flow"},
		},
	}
	board, item, err := service.AddBacklogItem(context.Background(), "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogFeature, Source: "manual", Title: "Dynamic request intake",
		Description: "Generate request-specific clarification controls before planning.",
		DeliveryTarget: "fullstack", RequiresUI: true, AcceptanceCriteria: []string{"Core flow works"}, Intake: submission,
	})
	if err != nil {
		t.Fatalf("AddBacklogItem() error = %v", err)
	}
	if item.Intake == nil || item.Intake.Questionnaire.SchemaVersion != IntakeSchemaVersion {
		t.Fatalf("backlog intake snapshot = %#v", item.Intake)
	}
	_, plan, err := service.StartBacklogPlanning(context.Background(), board.ProjectID, item.ID)
	if err != nil {
		t.Fatalf("StartBacklogPlanning() error = %v", err)
	}
	if plan.Request.Intake == nil || plan.Request.Intake.Answers["delivery-surface"][0] != "fullstack" {
		t.Fatalf("planning intake snapshot = %#v", plan.Request.Intake)
	}
}

func testIntakeQuestionnaire() IntakeQuestionnaire {
	return IntakeQuestionnaire{
		SchemaVersion: IntakeSchemaVersion,
		Heading:       "Confirm delivery decisions",
		Summary:       "Answer the decisions Team Lead needs before planning.",
		WorkType:      "feature",
		Source:        "codex",
		Questions: []IntakeQuestion{
			{ID: "user-experience", Label: "Does this change the user experience?", HelpText: "Controls Designer work.", Type: "toggle", Binding: "requiresUI", Required: true, Options: []IntakeOption{{Value: "true", Label: "Yes", Description: "Include UX."}, {Value: "false", Label: "No", Description: "Engineering only."}}, DefaultValues: []string{"true"}, Layout: IntakeLayout{Span: 12}},
			{ID: "delivery-surface", Label: "Which surface is affected?", HelpText: "Choose the implementation boundary.", Type: "dropdown", Binding: "deliveryTarget", Required: true, Options: []IntakeOption{{Value: "frontend", Label: "Frontend", Description: "Client work."}, {Value: "backend", Label: "Backend", Description: "Service work."}, {Value: "fullstack", Label: "Full-stack", Description: "Both."}}, DefaultValues: []string{"fullstack"}, Layout: IntakeLayout{Span: 6}},
			{ID: "quality-bar", Label: "Select the quality bar", HelpText: "Used by QA.", Type: "checkbox", Binding: "acceptanceCriteria", Required: true, Options: []IntakeOption{{Value: "core-flow", Label: "Core flow", Description: "End to end.", AcceptanceCriterion: "Core flow works end to end"}, {Value: "tests", Label: "Tests", Description: "Regression coverage.", AcceptanceCriterion: "Critical behavior has regression coverage"}}, DefaultValues: []string{"core-flow"}, Layout: IntakeLayout{Span: 12}},
		},
	}
}
