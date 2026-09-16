package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func (s *server) recordAIUsage(traceID, projectID, entityType, entityID, agent string, collector *agentusage.Collector) {
	usage := collector.Snapshot()
	if usage.Samples == 0 {
		return
	}
	confidence := "exact"
	if usage.Partial {
		confidence = "partial"
	}
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.usage", Message: "Provider usage received", CorrelationID: traceID, ProjectID: projectID, EntityType: entityType, EntityID: entityID, Agent: agent, Stage: "usage", Outcome: "success", Attributes: map[string]any{
		"inputTokens": usage.InputTokens, "cachedInputTokens": usage.CachedInputTokens, "cacheWriteInputTokens": usage.CacheWriteInputTokens,
		"outputTokens": usage.OutputTokens, "reasoningTokens": usage.ReasoningTokens, "totalTokens": usage.TotalTokens,
		"costUsd": usage.CostUSD, "usageConfidence": confidence,
	}})
}

func (s *server) getAISessionOverview(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	events, err := s.observability.List(observability.Query{ProjectID: projectID, Category: "ai", Since: time.Now().UTC().AddDate(0, 0, -days), Limit: 5000})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	boards, err := s.boards.ListBoards(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, buildAISessionOverview(events, boards, days, time.Now().UTC()))
}

type aiEntityLabel struct{ key, title, project string }

