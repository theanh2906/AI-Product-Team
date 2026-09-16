package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

type correlationContextKey struct{}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += written
	return written, err
}

func (s *server) withObservability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if correlationID == "" {
			correlationID = observability.NewCorrelationID()
		}
		w.Header().Set("X-Correlation-ID", correlationID)
		recorder := &statusRecorder{ResponseWriter: w}
		ctx := context.WithValue(r.Context(), correlationContextKey{}, correlationID)
		next.ServeHTTP(recorder, r.WithContext(ctx))
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		level := observability.LevelInfo
		outcome := "success"
		if status >= 500 {
			level, outcome = observability.LevelError, "failed"
		} else if status >= 400 {
			level, outcome = observability.LevelWarn, "rejected"
		}
		projectID := r.PathValue("projectID")
		s.observability.Record(observability.Event{
			Level: level, Category: "http", Name: "http.request", Message: r.Method + " " + r.URL.Path,
			CorrelationID: correlationID, ProjectID: projectID, Stage: "request", Outcome: outcome,
			DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"method": r.Method, "path": r.URL.Path, "status": status, "responseBytes": recorder.bytes},
		})
	})
}

func correlationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationContextKey{}).(string)
	return value
}

func (s *server) getObservabilityEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}
	events, err := s.observability.List(observability.Query{
		ProjectID: strings.TrimSpace(r.URL.Query().Get("projectId")), CorrelationID: strings.TrimSpace(r.URL.Query().Get("correlationId")),
		Level: observability.Level(strings.TrimSpace(r.URL.Query().Get("level"))), Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Since: time.Now().UTC().AddDate(0, 0, -days), Limit: limit,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *server) streamObservabilityEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming is unavailable."})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	updates, unsubscribe := s.observability.Subscribe()
	defer unsubscribe()
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case event := <-updates:
			writeSSE(w, "observation", event)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) getObservabilityOverview(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	overview, err := s.buildOverview(r.Context(), projectID, days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *server) buildOverview(ctx context.Context, projectID string, days int) (observability.Overview, error) {
	boards, err := s.boards.ListBoards(ctx)
	if err != nil {
		return observability.Overview{}, err
	}
	featureFeasibility := map[string]int{"high": 0, "medium": 0, "exploratory": 0}
	featureStatus := map[string]int{"suggested": 0, "backlog": 0, "planning": 0, "done": 0}
	bugSeverity := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	bugStatus := map[string]int{"suggested": 0, "backlog": 0, "planning": 0, "done": 0}
	delivery := map[string]int{"frontend": 0, "backend": 0, "fullstack": 0}
	agents := map[string]int{"designer": 0, "developer": 0, "qa": 0}
	metrics := observability.Metrics{}
	approved, decided := 0, 0
	leadHours := 0.0
	leadCount := 0
	featurePlanningOverrides, featureDoneOverrides := 0, 0
	bugPlanningOverrides, bugDoneOverrides := 0, 0
	projectIDs := make(map[string]struct{})
	for _, board := range boards {
		if projectID != "" && board.ProjectID != projectID {
			continue
		}
		projectIDs[board.ProjectID] = struct{}{}
		for _, item := range board.Backlog {
			if item.Status == kanban.BacklogOpen {
				metrics.BacklogItems++
			}
			status := string(item.Status)
			done := item.PlanID != "" && planTasksCompleted(board, item.PlanID)
			if item.Type == kanban.BacklogFeature {
				if item.Source == "feature-radar" {
					if done {
						featureDoneOverrides++
					} else if item.Status == kanban.BacklogPlanning {
						featurePlanningOverrides++
					}
				} else {
					metrics.TotalFeatures++
					if done {
						status = "done"
					}
					featureStatus[status]++
				}
			} else if item.Type == kanban.BacklogBug {
				if item.Source == "source-scan" {
					if done {
						bugDoneOverrides++
					} else if item.Status == kanban.BacklogPlanning {
						bugPlanningOverrides++
					}
				} else {
					metrics.TotalBugs++
					if done {
						status = "done"
					}
					bugStatus[status]++
				}
			}
		}
		for _, plan := range board.Plans {
			if plan.Status == kanban.PlanningApproved {
				approved++
				decided++
			}
			if plan.Status == kanban.PlanningAwaitingApproval || plan.Status == kanban.PlanningChangesRequested || plan.Status == kanban.PlanningFailed {
				metrics.ActivePlans++
			}
			if plan.Status == kanban.PlanningChangesRequested || plan.Status == kanban.PlanningFailed {
				metrics.ReworkCount++
			}
			for _, review := range plan.Reviews {
				if review.Decision == "deny" {
					metrics.ReworkCount++
					decided++
				}
			}
		}
		for _, task := range board.Tasks {
			agents[string(task.Role)]++
			if task.Status == kanban.TaskCompleted {
				metrics.CompletedTasks++
				leadHours += task.UpdatedAt.Sub(task.CreatedAt).Hours()
				leadCount++
			}
		}
	}
	applyStatusOverrides(featureStatus, featurePlanningOverrides, featureDoneOverrides)
	applyStatusOverrides(bugStatus, bugPlanningOverrides, bugDoneOverrides)
	if projectID != "" {
		projectIDs[projectID] = struct{}{}
	}
	for id := range projectIDs {
		records, listErr := s.insights.ListProject(ctx, id)
		if listErr != nil {
			return observability.Overview{}, listErr
		}
		for _, record := range records {
			switch record.Kind {
			case insights.KindFeatureRadar:
				var payload struct {
					Suggestions []struct {
						Feasibility            int `json:"feasibility"`
						Status, DeliveryTarget string
					} `json:"suggestions"`
				}
				if json.Unmarshal(record.Payload, &payload) != nil {
					continue
				}
				for _, item := range payload.Suggestions {
					metrics.TotalFeatures++
					featureStatus[normalizedInsightStatus(item.Status)]++
					delivery[item.DeliveryTarget]++
					if item.Feasibility >= 80 {
						featureFeasibility["high"]++
					} else if item.Feasibility >= 60 {
						featureFeasibility["medium"]++
					} else {
						featureFeasibility["exploratory"]++
					}
				}
			case insights.KindBugScan:
				var payload struct {
					Findings []struct{ Severity, Status string } `json:"findings"`
				}
				if json.Unmarshal(record.Payload, &payload) != nil {
					continue
				}
				for _, item := range payload.Findings {
					metrics.TotalBugs++
					bugSeverity[item.Severity]++
					bugStatus[normalizedInsightStatus(item.Status)]++
				}
			}
		}
	}
	if decided > 0 {
		metrics.PlanningApprovalRate = float64(approved) / float64(decided) * 100
	}
	if leadCount > 0 {
		metrics.AverageTaskLeadHours = leadHours / float64(leadCount)
	}
	events, err := s.observability.List(observability.Query{ProjectID: projectID, Since: time.Now().UTC().AddDate(0, 0, -days), Limit: 2000})
	if err != nil {
		return observability.Overview{}, err
	}
	success, failed, durationTotal, durationCount := 0, 0, int64(0), 0
	trend := make(map[string]*observability.TrendPoint)
	for _, event := range events {
		date := event.Timestamp.Format("2006-01-02")
		if trend[date] == nil {
			trend[date] = &observability.TrendPoint{Date: date}
		}
		if event.Name == "ai.job.completed" {
			success++
			durationTotal += event.DurationMS
			durationCount++
		}
		if event.Name == "ai.job.failed" {
			failed++
			durationTotal += event.DurationMS
			durationCount++
		}
		if event.Name == "backlog.feature.added" {
			trend[date].Features++
		}
		if event.Name == "backlog.bug.added" {
			trend[date].Bugs++
		}
		if event.Name == "task.completed" {
			trend[date].Done++
		}
	}
	if success+failed > 0 {
		metrics.AIJobSuccessRate = float64(success) / float64(success+failed) * 100
	}
	if durationCount > 0 {
		metrics.AverageAIJobSeconds = float64(durationTotal) / float64(durationCount) / 1000
	}
	trendItems := make([]observability.TrendPoint, 0, days)
	for index := days - 1; index >= 0; index-- {
		date := time.Now().UTC().AddDate(0, 0, -index).Format("2006-01-02")
		if trend[date] != nil {
			trendItems = append(trendItems, *trend[date])
		} else {
			trendItems = append(trendItems, observability.TrendPoint{Date: date})
		}
	}
	recent := events
	if len(recent) > 12 {
		recent = recent[:12]
	}
	return observability.Overview{GeneratedAt: time.Now().UTC(), RangeDays: days, Metrics: metrics,
		FeatureFeasibility: distribution(featureFeasibility, []string{"high", "medium", "exploratory"}), FeatureStatus: distribution(featureStatus, []string{"suggested", "backlog", "planning", "done"}),
		BugSeverity: distribution(bugSeverity, []string{"critical", "high", "medium", "low"}), BugStatus: distribution(bugStatus, []string{"suggested", "backlog", "planning", "done"}),
		DeliveryTargets: distribution(delivery, []string{"frontend", "backend", "fullstack"}), AgentWorkload: distribution(agents, []string{"designer", "developer", "qa"}), Trend: trendItems, RecentEvents: recent}, nil
}

func normalizedInsightStatus(status string) string {
	if status == "" {
		return "suggested"
	}
	return status
}

func planTasksCompleted(board kanban.Board, planID string) bool {
	found := false
	for _, task := range board.Tasks {
		if task.PlanID != planID {
			continue
		}
		found = true
		if task.Status != kanban.TaskCompleted {
			return false
		}
	}
	return found
}

func applyStatusOverrides(status map[string]int, planning, done int) {
	for range planning + done {
		if status["backlog"] > 0 {
			status["backlog"]--
		}
	}
	status["planning"] += planning
	status["done"] += done
}
func distribution(values map[string]int, order []string) []observability.DistributionItem {
	items := make([]observability.DistributionItem, 0, len(order))
	for _, key := range order {
		items = append(items, observability.DistributionItem{Key: key, Label: strings.ReplaceAll(strings.Title(key), "_", " "), Value: values[key]})
	}
	return items
}
