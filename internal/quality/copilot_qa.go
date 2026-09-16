package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/copilot"
)

type CopilotQA struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewCopilotQAWithRuntime(instructions func() (string, error), runtime func() (string, string)) *CopilotQA {
	return &CopilotQA{instructionProvider: instructions, runtimeProvider: runtime}
}

func (q *CopilotQA) Verify(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := q.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := qaPrompt(request)
	if err != nil {
		return Result{}, err
	}
	model, effort := q.runtimeConfig()
	copilotResult, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		RestrictPaths:  len(request.AdditionalPaths) > 0,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         qaResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		Writable:       true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("GitHub Copilot QA verification failed: %w", err)
	}
	var output qaOutput
	if err := json.Unmarshal(copilot.Payload(copilotResult), &output); err != nil {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("decode GitHub Copilot QA result: %w", err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("GitHub Copilot QA returned an empty verification summary")
	}
	if err := validateQAOutput(output); err != nil {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("GitHub Copilot %w", err)
	}
	return Result{ThreadID: copilotResult.SessionID, Outcome: output.Outcome, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, Bugs: output.Bugs}, nil
}

func (q *CopilotQA) runtimeConfig() (string, string) {
	if q.runtimeProvider == nil {
		return copilot.DefaultModel, copilot.DefaultEffort
	}
	model, effort := q.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (q *CopilotQA) instructions() (string, error) {
	if q.instructionProvider == nil {
		return defaultQAInstructions, nil
	}
	value, err := q.instructionProvider()
	if err != nil {
		return "", fmt.Errorf("load QA instructions: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaultQAInstructions, nil
	}
	return value, nil
}
