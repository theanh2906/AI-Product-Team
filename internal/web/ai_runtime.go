package web

import (
	"context"

	"github.com/theanh2906/AI-Product-Team/internal/codex"
	"github.com/theanh2906/AI-Product-Team/internal/design"
	"github.com/theanh2906/AI-Product-Team/internal/development"
	"github.com/theanh2906/AI-Product-Team/internal/planning"
	"github.com/theanh2906/AI-Product-Team/internal/quality"
)

type aiPlanner struct {
	projects *projectService
	runtime  *codex.Client
}

func newAIPlanner(projects *projectService, runtime *codex.Client) planning.Planner {
	return aiPlanner{projects: projects, runtime: runtime}
}

func (p aiPlanner) Generate(ctx context.Context, request planning.Request) (planning.Result, error) {
	instructions := p.projects.scopedInstructionProvider("team-lead", request.ProjectPath)
	switch p.projects.selectedAIProvider(p.runtime) {
	case "claude-code":
		return planning.NewClaudePlannerWithRuntime(instructions, p.projects.runtimeProvider("claude-code")).Generate(ctx, request)
	case "github-copilot":
		return planning.NewCopilotPlannerWithRuntime(instructions, p.projects.runtimeProvider("github-copilot")).Generate(ctx, request)
	}
	return planning.NewCodexPlannerWithRuntime(p.runtime, instructions, p.projects.runtimeProvider("codex")).Generate(ctx, request)
}

type aiDesigner struct {
	projects *projectService
	runtime  *codex.Client
}

func newAIDesigner(projects *projectService, runtime *codex.Client) design.Designer {
	return aiDesigner{projects: projects, runtime: runtime}
}

func (d aiDesigner) Design(ctx context.Context, request design.Request) (design.Result, error) {
	instructions := d.projects.scopedInstructionProvider("designer", request.ProjectPath)
	switch d.projects.selectedAIProvider(d.runtime) {
	case "claude-code":
		return design.NewClaudeDesignerWithRuntime(instructions, d.projects.runtimeProvider("claude-code")).Design(ctx, request)
	case "github-copilot":
		return design.NewCopilotDesignerWithRuntime(instructions, d.projects.runtimeProvider("github-copilot")).Design(ctx, request)
	}
	return design.NewCodexDesignerWithRuntime(d.runtime, instructions, d.projects.runtimeProvider("codex")).Design(ctx, request)
}

type aiDeveloper struct {
	projects *projectService
	runtime  *codex.Client
}

func newAIDeveloper(projects *projectService, runtime *codex.Client) development.Developer {
	return aiDeveloper{projects: projects, runtime: runtime}
}

func (d aiDeveloper) Implement(ctx context.Context, request development.Request) (development.Result, error) {
	instructions := d.projects.scopedInstructionProvider("developer", request.ProjectPath)
	switch d.projects.selectedAIProvider(d.runtime) {
	case "claude-code":
		return development.NewClaudeDeveloperWithRuntime(instructions, d.projects.runtimeProvider("claude-code")).Implement(ctx, request)
	case "github-copilot":
		return development.NewCopilotDeveloperWithRuntime(instructions, d.projects.runtimeProvider("github-copilot")).Implement(ctx, request)
	}
	return development.NewCodexDeveloperWithRuntime(d.runtime, instructions, d.projects.runtimeProvider("codex")).Implement(ctx, request)
}

func (d aiDeveloper) RepairBuild(ctx context.Context, request development.BuildRepairRequest) (development.Result, error) {
	instructions := d.projects.scopedInstructionProvider("developer", request.Request.ProjectPath)
	switch d.projects.selectedAIProvider(d.runtime) {
	case "claude-code":
		return development.NewClaudeDeveloperWithRuntime(instructions, d.projects.runtimeProvider("claude-code")).RepairBuild(ctx, request)
	case "github-copilot":
		return development.NewCopilotDeveloperWithRuntime(instructions, d.projects.runtimeProvider("github-copilot")).RepairBuild(ctx, request)
	}
	return development.NewCodexDeveloperWithRuntime(d.runtime, instructions, d.projects.runtimeProvider("codex")).RepairBuild(ctx, request)
}

func (d aiDeveloper) RepairGitDelivery(ctx context.Context, request development.GitRepairRequest) (development.Result, error) {
	instructions := d.projects.scopedInstructionProvider("developer", request.Request.ProjectPath)
	switch d.projects.selectedAIProvider(d.runtime) {
	case "claude-code":
		return development.NewClaudeDeveloperWithRuntime(instructions, d.projects.runtimeProvider("claude-code")).RepairGitDelivery(ctx, request)
	case "github-copilot":
		return development.NewCopilotDeveloperWithRuntime(instructions, d.projects.runtimeProvider("github-copilot")).RepairGitDelivery(ctx, request)
	}
	return development.NewCodexDeveloperWithRuntime(d.runtime, instructions, d.projects.runtimeProvider("codex")).RepairGitDelivery(ctx, request)
}

type aiQA struct {
	projects *projectService
	runtime  *codex.Client
}

func newAIQA(projects *projectService, runtime *codex.Client) quality.QA {
	return aiQA{projects: projects, runtime: runtime}
}

func (q aiQA) Verify(ctx context.Context, request quality.Request) (quality.Result, error) {
	instructions := q.projects.scopedInstructionProvider("qa", request.ProjectPath)
	switch q.projects.selectedAIProvider(q.runtime) {
	case "claude-code":
		return quality.NewClaudeQAWithRuntime(instructions, q.projects.runtimeProvider("claude-code")).Verify(ctx, request)
	case "github-copilot":
		return quality.NewCopilotQAWithRuntime(instructions, q.projects.runtimeProvider("github-copilot")).Verify(ctx, request)
	}
	return quality.NewCodexQAWithRuntime(q.runtime, instructions, q.projects.runtimeProvider("codex")).Verify(ctx, request)
}

func (s *projectService) scopedInstructionProvider(role, projectPath string) func() (string, error) {
	return func() (string, error) {
		return s.effectiveRoleInstructionsForProjectPath(role, projectPath)
	}
}

func (s *projectService) runtimeProvider(provider string) func() (string, string) {
	return func() (string, string) {
		config := s.agentRuntimeConfig(provider)
		return config.Model, config.Effort
	}
}

func (s *server) selectedAIProvider() string {
	return s.projectService.selectedAIProvider(s.codexRuntime)
}

func (s *server) aiRuntimeAttributes(provider string) map[string]any {
	config := s.projectService.agentRuntimeConfig(provider)
	return map[string]any{"provider": provider, "model": config.Model, "reasoningEffort": config.Effort}
}

func (s *projectService) selectedAIProvider(runtime *codex.Client) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.readSettings()
	if err != nil {
		if runtime == nil {
			return "claude-code"
		}
		return "codex"
	}
	switch stored.AIProvider {
	case "claude-code", "github-copilot":
		return stored.AIProvider
	}
	if runtime == nil && stored.AIFallbackEnabled {
		switch stored.AIFallbackProvider {
		case "claude-code", "github-copilot":
			return stored.AIFallbackProvider
		}
	}
	return "codex"
}
