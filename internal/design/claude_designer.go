package design

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
)

type ClaudeDesigner struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewClaudeDesignerWithInstructions(provider func() (string, error)) *ClaudeDesigner {
	return &ClaudeDesigner{instructionProvider: provider}
}

func NewClaudeDesignerWithRuntime(instructions func() (string, error), runtime func() (string, string)) *ClaudeDesigner {
	return &ClaudeDesigner{instructionProvider: instructions, runtimeProvider: runtime}
}

func (d *ClaudeDesigner) Design(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := d.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := designerPrompt(request)
	if err != nil {
		return Result{}, err
	}
	model, effort := d.runtimeConfig()
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         designerResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		PermissionMode: "acceptEdits",
		AllowedTools:   claude.WorkspaceWriteTools(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("Claude Designer handoff failed: %w", err)
	}
	var output designerOutput
	if err := json.Unmarshal(claude.Payload(claudeResult), &output); err != nil {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("decode Claude Designer result: %w", err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("Claude Designer returned an empty handoff summary")
	}
	return Result{ThreadID: claudeResult.SessionID, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, RemainingRisks: output.RemainingRisks}, nil
}

func (d *ClaudeDesigner) runtimeConfig() (string, string) {
	if d.runtimeProvider == nil {
		return claude.DefaultModel, claude.DefaultEffort
	}
	model, effort := d.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (d *ClaudeDesigner) instructions() (string, error) {
	if d.instructionProvider == nil {
		return defaultDesignerInstructions, nil
	}
	value, err := d.instructionProvider()
	if err != nil {
		return "", fmt.Errorf("load Designer instructions: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaultDesignerInstructions, nil
	}
	return value, nil
}
