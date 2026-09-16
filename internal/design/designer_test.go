package design

import (
	"strings"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func TestDesignerPromptIncludesRevisionFeedbackAndInPlaceInstruction(t *testing.T) {
	request := Request{
		ProjectPath: "E:/Projects/AI-Product-Team",
		Plan: kanban.Plan{
			Request: kanban.WorkRequest{Title: "Designer feedback loop", Description: "Revise an existing mockup."},
			Summary: "Approved plan summary.",
		},
		Task: kanban.Task{
			ID:    "task-design",
			Key:   "DSN-001",
			Role:  kanban.RoleDesigner,
			Title: "[Designer] Revise mockup",
			RevisionHistory: []kanban.TaskRevision{{
				Revision:    1,
				Feedback:    "Tighten the hierarchy and keep Approve primary.",
				Reviewer:    "PM",
				RequestedAt: time.Date(2026, time.August, 28, 5, 0, 0, 0, time.UTC),
				Execution:   kanban.TaskExecution{Summary: "Initial mockup delivered."},
			}},
		},
	}

	prompt, err := designerPrompt(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "revise the existing mockup sources in .productcrew/design-artifacts/<assignedTask.id>/ in place") {
		t.Fatalf("prompt is missing the in-place revision instruction: %s", prompt)
	}
	if !strings.Contains(prompt, `"feedback": "Tighten the hierarchy and keep Approve primary."`) {
		t.Fatalf("prompt is missing PM feedback context: %s", prompt)
	}
	if !strings.Contains(prompt, `"nextRevision": 2`) {
		t.Fatalf("prompt is missing the next revision number: %s", prompt)
	}
}
