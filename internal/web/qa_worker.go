package web

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
	"github.com/theanh2906/AI-Product-Team/internal/quality"
)

const qaTaskTimeout = 90 * time.Minute

func (s *server) scheduleQueuedQATasks() {
	s.scheduleQueuedAgentTasks("qa_queue_recovery", "qa-queue-recovery")
}

func (s *server) enqueueQATask(projectID, taskID, traceID string) {
	s.scheduleQueuedAgentTasks("qa_task_queued", traceID)
}

func (s *server) runQATask(projectID, taskID, traceID string) {
	defer s.scheduleQueuedAgentTasks("qa_task_finished", traceID)
	started := time.Now()
	provider := s.selectedAIProvider()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "QA task queued for selected AI runtime", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "queued", Attributes: s.aiRuntimeAttributes(provider)})
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "QA acquired the sequential agent slot", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})

	board, err := s.boards.GetProjectBoard(context.Background(), projectID)
	if err != nil {
		s.recordQAFailure(traceID, projectID, taskID, "", started, err)
		return
	}
	task, plan, documents, ok := qaInputs(board, taskID)
	if !ok || task.Status != kanban.TaskQueued || task.Column != kanban.ColumnQA {
		return
	}
	task.ThreadID = taskThreadForRun(plan, task)
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		s.blockQATask(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	board, err = s.boards.UpdateTaskStatus(context.Background(), projectID, taskID, kanban.TaskInProgress, "")
	if err != nil {
		s.recordQAFailure(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	s.boardEvents.publish(board)

	ctx, cancel := context.WithTimeout(context.Background(), qaTaskTimeout)
	defer cancel()
	ctx, usageCollector := agentusage.WithCollector(ctx)
	defer s.recordAIUsage(traceID, projectID, "task", taskID, "qa", usageCollector)
	reporter := s.taskSessions.start(projectID, taskID, provider, "qa")
	ctx = agentstream.WithReporter(ctx, reporter)
	var sessionErr error
	defer func() { s.taskSessions.finish(projectID, taskID, sessionErr) }()
	sourceGuard, guardErr := s.beginTaskSourceGuard(ctx, project, taskID, traceID, "qa")
	if guardErr != nil {
		sessionErr = guardErr
		s.blockQATask(traceID, projectID, taskID, task.ThreadID, started, guardErr)
		return
	}
	result, runErr := s.qa.Verify(ctx, quality.Request{ProjectPath: project.executionPath(), AdditionalPaths: project.additionalExecutionPaths(), Plan: plan, Task: task, Documents: documents, DependencyReports: taskDependencyReports(board, task), ExistingBugs: existingQABugs(board)})
	guardErr = s.finishTaskSourceGuard(context.Background(), sourceGuard, project, taskID, traceID, "qa")
	if guardErr != nil {
		sessionErr = combineAgentAndGuardError(runErr, guardErr)
		s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, sessionErr)
		return
	}
	if runErr != nil {
		sessionErr = runErr
		s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, runErr)
		return
	}
	execution := qaExecution(result, nil)
	if result.Outcome == quality.OutcomePassed {
		board, err = s.boards.CompleteTask(context.Background(), projectID, taskID, result.ThreadID, execution)
		if err != nil {
			sessionErr = err
			s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, err)
			return
		}
		s.boardEvents.publish(board)
		s.persistTaskExecutionArtifact(project, board, taskID)
		s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.completed", Message: "QA verification passed", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "success", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"verification": result.Verification, "threadId": result.ThreadID}})
		s.notify(notifications.Draft{Level: notifications.LevelSuccess, Kind: "qa_passed", Title: task.Key + " passed QA", Message: qaPassMessage(result), ProjectID: projectID, ProjectName: project.Name, EntityID: taskID, Route: "/work-items"})
		return
	}
	if result.Outcome == quality.OutcomeBlocked {
		reason := "QA verification blocked: " + strings.TrimSpace(result.Summary)
		board, err = s.boards.BlockTaskWithExecution(context.Background(), projectID, taskID, result.ThreadID, reason, execution)
		if err != nil {
			sessionErr = err
			s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, err)
			return
		}
		s.boardEvents.publish(board)
		s.persistTaskExecutionArtifact(project, board, taskID)
		s.observability.Record(observability.Event{Level: observability.LevelWarn, Category: "ai", Name: "qa.verification_blocked", Message: "QA could not reach a reliable verdict", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "blocked", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"verification": result.Verification, "threadId": result.ThreadID}})
		s.notify(notifications.Draft{Level: notifications.LevelWarning, Kind: "qa_verification_blocked", Title: task.Key + " needs verification setup", Message: reason, ProjectID: projectID, ProjectName: project.Name, EntityID: taskID, Route: "/work-items"})
		return
	}

	createdBugCount := 0
	highlightedBugCount := 0
	matchedPlanningBugCount := 0
	linkedBugItems := make([]*kanban.BacklogItem, len(result.Bugs))
	for index, bug := range result.Bugs {
		reference := qaBugReference(taskID, bug.Title)
		if existing, found := findExistingQABug(board, bug, reference); found {
			var linked kanban.BacklogItem
			board, linked, err = s.boards.MarkBacklogItemReportedAgain(context.Background(), projectID, existing.ID, taskID)
			if err != nil {
				sessionErr = fmt.Errorf("highlight existing QA bug %s: %w", existing.Key, err)
				s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, sessionErr)
				return
			}
			linkedBugItems[index] = &linked
			if existing.Status == kanban.BacklogOpen {
				highlightedBugCount++
			} else {
				matchedPlanningBugCount++
			}
			continue
		}
		description := qaBugDescription(bug)
		title := qaBacklogTitle(bug.Title)
		requestID := projectartifact.NewRequestID()
		manifest, artifactErr := s.persistRequestArtifact(context.Background(), project, requestID, title, description, "qa", reference, nil)
		if artifactErr != nil {
			sessionErr = fmt.Errorf("persist QA bug request: %w", artifactErr)
			s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, sessionErr)
			return
		}
		var linked kanban.BacklogItem
		board, linked, err = s.boards.AddBacklogItem(context.Background(), projectID, project.Name, kanban.BacklogDraft{
			RequestID: requestID, ArtifactPath: manifest.Directory, Attachments: []kanban.RequestAttachment{},
			Type: kanban.BacklogBug, Source: "qa", SourceReference: reference, Title: title,
			Description: description, DeliveryTarget: plan.Request.DeliveryTarget, RequiresUI: plan.Request.RequiresUI,
			AcceptanceCriteria: []string{"The reported behavior is no longer reproducible.", "A focused regression test covers the failure.", "Directly affected behavior has no regression."}, Severity: bug.Severity,
		})
		if err != nil {
			_ = s.projectArtifacts.DeleteRequest(context.Background(), project.Path, requestID)
			sessionErr = fmt.Errorf("create QA bug backlog item: %w", err)
			s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, sessionErr)
			return
		}
		linkedBugItems[index] = &linked
		createdBugCount++
	}
	execution = qaExecution(result, linkedBugItems)
	reason := qaBugResultMessage(len(result.Bugs), createdBugCount, highlightedBugCount, matchedPlanningBugCount)
	board, err = s.boards.BlockTaskWithExecution(context.Background(), projectID, taskID, result.ThreadID, reason, execution)
	if err != nil {
		sessionErr = err
		s.blockQATask(traceID, projectID, taskID, result.ThreadID, started, err)
		return
	}
	s.boardEvents.publish(board)
	s.persistTaskExecutionArtifact(project, board, taskID)
	s.observability.Record(observability.Event{Level: observability.LevelWarn, Category: "ai", Name: "qa.bugs_found", Message: "QA found unresolved bugs", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "blocked", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"bugCount": len(result.Bugs), "newBugCount": createdBugCount, "highlightedBugCount": highlightedBugCount, "matchedPlanningBugCount": matchedPlanningBugCount, "threadId": result.ThreadID}})
	s.notify(notifications.Draft{Level: notifications.LevelWarning, Kind: "qa_bugs_found", Title: fmt.Sprintf("%s found %d bug(s)", task.Key, len(result.Bugs)), Message: reason, ProjectID: projectID, ProjectName: project.Name, EntityID: taskID, Route: "/work-items"})
}

