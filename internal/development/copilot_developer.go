package development

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/copilot"
)

type CopilotDeveloper struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewCopilotDeveloperWithRuntime(instructions func() (string, error), runtime func() (string, string)) *CopilotDeveloper {
	return &CopilotDeveloper{instructionProvider: instructions, runtimeProvider: runtime}
}

func (d *CopilotDeveloper) Implement(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := d.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := developerPrompt(request)
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
		Schema:         developerResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		Writable:       true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("GitHub Copilot Developer implementation failed: %w", err)
	}
	return decodeCopilotDeveloperResult(copilotResult, "GitHub Copilot Developer")
}

func (d *CopilotDeveloper) RepairBuild(ctx context.Context, request BuildRepairRequest) (Result, error) {
	if strings.TrimSpace(request.Request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := d.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := buildRepairPrompt(request)
	if err != nil {
		return Result{}, err
	}
	threadID := strings.TrimSpace(request.PreviousResult.ThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(request.Request.Task.ThreadID)
	}
	model, effort := d.runtimeConfig()
	copilotResult, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{
		CWD:            request.Request.ProjectPath,
		AdditionalDirs: request.Request.AdditionalPaths,
		RestrictPaths:  len(request.Request.AdditionalPaths) > 0,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         developerResultSchema,
		SessionID:      threadID,
		Writable:       true,
	})
	if err != nil {
		return Result{ThreadID: threadID}, fmt.Errorf("GitHub Copilot Developer build repair failed: %w", err)
	}
	return decodeCopilotDeveloperResult(copilotResult, "GitHub Copilot Developer")
}

func (d *CopilotDeveloper) RepairGitDelivery(ctx context.Context, request GitRepairRequest) (Result, error) {
	if strings.TrimSpace(request.Request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := d.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := gitRepairPrompt(request)
	if err != nil {
		return Result{}, err
	}
	model, effort := d.runtimeConfig()
	copilotResult, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{CWD: request.Request.ProjectPath, SystemPrompt: instructions, Model: model, Effort: effort, Schema: developerResultSchema, SessionID: strings.TrimSpace(request.PreviousResult.ThreadID), Writable: true})
	if err != nil {
		return Result{}, fmt.Errorf("GitHub Copilot Developer Git recovery failed: %w", err)
	}
	return decodeCopilotDeveloperResult(copilotResult, "GitHub Copilot Developer")
}

func decodeCopilotDeveloperResult(copilotResult copilot.Result, agentLabel string) (Result, error) {
	var output developerOutput
	if err := json.Unmarshal(copilot.Payload(copilotResult), &output); err != nil {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("decode %s result: %w", agentLabel, err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("%s returned an empty implementation summary", agentLabel)
	}
	return Result{ThreadID: copilotResult.SessionID, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, RemainingRisks: output.RemainingRisks}, nil
}

func (d *CopilotDeveloper) runtimeConfig() (string, string) {
	if d.runtimeProvider == nil {
		return copilot.DefaultModel, copilot.DefaultEffort
	}
	model, effort := d.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (d *CopilotDeveloper) instructions() (string, error) {
	if d.instructionProvider == nil {
		return defaultDeveloperInstructions, nil
	}
	value, err := d.instructionProvider()
	if err != nil {
		return "", fmt.Errorf("load Developer instructions: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaultDeveloperInstructions, nil
	}
	return value, nil
}
