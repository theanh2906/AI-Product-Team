package development

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
)

type ClaudeDeveloper struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewClaudeDeveloperWithInstructions(provider func() (string, error)) *ClaudeDeveloper {
	return &ClaudeDeveloper{instructionProvider: provider}
}

func NewClaudeDeveloperWithRuntime(instructions func() (string, error), runtime func() (string, string)) *ClaudeDeveloper {
	return &ClaudeDeveloper{instructionProvider: instructions, runtimeProvider: runtime}
}

func (d *ClaudeDeveloper) Implement(ctx context.Context, request Request) (Result, error) {
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
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         developerResultSchema,
		SessionID:      strings.TrimSpace(request.Task.ThreadID),
		PermissionMode: "acceptEdits",
		AllowedTools:   claude.WorkspaceWriteTools(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("Claude Developer implementation failed: %w", err)
	}
	return decodeClaudeDeveloperResult(claudeResult, "Claude Developer")
}

func (d *ClaudeDeveloper) RepairBuild(ctx context.Context, request BuildRepairRequest) (Result, error) {
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
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{
		CWD:            request.Request.ProjectPath,
		AdditionalDirs: request.Request.AdditionalPaths,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         developerResultSchema,
		SessionID:      threadID,
		PermissionMode: "acceptEdits",
		AllowedTools:   claude.WorkspaceWriteTools(),
	})
	if err != nil {
		return Result{ThreadID: threadID}, fmt.Errorf("Claude Developer build repair failed: %w", err)
	}
	return decodeClaudeDeveloperResult(claudeResult, "Claude Developer")
}

func (d *ClaudeDeveloper) RepairGitDelivery(ctx context.Context, request GitRepairRequest) (Result, error) {
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
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{CWD: request.Request.ProjectPath, SystemPrompt: instructions, Model: model, Effort: effort, Schema: developerResultSchema, SessionID: strings.TrimSpace(request.PreviousResult.ThreadID), PermissionMode: "acceptEdits", AllowedTools: claude.WorkspaceWriteTools()})
	if err != nil {
		return Result{}, fmt.Errorf("Claude Developer Git recovery failed: %w", err)
	}
	return decodeClaudeDeveloperResult(claudeResult, "Claude Developer")
}

func decodeClaudeDeveloperResult(claudeResult claude.Result, agentLabel string) (Result, error) {
	var output developerOutput
	if err := json.Unmarshal(claude.Payload(claudeResult), &output); err != nil {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("decode %s result: %w", agentLabel, err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("%s returned an empty implementation summary", agentLabel)
	}
	return Result{ThreadID: claudeResult.SessionID, Summary: output.Summary, ChangedFiles: output.ChangedFiles, Verification: output.Verification, RemainingRisks: output.RemainingRisks}, nil
}

func (d *ClaudeDeveloper) runtimeConfig() (string, string) {
	if d.runtimeProvider == nil {
		return claude.DefaultModel, claude.DefaultEffort
	}
	model, effort := d.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (d *ClaudeDeveloper) instructions() (string, error) {
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
