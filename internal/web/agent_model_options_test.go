package web

import (
	"strings"
	"testing"
)

func TestExtractClaudeModelOptionsFromHelp(t *testing.T) {
	help := "--model <model> Provide an alias for the latest model (e.g. 'fable', 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5').\n--effort <level> Effort level for the current session (low, medium, high, xhigh, max)"

	models := extractClaudeModelAliases(help)
	if len(models) != 3 || models[0] != "fable" || models[1] != "opus" || models[2] != "sonnet" {
		t.Fatalf("unexpected Claude model aliases: %v", models)
	}
	efforts := extractClaudeEfforts(help)
	if len(efforts) != 5 || efforts[0] != "low" || efforts[4] != "max" {
		t.Fatalf("unexpected Claude effort options: %v", efforts)
	}
}

func TestExtractCopilotModelOptionsFromHelp(t *testing.T) {
	help := `--effort, --reasoning-effort <level>  Set the reasoning effort level (choices:
                                        "none", "minimal", "low", "medium",
                                        "high", "xhigh", "max")
  --model <model>                       Set the AI model to use (use 'auto' to
                                        let Copilot pick automatically)`
	configHelp := "  `model`: AI model to use for Copilot CLI; can be changed with /model command or --model flag option.\n" + `
    - "claude-sonnet-5"
    - "gpt-5.6-sol"
    - "gpt-5.4"

  ` + "`contextTier`" + `: context window tier`

	group := agentModelOptionGroup{Provider: "github-copilot", Source: "gh copilot -- --help"}
	efforts := extractCopilotEfforts(help)
	if strings.Contains(extractFlagHelpBlock(help, "--model <model>"), "'auto'") {
		group.Models = append(group.Models, agentModelOption{
			ID: "auto", Label: "Auto", Efforts: []string{}, Source: group.Source,
		})
	}
	for _, model := range extractCopilotModelNames(configHelp) {
		group.Models = append(group.Models, agentModelOption{ID: model, Label: model, DefaultEffort: "high", Efforts: efforts})
	}

	if len(efforts) != 7 || efforts[0] != "none" || efforts[6] != "max" {
		t.Fatalf("unexpected Copilot effort options: %v", efforts)
	}
	if len(group.Models) != 4 || group.Models[0].ID != "auto" || group.Models[1].ID != "claude-sonnet-5" || group.Models[3].ID != "gpt-5.4" {
		t.Fatalf("unexpected Copilot models: %+v", group.Models)
	}
	if group.Models[0].DefaultEffort != "" || len(group.Models[0].Efforts) != 0 {
		t.Fatalf("auto model should use automatic effort: %+v", group.Models[0])
	}
	if group.Models[1].DefaultEffort != "high" || len(group.Models[1].Efforts) != len(efforts) {
		t.Fatalf("specific Copilot models should retain effort options: %+v", group.Models[1])
	}
}

func TestNormalizeAgentModelGroupReturnsEmptyArrays(t *testing.T) {
	group := normalizeAgentModelGroup(agentModelOptionGroup{
		Provider: "github-copilot",
		Source:   "gh copilot -- --help",
		Models:   []agentModelOption{{ID: "auto", Label: "Auto"}},
	})

	if group.Models == nil {
		t.Fatal("expected models to be an empty array, got nil")
	}
	if group.Efforts == nil {
		t.Fatal("expected efforts to be an empty array, got nil")
	}
	if group.Models[0].Efforts == nil {
		t.Fatal("expected model efforts to be an empty array, got nil")
	}
}

func TestLoadCodexModelOptionsFiltersHiddenModels(t *testing.T) {
	group := agentModelOptionGroup{Provider: "codex", Source: "codex debug models"}
	catalog := struct {
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
	}{
		Models: []struct {
			Slug                    string `json:"slug"`
			DisplayName             string `json:"display_name"`
			Description             string `json:"description"`
			Visibility              string `json:"visibility"`
			DefaultReasoningLevel   string `json:"default_reasoning_level"`
			SupportedReasoningLevel []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		}{
			{Slug: "hidden", DisplayName: "Hidden", Visibility: "hide"},
			{Slug: "gpt-visible", DisplayName: "GPT Visible", Visibility: "list", DefaultReasoningLevel: "medium", SupportedReasoningLevel: []struct {
				Effort string `json:"effort"`
			}{{Effort: "low"}, {Effort: "medium"}}},
		},
	}
	for _, model := range catalog.Models {
		if model.Visibility == "hide" {
			continue
		}
		group.Models = append(group.Models, agentModelOption{
			ID: model.Slug, Label: firstNonEmpty(model.DisplayName, model.Slug),
			DefaultEffort: model.DefaultReasoningLevel, Efforts: []string{model.SupportedReasoningLevel[0].Effort, model.SupportedReasoningLevel[1].Effort},
		})
	}
	if len(group.Models) != 1 || group.Models[0].ID != "gpt-visible" {
		t.Fatalf("hidden Codex models were not filtered: %+v", group.Models)
	}
}