func qaInputs(board kanban.Board, taskID string) (kanban.Task, kanban.Plan, []kanban.Document, bool) {
	var task kanban.Task
	for _, candidate := range board.Tasks {
		if candidate.ID == taskID {
			task = candidate
			break
		}
	}
	if task.ID == "" || task.Role != kanban.RoleQA {
		return kanban.Task{}, kanban.Plan{}, nil, false
	}
	var plan kanban.Plan
	for _, candidate := range board.Plans {
		if candidate.ID == task.PlanID {
			plan = candidate
			break
		}
	}
	if plan.ID == "" || plan.Status != kanban.PlanningApproved {
		return kanban.Task{}, kanban.Plan{}, nil, false
	}
	ids := make(map[string]struct{}, len(task.DocumentIDs))
	for _, id := range task.DocumentIDs {
		ids[id] = struct{}{}
	}
	documents := make([]kanban.Document, 0, len(ids))
	for _, document := range plan.Documents {
		if _, included := ids[document.ID]; included {
			documents = append(documents, document)
		}
	}
	return task, plan, documents, true
}

func qaExecution(result quality.Result, linkedBugItems []*kanban.BacklogItem) kanban.TaskExecution {
	findings := make([]kanban.TaskFinding, 0, len(result.Bugs))
	risks := make([]string, 0, len(result.Bugs))
	for index, bug := range result.Bugs {
		finding := kanban.TaskFinding{Severity: bug.Severity, Title: bug.Title, Description: bug.Description, Evidence: bug.Evidence, Steps: bug.Steps, Expected: bug.Expected, Actual: bug.Actual, AffectedFiles: bug.AffectedFiles}
		if index < len(linkedBugItems) && linkedBugItems[index] != nil {
			item := linkedBugItems[index]
			finding.LinkedBacklogID = item.ID
			finding.LinkedBacklogKey = item.Key
			finding.LinkedBacklogTitle = item.Title
			finding.LinkedBacklogStatus = item.Status
			finding.LinkedBacklogPlanID = item.PlanID
		}
		findings = append(findings, finding)
		risks = append(risks, bug.Title)
	}
	return kanban.TaskExecution{Verdict: string(result.Outcome), Summary: result.Summary, ChangedFiles: result.ChangedFiles, Verification: result.Verification, RemainingRisks: risks, Findings: findings}
}