func buildAISessionOverview(events []observability.Event, boards []kanban.Board, days int, now time.Time) observability.AISessionOverview {
	labels := make(map[string]aiEntityLabel)
	for _, board := range boards {
		for _, task := range board.Tasks {
			labels[board.ProjectID+"/task/"+task.ID] = aiEntityLabel{task.Key, task.Title, board.ProjectName}
		}
		for _, plan := range board.Plans {
			labels[board.ProjectID+"/plan/"+plan.ID] = aiEntityLabel{"PLAN", plan.Request.Title, board.ProjectName}
		}
	}
	byID := make(map[string]*observability.AISessionRun)
	for index := len(events) - 1; index >= 0; index-- { // repositories return newest first
		event := events[index]
		if !strings.HasPrefix(event.Name, "ai.job.") || event.CorrelationID == "" {
			continue
		}
		runID := event.CorrelationID + "/" + event.EntityType + "/" + event.EntityID + "/" + event.Agent
		run := byID[runID]
		if run == nil {
			label := labels[event.ProjectID+"/"+event.EntityType+"/"+event.EntityID]
			run = &observability.AISessionRun{ID: runID, CorrelationID: event.CorrelationID, ProjectID: event.ProjectID, ProjectName: label.project, EntityType: event.EntityType, EntityID: event.EntityID, EntityKey: label.key, Title: label.title, Agent: event.Agent, Stage: event.Stage, Status: "queued", StartedAt: event.Timestamp, Usage: observability.AIUsage{Confidence: "unavailable"}, Events: make([]observability.Event, 0, 4)}
			byID[runID] = run
		}
		run.Events = append(run.Events, observability.Event{ID: event.ID, Timestamp: event.Timestamp, Level: event.Level, Category: event.Category, Name: event.Name, Message: event.Message, Stage: event.Stage, Outcome: event.Outcome, DurationMS: event.DurationMS})
		if run.ProjectName == "" {
			run.ProjectName = stringAttribute(event.Attributes, "projectName")
		}
		if run.Title == "" {
			run.Title = stringAttribute(event.Attributes, "title")
		}
		if run.EntityKey == "" {
			run.EntityKey = stringAttribute(event.Attributes, "entityKey", "taskKey")
		}
		if provider := stringAttribute(event.Attributes, "provider"); provider != "" {
			run.Provider = provider
		}
		if model := stringAttribute(event.Attributes, "model"); model != "" {
			run.Model = model
		}
		if effort := stringAttribute(event.Attributes, "reasoningEffort", "effort"); effort != "" {
			run.Effort = effort
		}
		if threadID := stringAttribute(event.Attributes, "threadId"); threadID != "" {
			run.ThreadID = threadID
		}
		if resumed, ok := event.Attributes["resumed"].(bool); ok {
			run.Resumed = resumed
		}
		mergeAIUsage(&run.Usage, event.Attributes)
		switch event.Name {
		case "ai.job.started":
			run.StartedAt = event.Timestamp
			run.Status = "queued"
		case "ai.job.acquired":
			run.Status = "running"
		case "ai.job.completed":
			completed := event.Timestamp
			run.CompletedAt, run.Status, run.DurationMS = &completed, "completed", event.DurationMS
		case "ai.job.failed":
			completed := event.Timestamp
			run.CompletedAt, run.Status, run.DurationMS = &completed, "failed", event.DurationMS
			run.Error = stringAttribute(event.Attributes, "error")
		}
	}
	runs := make([]observability.AISessionRun, 0, len(byID))
	for _, run := range byID {
		if run.DurationMS <= 0 {
			end := now
			if run.CompletedAt != nil {
				end = *run.CompletedAt
			}
			run.DurationMS = end.Sub(run.StartedAt).Milliseconds()
		}
		if run.Provider == "" {
			run.Provider = "unknown"
		}
		if run.Model == "" {
			run.Model = "not reported"
		}
		if (run.Status == "running" || run.Status == "queued") && now.Sub(run.StartedAt) > 2*time.Hour {
			run.Status = "interrupted"
			run.Error = "No terminal lifecycle event was recorded before the runtime stopped."
		}
		runs = append(runs, *run)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if (runs[i].Status == "running" || runs[i].Status == "queued") != (runs[j].Status == "running" || runs[j].Status == "queued") {
			return runs[i].Status == "running" || runs[i].Status == "queued"
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return summarizeAISessions(runs, days, now)
}

func summarizeAISessions(runs []observability.AISessionRun, days int, now time.Time) observability.AISessionOverview {
	result := observability.AISessionOverview{GeneratedAt: now, RangeDays: days, Runs: runs}
	durations := make([]int64, 0, len(runs))
	type comparisonAccumulator struct {
		item      observability.AIModelComparison
		durations []int64
		successes int
	}
	type providerAccumulator struct {
		item        observability.AIProviderUsage
		durations   []int64
		successes   int
		modelRuns   map[string]int
		hasExact    bool
		hasPartial  bool
		hasUsage    bool
		hasCost     bool
		latestError time.Time
	}
	groups := make(map[string]*comparisonAccumulator)
	providers := make(map[string]*providerAccumulator)
	totalTrackedTokens := int64(0)
	for _, run := range runs {
		active := run.Status == "running" || run.Status == "queued"
		if active {
			result.Metrics.ActiveRuns++
		} else {
			result.Metrics.CompletedRuns++
			durations = append(durations, run.DurationMS)
		}
		if run.Status == "completed" {
			result.Metrics.SuccessRate++
		}
		if run.Usage.TotalTokens != nil {
			result.Metrics.TrackedTokens += *run.Usage.TotalTokens
			result.Metrics.UsageTrackedRuns++
			totalTrackedTokens += *run.Usage.TotalTokens
		}
		key := run.Provider + "\x00" + run.Model
		group := groups[key]
		if group == nil {
			group = &comparisonAccumulator{item: observability.AIModelComparison{Provider: run.Provider, Model: run.Model}}
			groups[key] = group
		}
		group.item.Runs++
		if !active {
			group.durations = append(group.durations, run.DurationMS)
		}
		if run.Status == "completed" {
			group.successes++
		}
		if run.Usage.TotalTokens != nil {
			group.item.TrackedTokens += *run.Usage.TotalTokens
			group.item.UsageTrackedRuns++
		}
		providerKey := canonicalProviderKey(run.Provider)
		provider := providers[providerKey]
		if provider == nil {
			provider = &providerAccumulator{
				item:      defaultProviderUsage(providerKey),
				modelRuns: make(map[string]int),
			}
			providers[providerKey] = provider
		}
		provider.item.Runs++
		provider.modelRuns[run.Model]++
		if active {
			provider.item.ActiveRuns++
		} else {
			provider.item.CompletedRuns++
			provider.durations = append(provider.durations, run.DurationMS)
		}
		if run.Status == "completed" {
			provider.successes++
		}
		if run.Status == "failed" || run.Status == "interrupted" {
			provider.item.FailedRuns++
			errorTime := run.StartedAt
			if run.CompletedAt != nil {
				errorTime = *run.CompletedAt
			}
			if errorTime.After(provider.latestError) {
				provider.latestError = errorTime
				provider.item.LastError = run.Error
			}
		}
		if provider.item.LastUsedAt == nil || run.StartedAt.After(*provider.item.LastUsedAt) {
			usedAt := run.StartedAt
			provider.item.LastUsedAt = &usedAt
		}
		if run.Usage.TotalTokens != nil {
			provider.hasUsage = true
			provider.item.TrackedTokens += *run.Usage.TotalTokens
			provider.item.UsageTrackedRuns++
			if run.Usage.CostUSD != nil {
				provider.hasCost = true
				provider.item.CostUSD += *run.Usage.CostUSD
			}
			switch run.Usage.Confidence {
			case "partial":
				provider.hasPartial = true
			case "exact":
				provider.hasExact = true
			default:
				provider.hasPartial = true
			}
		}
	}
	terminal := result.Metrics.CompletedRuns
	if terminal > 0 {
		result.Metrics.SuccessRate = result.Metrics.SuccessRate / float64(terminal) * 100
	}
	result.Metrics.MedianDurationMS = medianInt64(durations)
	for _, group := range groups {
		if len(group.durations) > 0 {
			group.item.SuccessRate = float64(group.successes) / float64(len(group.durations)) * 100
		}
		group.item.MedianDurationMS = medianInt64(group.durations)
		result.Comparisons = append(result.Comparisons, group.item)
	}
	sort.SliceStable(result.Comparisons, func(i, j int) bool { return result.Comparisons[i].Runs > result.Comparisons[j].Runs })
	for _, provider := range providers {
		terminalRuns := provider.item.CompletedRuns
		if terminalRuns > 0 {
			provider.item.SuccessRate = float64(provider.successes) / float64(terminalRuns) * 100
		}
		provider.item.MedianDurationMS = medianInt64(provider.durations)
		provider.item.PrimaryModel = primaryModel(provider.modelRuns)
		if provider.hasUsage {
			if provider.hasPartial {
				provider.item.Confidence = "partial"
			} else if provider.hasExact {
				provider.item.Confidence = "exact"
			}
			if totalTrackedTokens > 0 {
				provider.item.LocalPressurePercent = float64(provider.item.TrackedTokens) / float64(totalTrackedTokens) * 100
			}
		}
		if !provider.hasCost {
			provider.item.CostUSD = 0
		}
		if provider.item.FailedRuns > 0 {
			provider.item.Status = "attention"
		} else if provider.item.ActiveRuns > 0 && provider.item.Status == "unavailable" {
			provider.item.Status = "active"
		}
		result.ProviderUsage = append(result.ProviderUsage, provider.item)
	}
	sort.SliceStable(result.ProviderUsage, func(i, j int) bool {
		iKnown := providerSortRank(result.ProviderUsage[i].Provider)
		jKnown := providerSortRank(result.ProviderUsage[j].Provider)
		if iKnown != jKnown {
			return iKnown < jKnown
		}
		if result.ProviderUsage[i].ActiveRuns != result.ProviderUsage[j].ActiveRuns {
			return result.ProviderUsage[i].ActiveRuns > result.ProviderUsage[j].ActiveRuns
		}
		return result.ProviderUsage[i].Runs > result.ProviderUsage[j].Runs
	})
	if len(result.Runs) > 60 {
		result.Runs = result.Runs[:60]
	}
	return result
}

func defaultProviderUsage(provider string) observability.AIProviderUsage {
	normalized := canonicalProviderKey(provider)
	item := observability.AIProviderUsage{
		Provider:   provider,
		Status:     "unavailable",
		Confidence: "unavailable",
	}
	switch normalized {
	case "codex":
		item.Provider = "codex"
	case "claude-code", "claude code", "claude":
		item.Provider = "claude-code"
	case "github-copilot", "github copilot", "github-copilot-cli":
		item.Provider = "github-copilot"
	case "", "unknown":
		item.Provider = "unknown"
	}
	return item
}

func canonicalProviderKey(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return "codex"
	case "claude-code", "claude code", "claude":
		return "claude-code"
	case "github-copilot", "github copilot", "github-copilot-cli":
		return "github-copilot"
	case "", "unknown":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func providerSortRank(provider string) int {
	switch canonicalProviderKey(provider) {
	case "codex":
		return 0
	case "claude-code":
		return 1
	case "github-copilot":
		return 2
	case "unknown", "":
		return 99
	default:
		return 50
	}
}

func primaryModel(models map[string]int) string {
	bestModel := ""
	bestCount := 0
	for model, count := range models {
		if count > bestCount || (count == bestCount && (bestModel == "" || model < bestModel)) {
			bestModel = model
			bestCount = count
		}
	}
	if strings.TrimSpace(bestModel) == "" {
		return "not reported"
	}
	return bestModel
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	middle := len(copyValues) / 2
	if len(copyValues)%2 == 1 {
		return copyValues[middle]
	}
	return (copyValues[middle-1] + copyValues[middle]) / 2
}

func stringAttribute(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attributes[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mergeAIUsage(usage *observability.AIUsage, attributes map[string]any) {
	usage.InputTokens = int64Attribute(attributes, "inputTokens")
	usage.CachedInputTokens = int64Attribute(attributes, "cachedInputTokens", "cacheReadTokens")
	usage.CacheWriteInputTokens = int64Attribute(attributes, "cacheWriteInputTokens", "cacheWriteTokens")
	usage.OutputTokens = int64Attribute(attributes, "outputTokens")
	usage.ReasoningTokens = int64Attribute(attributes, "reasoningTokens", "reasoningOutputTokens")
	usage.TotalTokens = int64Attribute(attributes, "totalTokens")
	if cost := float64Attribute(attributes, "costUsd", "costUSD"); cost != nil {
		usage.CostUSD = cost
	}
	if usage.TotalTokens == nil && (usage.InputTokens != nil || usage.OutputTokens != nil) {
		value := int64(0)
		if usage.InputTokens != nil {
			value += *usage.InputTokens
		}
		if usage.OutputTokens != nil {
			value += *usage.OutputTokens
		}
		usage.TotalTokens = &value
	}
	if usage.TotalTokens != nil {
		usage.Confidence = stringAttribute(attributes, "usageConfidence")
		if usage.Confidence == "" {
			usage.Confidence = "exact"
		}
	}
}

func float64Attribute(attributes map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		switch value := attributes[key].(type) {
		case float64:
			converted := value
			return &converted
		case float32:
			converted := float64(value)
			return &converted
		case int:
			converted := float64(value)
			return &converted
		case int64:
			converted := float64(value)
			return &converted
		}
	}
	return nil
}

func int64Attribute(attributes map[string]any, keys ...string) *int64 {
	for _, key := range keys {
		switch value := attributes[key].(type) {
		case float64:
			converted := int64(value)
			return &converted
		case int64:
			converted := value
			return &converted
		case int:
			converted := int64(value)
			return &converted
		}
	}
	return nil
}
