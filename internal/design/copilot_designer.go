package design

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/copilot"
)

type CopilotDesigner struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewCopilotDesignerWithRuntime(instructions func() (string, error), runtime func() (string, string)) *CopilotDesigner {
	return &CopilotDesigner{instructionProvider: instructions, runtimeProvider: runtime}
}

func (d *CopilotDesigner) Design(ctx context.Context, request Request) (Result, error) {
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
	copilotResult, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		RestrictPaths:  len(request.AdditionalPaths) > 0,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         designerResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		Writable:       true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("GitHub Copilot Designer handoff failed: %w", err)
	}
	var output designerOutput
	if err := json.Unmarshal(copilot.Payload(copilotResult), &output); err != nil {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("decode GitHub Copilot Designer result: %w", err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("GitHub Copilot Designer returned an empty handoff summary")
	}
	return Result{ThreadID: copilotResult.SessionID, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, RemainingRisks: output.RemainingRisks}, nil
}

func (d *CopilotDesigner) runtimeConfig() (string, string) {
	if d.runtimeProvider == nil {
		return copilot.DefaultModel, copilot.DefaultEffort
	}
	model, effort := d.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (d *CopilotDesigner) instructions() (string, error) {
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
