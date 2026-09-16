package web

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/development"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

const developerTaskTimeout = 90 * time.Minute
const maxBuildRepairAttempts = 2

func (s *server) scheduleQueuedDeveloperTasks() {
	s.scheduleQueuedAgentTasks("developer_queue_recovery", "developer-queue-recovery")
}

func (s *server) enqueueDeveloperTask(projectID, taskID, traceID string) {
	s.scheduleQueuedAgentTasks("developer_task_queued", traceID)
}

func (s *server) runDeveloperTask(projectID, taskID, traceID string) {
	defer s.scheduleQueuedAgentTasks("developer_task_finished", traceID)
	started := time.Now()
	provider := s.selectedAIProvider()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Developer task queued for selected AI runtime", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "developer", Stage: "implementation", Outcome: "queued", Attributes: s.aiRuntimeAttributes(provider)})

	// This is the same gate used by Team Lead, Bug Scanner, and Feature Radar.
	// Waiting here is backend-owned and is independent from the HTTP request.
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Developer acquired the sequential agent slot", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "developer", Stage: "implementation", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})

	board, err := s.boards.GetProjectBoard(context.Background(), projectID)
	if err != nil {
		s.recordDeveloperFailure(traceID, projectID, taskID, "", started, err)
		return
	}
	task, plan, documents, ok := developerInputs(board, taskID)
	if !ok || task.Status != kanban.TaskQueued || task.Column != kanban.ColumnDeveloper {
		return
	}
	task.ThreadID = taskThreadForRun(plan, task)
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		s.blockDeveloperTask(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	board, err = s.boards.UpdateTaskStatus(context.Background(), projectID, taskID, kanban.TaskInProgress, "")
	if err != nil {
		s.recordDeveloperFailure(traceID, projectID, taskID, task.ThreadID, started, err)
		return
	}
	s.boardEvents.publish(board)

	ctx, cancel := context.WithTimeout(context.Background(), developerTaskTimeout)
	defer cancel()
	ctx, usageCollector := agentusage.WithCollector(ctx)
	defer s.recordAIUsage(traceID, projectID, "task", taskID, "developer", usageCollector)
	reporter := s.taskSessions.start(projectID, taskID, provider, "developer")
	ctx = agentstream.WithReporter(ctx, reporter)
	var sessionErr error
	defer func() { s.taskSessions.finish(projectID, taskID, sessionErr) }()
	sourceGuard, guardErr := s.beginTaskSourceGuard(ctx, project, taskID, traceID, "developer")
	if guardErr != nil {
		sessionErr = guardErr
		s.blockDeveloperTask(traceID, projectID, taskID, task.ThreadID, started, guardErr)
		return
	}
	result, runErr := s.developer.Implement(ctx, development.Request{ProjectPath: project.executionPath(), AdditionalPaths: project.additionalExecutionPaths(), Plan: plan, Task: task, Documents: documents, DependencyReports: taskDependencyReports(board, task)})
	guardErr = s.finishTaskSourceGuard(context.Background(), sourceGuard, project, taskID, traceID, "developer")
	if guardErr != nil {
		sessionErr = combineAgentAndGuardError(runErr, guardErr)
		s.blockDeveloperTaskWithResult(traceID, projectID, taskID, result, started, sessionErr, nil)
		return
	}
	if runErr != nil {
		sessionErr = runErr
		s.blockDeveloperTask(traceID, projectID, taskID, result.ThreadID, started, runErr)
		return
	}
	if !developerTaskNeedsBuildVerification(board, task) {
		if err := s.completeDeveloperTask(project, task, traceID, started, result, nil); err != nil {
			sessionErr = err
			s.blockDeveloperTask(traceID, projectID, taskID, result.ThreadID, started, err)
		}
		return
	}
	buildProfile, profileErr := s.buildProfiles.Get(ctx, projectID)
	if profileErr != nil {
		agentstream.Emit(ctx, agentstream.Event{Kind: "build", Message: "Detecting project build actions"})
		buildProfile, profileErr = s.buildDetector.Detect(ctx, project.ID, project.Name, project.Path)
		if profileErr == nil {
			profileErr = s.buildProfiles.Upsert(ctx, buildProfile)
		}
	}
	if profileErr != nil {
		sessionErr = profileErr
		s.blockDeveloperTaskWithResult(traceID, projectID, taskID, result, started, fmt.Errorf("build verification setup failed: %w", profileErr), nil)
		return
	}
	action, ok := selectedBuildAction(buildProfile)
	if !ok {
		err := fmt.Errorf("no build action is selected; open Settings and choose the Work Board build command")
		sessionErr = err
		s.blockDeveloperTaskWithResult(traceID, projectID, taskID, result, started, err, nil)
		return
	}
	verification, buildErr := s.verifyBuildWithRepair(ctx, project, board, task, documents, traceID, started, action, &result)
	if buildErr != nil {
		sessionErr = buildErr
		s.blockDeveloperTaskWithResult(traceID, projectID, taskID, result, started, buildErr, verification)
		return
	}
	if err = s.completeDeveloperTask(project, task, traceID, started, result, verification); err != nil {
		sessionErr = err
		s.blockDeveloperTask(traceID, projectID, taskID, result.ThreadID, started, err)
	}
}

func (s *server) verifyBuildWithRepair(ctx context.Context, project project, board kanban.Board, task kanban.Task, documents []kanban.Document, traceID string, started time.Time, action buildverify.Action, result *development.Result) (*kanban.BuildVerification, error) {
	for attempt := 0; attempt <= maxBuildRepairAttempts; attempt++ {
		verification, buildRun, buildErr := s.runTaskBuildVerification(ctx, project, task.ID, traceID, action)
		if buildErr == nil {
			agentstream.Emit(ctx, agentstream.Event{Kind: "build", Message: "Build verification passed", Detail: buildRun.Reason})
			s.observability.Record(observability.Event{Category: "build", Name: "build.execution.passed", Message: buildRun.Reason, CorrelationID: traceID, ProjectID: project.ID, EntityType: "task", EntityID: task.ID, Agent: "developer", Stage: "verification", Outcome: "success", DurationMS: buildRun.DurationMS, Attributes: map[string]any{"command": buildRun.Command, "exitCode": buildRun.ExitCode, "logPath": buildRun.LogPath, "repairAttempts": attempt}})
			return verification, nil
		}
		s.observability.Record(observability.Event{Level: observability.LevelError, Category: "build", Name: "build.execution.failed", Message: buildRun.Reason, CorrelationID: traceID, ProjectID: project.ID, EntityType: "task", EntityID: task.ID, Agent: "developer", Stage: "verification", Outcome: "failed", DurationMS: buildRun.DurationMS, Attributes: map[string]any{"command": buildRun.Command, "exitCode": buildRun.ExitCode, "logPath": buildRun.LogPath, "repairAttempt": attempt}})
		if attempt >= maxBuildRepairAttempts {
			return verification, fmt.Errorf("build verification failed after %d repair attempt(s): %s", maxBuildRepairAttempts, buildRun.Reason)
		}
		repairAttempt := attempt + 1
		agentstream.Emit(ctx, agentstream.Event{Kind: "build", Message: "Build verification failed; starting Developer repair pass", Detail: buildRun.Reason})
		repairTask := task
		repairTask.ThreadID = result.ThreadID
		sourceGuard, guardErr := s.beginTaskSourceGuard(ctx, project, task.ID, traceID, "developer")
		if guardErr != nil {
			return verification, guardErr
		}
		repairResult, repairErr := s.developer.RepairBuild(ctx, development.BuildRepairRequest{
			Request: development.Request{
				ProjectPath: project.executionPath(), AdditionalPaths: project.additionalExecutionPaths(), Plan: taskPlan(board, task.PlanID), Task: repairTask,
				Documents: documents, DependencyReports: taskDependencyReports(board, task),
			},
			PreviousResult: *result,
			Verification:   *verification,
			BuildLog:       buildLogExcerpt(buildRun.LogPath),
			Attempt:        repairAttempt,
			MaxAttempts:    maxBuildRepairAttempts,
		})
		guardErr = s.finishTaskSourceGuard(context.Background(), sourceGuard, project, task.ID, traceID, "developer")
		if guardErr != nil {
			return verification, combineAgentAndGuardError(repairErr, guardErr)
		}
		if repairErr != nil {
			if strings.TrimSpace(repairResult.ThreadID) != "" {
				result.ThreadID = strings.TrimSpace(repairResult.ThreadID)
			}
			return verification, fmt.Errorf("build repair attempt %d failed: %w", repairAttempt, repairErr)
		}
		*result = mergeDeveloperResults(*result, repairResult)
		agentstream.Emit(ctx, agentstream.Event{Kind: "build", Message: "Developer repair pass completed", Detail: repairResult.Summary})
	}
	return nil, fmt.Errorf("build verification did not complete")
}

func (s *server) runTaskBuildVerification(ctx context.Context, project project, taskID, traceID string, action buildverify.Action) (*kanban.BuildVerification, buildverify.Run, error) {
	verificationStarted := time.Now().UTC()
	runningVerification := &kanban.BuildVerification{ActionID: action.ID, ActionLabel: action.Label, Command: buildCommand(action), Status: "running", ExitCode: -1, StartedAt: verificationStarted}
	board, err := s.boards.StartBuildVerification(context.Background(), project.ID, taskID, *runningVerification)
	if err != nil {
		return nil, buildverify.Run{}, err
	}
	s.boardEvents.publish(board)
	agentstream.Emit(ctx, agentstream.Event{Kind: "build", Message: "Build verification started", Detail: buildCommand(action)})
	s.observability.Record(observability.Event{Category: "build", Name: "build.execution.started", Message: "Post-implementation build verification started", CorrelationID: traceID, ProjectID: project.ID, EntityType: "task", EntityID: taskID, Agent: "developer", Stage: "verification", Outcome: "running", Attributes: map[string]any{"actionId": action.ID, "command": buildCommand(action)}})
	buildRun, buildErr := s.buildRunner.Run(ctx, project.Path, project.ID, taskID, action, func(level, line string) {
		agentstream.Emit(ctx, agentstream.Event{Level: level, Kind: "command", Message: "Build output", Detail: line})
	})
	verification := buildVerificationResult(buildRun)
	return &verification, buildRun, buildErr
}

func sequenceHasLaterDeveloper(board kanban.Board, task kanban.Task) bool {
	for _, candidate := range board.Tasks {
		if candidate.PlanID == task.PlanID && candidate.SequenceOrder > task.SequenceOrder && candidate.Role == kanban.RoleDeveloper {
			return true
		}
	}
	return false
}

func developerTaskNeedsBuildVerification(board kanban.Board, task kanban.Task) bool {
	return task.SequenceOrder <= 0 || !sequenceHasLaterDeveloper(board, task)
}

func (s *server) completeDeveloperTask(project project, task kanban.Task, traceID string, started time.Time, result development.Result, verification *kanban.BuildVerification) error {
	board, err := s.boards.CompleteTask(context.Background(), project.ID, task.ID, result.ThreadID, kanban.TaskExecution{
		Summary: result.Summary, ChangedFiles: result.ChangedFiles, Verification: result.Verification,
		RemainingRisks: result.RemainingRisks, BuildVerification: verification,
	})
	if err != nil {
		return err
	}
	s.boardEvents.publish(board)
	s.persistTaskExecutionArtifact(project, board, task.ID)
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.completed", Message: "Developer implementation completed", CorrelationID: traceID, ProjectID: project.ID, EntityType: "task", EntityID: task.ID, Agent: "developer", Stage: "implementation", Outcome: "success", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"changedFiles": result.ChangedFiles, "verification": result.Verification, "threadId": result.ThreadID, "buildVerified": verification != nil}})
	message := developerCompletionMessage(result, verification != nil)
	s.notify(notifications.Draft{Level: notifications.LevelSuccess, Kind: "developer_completed", Title: task.Key + " implementation completed", Message: message, ProjectID: project.ID, ProjectName: project.Name, EntityID: task.ID, Route: "/work-items"})
	return nil
}

func selectedBuildAction(profile buildverify.Profile) (buildverify.Action, bool) {
	for _, action := range profile.Actions {
		if action.ID == profile.SelectedActionID {
			return action, true
		}
	}
	return buildverify.Action{}, false
}

func buildCommand(action buildverify.Action) string {
	return strings.Join(append([]string{action.Executable}, action.Arguments...), " ")
}

func buildVerificationResult(run buildverify.Run) kanban.BuildVerification {
	return kanban.BuildVerification{RunID: run.ID, ActionID: run.ActionID, ActionLabel: run.ActionLabel, Command: run.Command, Status: run.Status, ExitCode: run.ExitCode, Reason: run.Reason, LogPath: run.LogPath, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, DurationMS: run.DurationMS}
}

func taskPlan(board kanban.Board, planID string) kanban.Plan {
	for _, plan := range board.Plans {
		if plan.ID == planID {
			return plan
		}
	}
	return kanban.Plan{}
}

func mergeDeveloperResults(base, repair development.Result) development.Result {
	if strings.TrimSpace(repair.ThreadID) != "" {
		base.ThreadID = strings.TrimSpace(repair.ThreadID)
	}
	if strings.TrimSpace(repair.Summary) != "" {
		base.Summary = strings.TrimSpace(base.Summary + "\n\nBuild repair: " + repair.Summary)
	}
	base.ChangedFiles = appendUniqueStrings(base.ChangedFiles, repair.ChangedFiles...)
	base.Verification = appendUniqueStrings(base.Verification, repair.Verification...)
	base.RemainingRisks = repair.RemainingRisks
	return base
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	result := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func buildLogExcerpt(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "Could not read build log excerpt: " + err.Error()
	}
	const maxBuildLogExcerptBytes = 12000
	if len(data) > maxBuildLogExcerptBytes {
		data = data[len(data)-maxBuildLogExcerptBytes:]
		return "[tail of build log]\n" + string(data)
	}
	return string(data)
}

func (s *server) blockDeveloperTaskWithResult(traceID, projectID, taskID string, result development.Result, started time.Time, cause error, verification *kanban.BuildVerification) {
	reason := developerErrorMessage(cause)
	board, err := s.boards.BlockTaskWithExecution(context.Background(), projectID, taskID, result.ThreadID, reason, kanban.TaskExecution{Summary: result.Summary, ChangedFiles: result.ChangedFiles, Verification: result.Verification, RemainingRisks: result.RemainingRisks, BuildVerification: verification})
	if err == nil {
		s.boardEvents.publish(board)
		if project, projectErr := s.projectService.findProject(projectID); projectErr == nil {
			s.persistTaskExecutionArtifact(project, board, taskID)
		}
	} else {
		cause = fmt.Errorf("%v; persist blocked state: %w", cause, err)
	}
	s.recordDeveloperFailure(traceID, projectID, taskID, result.ThreadID, started, cause)
	s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "build_verification_failed", Title: "Build verification needs attention", Message: reason, ProjectID: projectID, EntityID: taskID, Route: "/work-items"})
}

