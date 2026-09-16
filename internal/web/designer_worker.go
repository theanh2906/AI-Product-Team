package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/design"
	"github.com/theanh2906/AI-Product-Team/internal/designartifact"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

const designerTaskTimeout = 90 * time.Minute

func (s *server) scheduleQueuedDesignerTasks() {
	s.scheduleQueuedAgentTasks("designer_queue_recovery", "designer-queue-recovery")
}

func (s *server) enqueueDesignerTask(projectID, taskID, traceID string) {
	s.scheduleQueuedAgentTasks("designer_task_queued", traceID)
}

func (s *server) runDesignerTask(projectID, taskID, traceID string) {
	defer s.scheduleQueuedAgentTasks("designer_task_finished", traceID)
	started := time.Now()
	provider := s.selectedAIProvider()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Designer task queued for selected AI runtime", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "designer", Stage: "design", Outcome: "queued", Attributes: s.aiRuntimeAttributes(provider)})

	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Designer acquired the sequential agent slot", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "designer", Stage: "design", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})

	board, err := s.boards.GetProjectBoard(context.Background(), projectID)
	if err != nil {
		s.recordDesignerFailure(traceID, projectID, taskID, "", started, err)
		return
	}
	task, plan, documents, ok := designerInputs(board, taskID)
	if !ok || task.Status != kanban.TaskQueued || task.Column != kanban.ColumnDesigner {
		return
	}
	task.ThreadID = taskThreadForRun(plan, task)
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		s.blockDesignerTask(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	board, err = s.boards.UpdateTaskStatus(context.Background(), projectID, taskID, kanban.TaskInProgress, "")
	if err != nil {
		s.recordDesignerFailure(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	s.boardEvents.publish(board)

	ctx, cancel := context.WithTimeout(context.Background(), designerTaskTimeout)
	defer cancel()
	ctx, usageCollector := agentusage.WithCollector(ctx)
	defer s.recordAIUsage(traceID, projectID, "task", taskID, "designer", usageCollector)
	reporter := s.taskSessions.start(projectID, taskID, provider, "designer")
	ctx = agentstream.WithReporter(ctx, reporter)
	var sessionErr error
	defer func() { s.taskSessions.finish(projectID, taskID, sessionErr) }()
	sourceGuard, guardErr := s.beginTaskSourceGuard(ctx, project, taskID, traceID, "designer")
	if guardErr != nil {
		sessionErr = guardErr
		s.blockDesignerTask(traceID, projectID, taskID, task.ThreadID, started, guardErr)
		return
	}
	result, runErr := s.designer.Design(ctx, design.Request{ProjectPath: project.executionPath(), AdditionalPaths: project.additionalExecutionPaths(), Plan: plan, Task: task, Documents: documents, DependencyReports: taskDependencyReports(board, task)})
	guardErr = s.finishTaskSourceGuard(context.Background(), sourceGuard, project, taskID, traceID, "designer")
	if guardErr != nil {
		sessionErr = combineAgentAndGuardError(runErr, guardErr)
		s.blockDesignerTask(traceID, projectID, taskID, result.ThreadID, started, sessionErr)
		return
	}
	if runErr != nil {
		sessionErr = runErr
		s.blockDesignerTask(traceID, projectID, taskID, result.ThreadID, started, runErr)
		return
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "artifact", Message: "Validating and rendering design mockups"})
	artifacts, artifactWarnings, artifactErr := s.designArtifacts.Process(ctx, project.Path, task.ID)
	if artifactErr != nil {
		sessionErr = artifactErr
		s.blockDesignerTask(traceID, projectID, taskID, result.ThreadID, started, artifactErr)
		return
	}
	if len(artifacts) > 0 {
		agentstream.Emit(ctx, agentstream.Event{Kind: "artifact", Message: fmt.Sprintf("Rendered %d design preview(s)", len(artifacts))})
		for _, artifact := range artifacts {
			result.ChangedFiles = appendUniqueString(result.ChangedFiles, artifact.RelativePath)
		}
	}
	result.RemainingRisks = append(result.RemainingRisks, artifactWarnings...)
	board, err = s.boards.CompleteTask(context.Background(), projectID, taskID, result.ThreadID, kanban.TaskExecution{
		Summary: result.Summary, ChangedFiles: result.ChangedFiles, Verification: result.Verification,
		RemainingRisks: result.RemainingRisks, Artifacts: toKanbanDesignArtifacts(artifacts),
	})
	if err != nil {
		sessionErr = err
		s.blockDesignerTask(traceID, projectID, taskID, result.ThreadID, started, err)
		return
	}
	s.boardEvents.publish(board)
	s.persistTaskExecutionArtifact(project, board, taskID)
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.completed", Message: "Designer handoff completed", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "designer", Stage: "design", Outcome: "success", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"changedFiles": result.ChangedFiles, "verification": result.Verification, "threadId": result.ThreadID}})
	notification := notifications.Draft{Level: notifications.LevelSuccess, Kind: "designer_completed", Title: task.Key + " design handoff completed", Message: designerCompletionMessage(result), ProjectID: projectID, ProjectName: project.Name, EntityID: taskID, Route: "/work-items"}
	if completedIndex := findBoardTask(board, taskID); completedIndex >= 0 && board.Tasks[completedIndex].Status == kanban.TaskDesignReview {
		notification.Kind = "designer_review_required"
		notification.Title = task.Key + " design handoff ready for PM approval"
		notification.Message = "Review the rendered mockups and approve the handoff to unlock dependent implementation work."
	}
	s.notify(notification)
}

func appendUniqueString(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func toKanbanDesignArtifacts(artifacts []designartifact.Artifact) []kanban.DesignArtifact {
	result := make([]kanban.DesignArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, kanban.DesignArtifact{
			ID: artifact.ID, Title: artifact.Title, Kind: artifact.Kind, RelativePath: artifact.RelativePath,
			MediaType: artifact.MediaType, Width: artifact.Width, Height: artifact.Height,
		})
	}
	return result
}

func designerInputs(board kanban.Board, taskID string) (kanban.Task, kanban.Plan, []kanban.Document, bool) {
	task, plan, documents, ok := roleTaskInputs(board, taskID, kanban.RoleDesigner)
	return task, plan, documents, ok
}

func (s *server) blockDesignerTask(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	reason := designerErrorMessage(cause)
	board, err := s.boards.BlockTask(context.Background(), projectID, taskID, threadID, reason)
	if err == nil {
		s.boardEvents.publish(board)
	} else {
		cause = fmt.Errorf("%v; persist blocked state: %w", cause, err)
	}
	s.recordDesignerFailure(traceID, projectID, taskID, threadID, started, cause)
	s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "designer_failed", Title: "Designer needs attention", Message: reason, ProjectID: projectID, EntityID: taskID, Route: "/work-items"})
}

