package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
)

type ClaudePlanner struct {
	instructionProvider func() (string, error)
	runtimeProvider     func() (string, string)
}

func NewClaudePlannerWithInstructions(provider func() (string, error)) *ClaudePlanner {
	return &ClaudePlanner{instructionProvider: provider}
}

func NewClaudePlannerWithRuntime(instructions func() (string, error), runtime func() (string, string)) *ClaudePlanner {
	return &ClaudePlanner{instructionProvider: instructions, runtimeProvider: runtime}
}

func (p *ClaudePlanner) Generate(ctx context.Context, request Request) (Result, error) {
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
	claudeResult, err := claude.RunJSON(ctx, prompt, claude.RunConfig{
		CWD:            request.ProjectPath,
		AdditionalDirs: request.AdditionalPaths,
		SystemPrompt:   instructions,
		Model:          model,
		Effort:         effort,
		Schema:         teamLeadPlanSchema,
		SessionID:      strings.TrimSpace(request.Plan.ThreadID),
		PermissionMode: "dontAsk",
		AllowedTools:   claude.ReadOnlyTools(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("Claude Team Lead planning failed: %w", err)
	}
	payload := claudeResult.StructuredOutput
	if len(payload) == 0 {
		payload = []byte(claudeResult.Result)
	}
	var output codexPlanOutput
	if err := json.Unmarshal(payload, &output); err != nil {
		return Result{ThreadID: claudeResult.SessionID}, fmt.Errorf("decode Claude Team Lead plan: %w", err)
	}
	preflight, err := translatePreflight(output)
	if err != nil {
		return Result{ThreadID: claudeResult.SessionID}, err
	}
	if preflight != nil && (preflight.Status == "already_implemented" || preflight.Status == "not_feasible") {
		return Result{ThreadID: claudeResult.SessionID, Preflight: preflight}, nil
	}
	draft, err := translatePlan(output)
	if err != nil {
		return Result{ThreadID: claudeResult.SessionID}, err
	}
	return Result{ThreadID: claudeResult.SessionID, Draft: draft, Preflight: preflight}, nil
}

func (p *ClaudePlanner) runtimeConfig() (string, string) {
	if p.runtimeProvider == nil {
		return claude.DefaultModel, claude.DefaultEffort
	}
	model, effort := p.runtimeProvider()
	return strings.TrimSpace(model), strings.TrimSpace(effort)
}

func (p *ClaudePlanner) instructions() (string, error) {
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