func developerInputs(board kanban.Board, taskID string) (kanban.Task, kanban.Plan, []kanban.Document, bool) {
	var task kanban.Task
	for _, candidate := range board.Tasks {
		if candidate.ID == taskID {
			task = candidate
			break
		}
	}
	if task.ID == "" || task.Role != kanban.RoleDeveloper {
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

func (s *server) blockDeveloperTask(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	reason := developerErrorMessage(cause)
	board, err := s.boards.BlockTask(context.Background(), projectID, taskID, threadID, reason)
	if err == nil {
		s.boardEvents.publish(board)
	} else {
		cause = fmt.Errorf("%v; persist blocked state: %w", cause, err)
	}
	s.recordDeveloperFailure(traceID, projectID, taskID, threadID, started, cause)
	s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "developer_failed", Title: "Developer needs attention", Message: reason, ProjectID: projectID, EntityID: taskID, Route: "/work-items"})
}

func (s *server) recordDeveloperFailure(traceID, projectID, taskID, threadID string, started time.Time, cause error) {
	s.observability.Record(observability.Event{Level: observability.LevelError, Category: "ai", Name: "ai.job.failed", Message: "Developer implementation failed", CorrelationID: traceID, ProjectID: projectID, EntityType: "task", EntityID: taskID, Agent: "developer", Stage: "implementation", Outcome: "failed", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"error": cause.Error(), "threadId": threadID}})
}

func developerErrorMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	const limit = 1200
	if len(message) > limit {
		message = message[:limit] + "..."
	}
	return "Developer stopped: " + message
}

func developerCompletionMessage(result development.Result, buildVerified bool) string {
	parts := []string{"Developer completed this task."}
	if changed := len(result.ChangedFiles); changed > 0 {
		parts = append(parts, fmt.Sprintf("Changed %d file(s).", changed))
	}
	if len(result.Verification) > 0 {
		parts = append(parts, "Verification recorded.")
	}
	if buildVerified {
		parts = append(parts, "Build verification passed.")
	} else {
		parts = append(parts, "Sequence build verification is deferred until the final Developer task.")
	}
	if len(result.RemainingRisks) > 0 {
		parts = append(parts, fmt.Sprintf("%d risk(s) noted.", len(result.RemainingRisks)))
	}
	return strings.Join(parts, " ")
}