func (s *server) recordDesignerFailure(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	s.observability.Record(observability.Event{Level: observability.LevelError, Category: "ai", Name: "ai.job.failed", Message: "Designer handoff failed", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "designer", Stage: "design", Outcome: "failed", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"error": cause.Error(), "threadId": threadID}})
}

func designerErrorMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	const limit = 1200
	if len(message) > limit {
		message = message[:limit] + "..."
	}
	return "Designer stopped: " + message
}

func designerCompletionMessage(result design.Result) string {
	parts := []string{"Designer completed the implementation handoff."}
	if changed := len(result.ChangedFiles); changed > 0 {
		parts = append(parts, fmt.Sprintf("Updated %d design artifact(s).", changed))
	}
	if len(result.Verification) > 0 {
		parts = append(parts, "Repository patterns reviewed.")
	}
	if len(result.RemainingRisks) > 0 {
		parts = append(parts, fmt.Sprintf("%d risk(s) noted.", len(result.RemainingRisks)))
	}
	return strings.Join(parts, " ")
}

func roleTaskInputs(board kanban.Board, taskID string, role kanban.AgentRole) (kanban.Task, kanban.Plan, []kanban.Document, bool) {
	var task kanban.Task
	for _, candidate := range board.Tasks {
		if candidate.ID == taskID {
			task = candidate
			break
		}
	}
	if task.ID == "" || task.Role != role {
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
	documentIDs := make(map[string]struct{}, len(task.DocumentIDs))
	for _, id := range task.DocumentIDs {
		documentIDs[id] = struct{}{}
	}
	documents := make([]kanban.Document, 0, len(documentIDs))
	for _, document := range plan.Documents {
		if _, included := documentIDs[document.ID]; included {
			documents = append(documents, document)
		}
	}
	return task, plan, documents, true
}

func taskThreadForRun(plan kanban.Plan, task kanban.Task) string {
	taskThreadID := strings.TrimSpace(task.ThreadID)
	if task.SequenceOrder <= 0 || plan.Sequence == nil {
		return taskThreadID
	}
	if threadID := strings.TrimSpace(plan.Sequence.RoleThreadIDs[task.Role]); threadID != "" {
		return threadID
	}
	return taskThreadID
}

func taskDependencyReports(board kanban.Board, task kanban.Task) []kanban.TaskExecution {
	reports := make([]kanban.TaskExecution, 0, len(task.DependencyIDs))
	for _, dependencyID := range task.DependencyIDs {
		index := findBoardTask(board, dependencyID)
		if index < 0 || board.Tasks[index].Execution == nil {
			continue
		}
		reports = append(reports, *board.Tasks[index].Execution)
	}
	return reports
}

func findBoardTask(board kanban.Board, taskID string) int {
	for index := range board.Tasks {
		if board.Tasks[index].ID == taskID {
			return index
		}
	}
	return -1
}
