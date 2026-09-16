package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	agentModelOptionsCacheTTL        = 10 * time.Minute
	agentModelOptionsProviderTimeout = 12 * time.Second
	agentModelOptionsCommandTimeout  = 8 * time.Second
)

type agentModelOption struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Description   string   `json:"description,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	Efforts       []string `json:"efforts"`
	Source        string   `json:"source"`
}

type agentModelOptionGroup struct {
	Provider string             `json:"provider"`
	Source   string             `json:"source"`
	Error    string             `json:"error,omitempty"`
	Models   []agentModelOption `json:"models"`
	Efforts  []string           `json:"efforts"`
}

type agentModelOptionsResponse struct {
	Providers []agentModelOptionGroup `json:"providers"`
}

func (s *server) getAgentModelOptions(w http.ResponseWriter, r *http.Request) {
	refresh := strings.EqualFold(r.URL.Query().Get("refresh"), "true")
	if !refresh {
		s.agentModelsMu.Lock()
		if len(s.agentModelsCache.Providers) > 0 && time.Since(s.agentModelsCached) < agentModelOptionsCacheTTL {
			cached := s.agentModelsCache
			s.agentModelsMu.Unlock()
			writeJSON(w, http.StatusOK, cached)
			return
		}
		s.agentModelsMu.Unlock()
	}

	response := loadAgentModelOptions(r.Context())
	s.agentModelsMu.Lock()
	s.agentModelsCache = response
	s.agentModelsCached = time.Now()
	s.agentModelsMu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func loadAgentModelOptions(ctx context.Context) agentModelOptionsResponse {
	loaders := []func(context.Context) agentModelOptionGroup{
		loadCodexModelOptions,
		loadClaudeModelOptions,
		loadCopilotModelOptions,
	}
	providers := make([]agentModelOptionGroup, len(loaders))
	var wg sync.WaitGroup
	for index, loader := range loaders {
		wg.Add(1)
		go func(index int, loader func(context.Context) agentModelOptionGroup) {
			defer wg.Done()
			providerContext, cancel := context.WithTimeout(ctx, agentModelOptionsProviderTimeout)
			defer cancel()
			providers[index] = normalizeAgentModelGroup(loader(providerContext))
		}(index, loader)
	}
	wg.Wait()
	return agentModelOptionsResponse{Providers: providers}
}

func loadCodexModelOptions(ctx context.Context) agentModelOptionGroup {
	group := newAgentModelOptionGroup("codex", "codex debug models")
	output, err := exec.CommandContext(ctx, "codex", "debug", "models").Output()
	if err != nil {
		group.Error = fmt.Sprintf("Codex CLI did not return model catalog: %s", commandError(err))
		return group
	}
	var catalog struct {
		Models []struct {
			Slug                    string `json:"slug"`
			DisplayName             string `json:"display_name"`
			Description             string `json:"description"`
			Visibility              string `json:"visibility"`
			DefaultReasoningLevel   string `json:"default_reasoning_level"`
			SupportedReasoningLevel []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(output, &catalog); err != nil {
		group.Error = "Codex CLI model catalog was not valid JSON."
		return group
	}
	for _, model := range catalog.Models {
		if strings.TrimSpace(model.Slug) == "" || strings.EqualFold(model.Visibility, "hide") {
			continue
		}
		efforts := make([]string, 0, len(model.SupportedReasoningLevel))
		for _, level := range model.SupportedReasoningLevel {
			if effort := strings.TrimSpace(level.Effort); effort != "" {
				efforts = append(efforts, effort)
			}
		}
		group.Models = append(group.Models, agentModelOption{
			ID: strings.TrimSpace(model.Slug), Label: firstNonEmpty(model.DisplayName, model.Slug),
			Description: strings.TrimSpace(model.Description), DefaultEffort: strings.TrimSpace(model.DefaultReasoningLevel),
			Efforts: uniqueStrings(efforts), Source: group.Source,
		})
		group.Efforts = append(group.Efforts, efforts...)
	}
	sort.Slice(group.Models, func(i, j int) bool { return group.Models[i].ID < group.Models[j].ID })
	group.Efforts = uniqueStrings(group.Efforts)
	return group
}

func loadClaudeModelOptions(ctx context.Context) agentModelOptionGroup {
	group := newAgentModelOptionGroup("claude-code", "claude --help")
	output, err := commandOutput(ctx, "claude", "--help")
	if err != nil {
		group.Error = fmt.Sprintf("Claude Code CLI did not return help output: %s", commandError(err))
		if strings.TrimSpace(output) == "" {
			return group
		}
	}
	help := output
	efforts := extractClaudeEfforts(help)
	models := extractClaudeModelAliases(help)
	for _, model := range models {
		group.Models = append(group.Models, agentModelOption{
			ID: model, Label: titleCase(model), DefaultEffort: "high",
			Efforts: efforts, Source: group.Source,
		})
	}
	group.Efforts = efforts
	return group
}

func loadCopilotModelOptions(ctx context.Context) agentModelOptionGroup {
	group := newAgentModelOptionGroup("github-copilot", "gh copilot -- --help")

	type commandResult struct {
		output string
		err    error
	}
	helpResult := make(chan commandResult, 1)
	configResult := make(chan commandResult, 1)
	go func() {
		output, err := commandOutput(ctx, "gh", "copilot", "--", "--help")
		helpResult <- commandResult{output: output, err: err}
	}()
	go func() {
		output, err := commandOutput(ctx, "gh", "copilot", "--", "help", "config")
		configResult <- commandResult{output: output, err: err}
	}()

	helpCommand := <-helpResult
	configCommand := <-configResult
	if helpCommand.err != nil {
		group.Error = fmt.Sprintf("GitHub Copilot CLI did not return help output: %s", commandError(helpCommand.err))
	}
	help := helpCommand.output
	efforts := extractCopilotEfforts(help)
	if len(efforts) == 0 {
		efforts = []string{"high"}
	}
	addCopilotAuto := strings.TrimSpace(help) == "" || strings.Contains(extractFlagHelpBlock(help, "--model <model>"), "'auto'")
	if addCopilotAuto {
		group.Models = append(group.Models, agentModelOption{ID: "auto", Label: "Auto", Description: "Let GitHub Copilot choose the model and supported reasoning behavior.", Efforts: []string{}, Source: group.Source})
	}

	if configCommand.err != nil {
		message := fmt.Sprintf("GitHub Copilot CLI did not return config help output: %s", commandError(configCommand.err))
		if group.Error != "" {
			group.Error += "; " + message
		} else {
			group.Error = message
		}
	}
	group.Source = "gh copilot -- help config"
	for _, model := range extractCopilotModelNames(configCommand.output) {
		group.Models = append(group.Models, agentModelOption{
			ID: model, Label: model, DefaultEffort: "high",
			Efforts: efforts, Source: group.Source,
		})
	}
	group.Efforts = efforts
	return group
}

func newAgentModelOptionGroup(provider, source string) agentModelOptionGroup {
	return agentModelOptionGroup{Provider: provider, Source: source, Models: []agentModelOption{}, Efforts: []string{}}
}

func normalizeAgentModelGroup(group agentModelOptionGroup) agentModelOptionGroup {
	if group.Models == nil {
		group.Models = []agentModelOption{}
	}
	if group.Efforts == nil {
		group.Efforts = []string{}
	}
	for index := range group.Models {
		if group.Models[index].Efforts == nil {
			group.Models[index].Efforts = []string{}
		}
	}
	return group
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, agentModelOptionsCommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(commandContext, name, args...).CombinedOutput()
	return string(output), err
}

func extractClaudeModelAliases(help string) []string {
	modelHelp := extractFlagHelpBlock(help, "--model <model>")
	aliasExample := regexp.MustCompile(`(?s)alias for the latest model.*?\(e\.g\.\s*([^)]+)\)`).FindStringSubmatch(modelHelp)
	if len(aliasExample) < 2 {
		return nil
	}
	pattern := regexp.MustCompile(`'([a-z][a-z0-9-]*)'`)
	matches := pattern.FindAllStringSubmatch(aliasExample[1], -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if value := strings.TrimSpace(match[1]); value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func extractClaudeEfforts(help string) []string {
	effortHelp := extractFlagHelpBlock(help, "--effort <level>")
	match := regexp.MustCompile(`\(([^)]+)\)`).FindStringSubmatch(effortHelp)
	if len(match) < 2 {
		return nil
	}
	parts := strings.Split(match[1], ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			result = append(result, value)
		}
	}
	return uniqueStrings(result)
}

func extractCopilotEfforts(help string) []string {
	effortHelp := extractFlagHelpBlock(help, "--effort, --reasoning-effort <level>")
	if effortHelp == "" {
		effortHelp = extractFlagHelpBlock(help, "--reasoning-effort <level>")
	}
	pattern := regexp.MustCompile(`"([a-z]+)"`)
	matches := pattern.FindAllStringSubmatch(effortHelp, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if value := strings.TrimSpace(match[1]); value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func extractCopilotModelNames(help string) []string {
	start := strings.Index(help, "`model`:")
	if start < 0 {
		return nil
	}
	block := help[start:]
	if end := strings.Index(block, "\n\n  `contextTier`:"); end >= 0 {
		block = block[:end]
	}
	pattern := regexp.MustCompile(`(?m)^\s*-\s+"([^"]+)"`)
	matches := pattern.FindAllStringSubmatch(block, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if value := strings.TrimSpace(match[1]); value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func extractFlagHelpBlock(help string, flag string) string {
	index := strings.Index(help, flag)
	if index < 0 {
		return ""
	}
	nextFlag := regexp.MustCompile(`^  (?:-[a-zA-Z],\s+)?--[a-zA-Z0-9-]`)
	lines := strings.Split(help[index:], "\n")
	block := make([]string, 0, len(lines))
	for i, line := range lines {
		if i > 0 && nextFlag.MatchString(line) {
			break
		}
		block = append(block, strings.TrimSpace(line))
	}
	return strings.Join(block, " ")
}

func commandError(err error) string {
	if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
		return truncateMessage(string(exit.Stderr))
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func titleCase(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
