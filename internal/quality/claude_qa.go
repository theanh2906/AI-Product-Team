package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
)

type ClaudeQA struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewClaudeQAWithInstructions(provider func() (string, error)) *ClaudeQA {
	return &ClaudeQA{instructionProvider: provider}
}

func NewClaudeQAWithRuntime(instructions func() (string, error), runtime func() (string, string)) *ClaudeQA {
	return &ClaudeQA{instructionProvider: instructions, runtimeProvider: runtime}
}

func (q *ClaudeQA) Verify(ctx context.Context, request Request) (Result, error) {
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
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         qaResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		PermissionMode: "acceptEdits",
		AllowedTools:   claude.WorkspaceWriteTools(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("Claude QA verification failed: %w", err)
	}
	var output qaOutput
	if err := json.Unmarshal(claude.Payload(claudeResult), &output); err != nil {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("decode Claude QA result: %w", err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("Claude QA returned an empty verification summary")
	}
	if err := validateQAOutput(output); err != nil {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("Claude %w", err)
	}
	return Result{ThreadID: claudeResult.SessionID, Outcome: output.Outcome, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, Bugs: output.Bugs}, nil
}

func (q *ClaudeQA) runtimeConfig() (string, string) {
	if q.runtimeProvider == nil {
		return claude.DefaultModel, claude.DefaultEffort
	}
	model, effort := q.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (q *ClaudeQA) instructions() (string, error) {
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