func existingQABugs(board kanban.Board) []quality.ExistingBug {
	bugs := make([]quality.ExistingBug, 0)
	for _, item := range board.Backlog {
		if activeQABug(board, item) {
			bugs = append(bugs, quality.ExistingBug{BacklogID: item.ID, Key: item.Key, Title: item.Title})
		}
	}
	return bugs
}

func findExistingQABug(board kanban.Board, bug quality.Bug, reference string) (kanban.BacklogItem, bool) {
	normalizedTitle := normalizedQABugTitle(bug.Title)
	for _, item := range board.Backlog {
		if !activeQABug(board, item) {
			continue
		}
		if strings.TrimSpace(bug.ExistingBacklogID) != "" && item.ID == strings.TrimSpace(bug.ExistingBacklogID) {
			return item, true
		}
		if normalizedTitle != "" && normalizedQABugTitle(item.Title) == normalizedTitle {
			return item, true
		}
	}
	return kanban.BacklogItem{}, false
}

func activeQABug(board kanban.Board, item kanban.BacklogItem) bool {
	if item.Source != "qa" || item.Type != kanban.BacklogBug {
		return false
	}
	if item.Status == kanban.BacklogOpen {
		return true
	}
	return item.Status == kanban.BacklogPlanning && !backlogPlanFinished(board, item.PlanID)
}

func backlogPlanFinished(board kanban.Board, planID string) bool {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return false
	}
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

func qaBugReference(taskID, title string) string {
	digest := sha256.Sum256([]byte(normalizedQABugTitle(title)))
	return fmt.Sprintf("%s:bug:%x", strings.TrimSpace(taskID), digest[:6])
}

func qaBacklogTitle(value string) string {
	const prefix = "[QA] "
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Reported regression"
	}
	maxBytes := 120 - len(prefix)
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		value = strings.TrimSpace(value[:len(value)-size])
	}
	return prefix + value
}

func normalizedQABugTitle(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSpace(strings.TrimPrefix(value, "[qa]"))
	value = strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return ' '
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func qaBugResultMessage(total, created, highlighted, matchedPlanning int) string {
	if total > 0 && created == 0 && highlighted == 0 && matchedPlanning == total {
		return fmt.Sprintf("QA is waiting on %d unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.", matchedPlanning)
	}
	return fmt.Sprintf("QA found %d unresolved reproducible bug(s). Added %d new Backlog card(s), highlighted %d existing Backlog card(s), and matched %d bug(s) already in planning.", total, created, highlighted, matchedPlanning)
}

func qaBugDescription(bug quality.Bug) string {
	return fmt.Sprintf("%s\n\nEvidence: %s\n\nSteps to reproduce:\n- %s\n\nExpected: %s\nActual: %s\n\nAffected files: %s", strings.TrimSpace(bug.Description), strings.TrimSpace(bug.Evidence), strings.Join(bug.Steps, "\n- "), strings.TrimSpace(bug.Expected), strings.TrimSpace(bug.Actual), strings.Join(bug.AffectedFiles, ", "))
}

func (s *server) blockQATask(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	board, err := s.boards.BlockTask(context.Background(), projectID, taskID, threadID, qaErrorMessage(cause))
	if err == nil {
		s.boardEvents.publish(board)
	} else {
		cause = fmt.Errorf("%v; persist blocked state: %w", cause, err)
	}
	s.recordQAFailure(traceID, projectID, taskID, threadID, started, cause)
	s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "qa_failed", Title: "QA needs attention", Message: qaErrorMessage(cause), ProjectID: projectID, EntityID: taskID, Route: "/work-items"})
}

func (s *server) recordQAFailure(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	s.observability.Record(observability.Event{Level: observability.LevelError, Category: "ai", Name: "ai.job.failed", Message: "QA verification failed", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "qa", Stage: "verification", Outcome: "failed", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"error": cause.Error(), "threadId": threadID}})
}

func qaErrorMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1200 {
		message = message[:1200] + "..."
	}
	return "QA stopped: " + message
}

func qaPassMessage(result quality.Result) string {
	parts := []string{"QA passed this task."}
	if len(result.Verification) > 0 {
		parts = append(parts, "Verification recorded.")
	}
	return strings.Join(parts, " ")
}
