package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/copilot"
)

type CopilotPlanner struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewCopilotPlannerWithRuntime(instructions func() (string, error), runtime func() (string, string)) *CopilotPlanner {
	return &CopilotPlanner{instructionProvider: instructions, runtimeProvider: runtime}
}

func (p *CopilotPlanner) Generate(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.ProjectPath) == "" {
		return Result{}, fmt.Errorf("project workspace path is required")
	}
	instructions, err := p.instructions()
	if err != nil {
		return Result{}, err
	}
	prompt, err := planningPrompt(request.Plan)
	if err != nil {
		return Result{}, err
	}
	model, effort := p.runtimeConfig()
	copilotResult, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		RestrictPaths:  len(request.AdditionalPaths) > 0,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         teamLeadPlanSchema,
		SessionID:      strings.TrimSpace(request.Plan.ThreadID),
		Writable:       false,
	})
	if err != nil {
		return Result{}, fmt.Errorf("GitHub Copilot Team Lead planning failed: %w", err)
	}
	var output codexPlanOutput
	if err := json.Unmarshal(copilot.Payload(copilotResult), &output); err != nil {
		return Result{ThreadID: copilotResult.SessionID}, fmt.Errorf("decode GitHub Copilot Team Lead plan: %w", err)
	}
	preflight, err := translatePreflight(output)
	if err != nil {
		return Result{ThreadID: copilotResult.SessionID}, err
	}
	if preflight != nil && (preflight.Status == "already_implemented" || preflight.Status == "not_feasible") {
		return Result{ThreadID: copilotResult.SessionID, Preflight: preflight}, nil
	}
	draft, err := translatePlan(output)
	if err != nil {
		return Result{ThreadID: copilotResult.SessionID}, err
	}
	return Result{ThreadID: copilotResult.SessionID, Draft: draft, Preflight: preflight}, nil
}

func (p *CopilotPlanner) runtimeConfig() (string, string) {
	if p.runtimeProvider == nil {
		return copilot.DefaultModel, copilot.DefaultEffort
	}
	model, effort := p.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (p *CopilotPlanner) instructions() (string, error) {
	if p.instructionProvider == nil {
		return teamLeadInstructions, nil
	}
	configured, err := p.instructionProvider()
	if err != nil {
		return "", fmt.Errorf("load Team Lead instructions: %w", err)
	}
	if strings.TrimSpace(configured) == "" {
		return teamLeadInstructions, nil
	}
	return configured, nil
}
