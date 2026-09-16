package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/planning"
	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
)

type createPlanRequest struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	WorkType           string   `json:"workType"`
	DeliveryTarget     string   `json:"deliveryTarget"`
	RequiresUI         bool     `json:"requiresUI"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
}

type createBacklogRequest struct {
	Type               kanban.BacklogItemType   `json:"type"`
	Source             string                   `json:"source"`
	SourceReference    string                   `json:"sourceReference"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	WorkType           string                   `json:"workType"`
	DeliveryTarget     string                   `json:"deliveryTarget"`
	RequiresUI         bool                     `json:"requiresUI"`
	AcceptanceCriteria []string                 `json:"acceptanceCriteria"`
	Feasibility        int                      `json:"feasibility"`
	Severity           string                   `json:"severity"`
	Intake             *kanban.IntakeSubmission `json:"intake"`
}

type reviewPlanRequest struct {
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	Reviewer    string `json:"reviewer"`
	RunSequence bool   `json:"runSequence"`
}

type moveTaskRequest struct {
	TargetColumn kanban.Column `json:"targetColumn"`
}

type updateTaskStatusRequest struct {
	Status        kanban.TaskStatus `json:"status"`
	BlockedReason string            `json:"blockedReason"`
}

type designFeedbackRequest struct {
	Feedback string `json:"feedback"`
	Reviewer string `json:"reviewer"`
}

func (s *server) getBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := s.boards.ListBoards(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, boards)
}

func (s *server) getProjectBoard(w http.ResponseWriter, r *http.Request) {
	board, err := s.boards.GetProjectBoard(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, kanban.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "This project does not have a board yet."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *server) createBoardPlan(w http.ResponseWriter, r *http.Request) {
	var request createPlanRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, plan, err := s.boards.CreatePlan(r.Context(), project.ID, project.Name, kanban.WorkRequest{
		Title: request.Title, Description: request.Description, WorkType: request.WorkType,
		DeliveryTarget: request.DeliveryTarget, RequiresUI: request.RequiresUI, AcceptanceCriteria: request.AcceptanceCriteria,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "planning", Name: "planning.started", Message: "Team Lead planning started", CorrelationID: correlationID(r.Context()), ProjectID: project.ID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "running"})
	go s.runTeamLeadPlanning(board.ID, project, plan, correlationID(r.Context()))
	writeJSON(w, http.StatusAccepted, board)
}

func (s *server) createBoardBacklogItem(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeBacklogAPIError(w, r, http.StatusUnprocessableEntity, err)
		return
	}
	request, uploads, ok := decodeBacklogRequest(w, r)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !ok {
		return
	}
	if request.Intake != nil {
		normalized, normalizeErr := kanban.NormalizeIntake(*request.Intake)
		if normalizeErr != nil {
			writeBacklogAPIError(w, r, http.StatusUnprocessableEntity, normalizeErr)
			return
		}
		if request.Type == "" {
			request.Type = kanban.BacklogItemType(normalized.WorkType)
		}
		request.DeliveryTarget = normalized.DeliveryTarget
		request.RequiresUI = normalized.RequiresUI
		request.AcceptanceCriteria = normalized.AcceptanceCriteria
	}
	if request.Type == "" && strings.TrimSpace(request.WorkType) != "" {
		request.Type = kanban.BacklogItemType(strings.TrimSpace(request.WorkType))
	}
	if strings.EqualFold(strings.TrimSpace(string(request.Type)), "task") {
		request.Type = kanban.BacklogTodo
	}
	requestID := projectartifact.NewRequestID()
	manifest, err := s.persistRequestArtifact(r.Context(), project, requestID, request.Title, request.Description, request.Source, request.SourceReference, uploads)
	if err != nil {
		writeBacklogAPIError(w, r, http.StatusUnprocessableEntity, err)
		return
	}
	board, item, err := s.boards.AddBacklogItem(r.Context(), project.ID, project.Name, kanban.BacklogDraft{
		RequestID: requestID, ArtifactPath: manifest.Directory, Attachments: toKanbanAttachments(manifest.Attachments),
		Type: request.Type, Source: request.Source, SourceReference: request.SourceReference,
		Title: request.Title, Description: request.Description, DeliveryTarget: request.DeliveryTarget,
		RequiresUI: request.RequiresUI, AcceptanceCriteria: request.AcceptanceCriteria,
		Feasibility: request.Feasibility, Severity: request.Severity,
		Intake: request.Intake,
	})
	if err != nil {
		_ = s.projectArtifacts.DeleteRequest(context.Background(), project.Path, requestID)
		writeBacklogAPIError(w, r, http.StatusUnprocessableEntity, err)
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "backlog", Name: "backlog." + string(item.Type) + ".added", Message: "Backlog item added", CorrelationID: correlationID(r.Context()), ProjectID: project.ID, EntityType: "backlog", EntityID: item.ID, Stage: "backlog", Outcome: "success", Attributes: map[string]any{"source": item.Source, "title": item.Title}})
	if strings.HasSuffix(r.URL.Path, "/tickets") || isRemoteAutomationBacklogPath(r.URL.Path) {
		writeJSON(w, http.StatusCreated, remoteTicketResponse{
			APIVersion: remoteAPIVersion,
			Project:    remoteProjectReference{ID: project.ID, Name: project.Name},
			Ticket:     buildRemoteTicket(board, item),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"board": board, "item": item})
}

func (s *server) planBoardBacklogItem(w http.ResponseWriter, r *http.Request) {
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, plan, err := s.boards.StartBacklogPlanning(r.Context(), project.ID, r.PathValue("itemID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "planning", Name: "planning.started", Message: "Backlog item moved to Team Lead", CorrelationID: correlationID(r.Context()), ProjectID: project.ID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "running", Attributes: map[string]any{"backlogItemId": r.PathValue("itemID")}})
	go s.runTeamLeadPlanning(board.ID, project, plan, correlationID(r.Context()))
	writeJSON(w, http.StatusAccepted, board)
}

func (s *server) removeBoardBacklogItem(w http.ResponseWriter, r *http.Request) {
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, err := s.boards.RemoveBacklogItem(r.Context(), project.ID, r.PathValue("itemID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{
		Category:      "backlog",
		Name:          "backlog.removed",
		Message:       "Backlog item removed",
		CorrelationID: correlationID(r.Context()),
		ProjectID:     project.ID,
		EntityType:    "backlog",
		EntityID:      r.PathValue("itemID"),
		Stage:         "backlog",
		Outcome:       "success",
	})
	writeJSON(w, http.StatusOK, board)
}

func (s *server) reviewBoardPlan(w http.ResponseWriter, r *http.Request) {
	var request reviewPlanRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	projectID := r.PathValue("projectID")
	var board kanban.Board
	var err error
	if request.RunSequence && strings.EqualFold(request.Decision, "approve") {
		board, err = s.boards.ApprovePlanSequence(r.Context(), projectID, r.PathValue("planID"), request.Reviewer)
	} else {
		board, err = s.boards.ReviewPlan(r.Context(), projectID, r.PathValue("planID"), request.Decision, request.Reason, request.Reviewer)
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "planning", Name: "planning.reviewed", Message: "PM reviewed Team Lead plan", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "plan", EntityID: r.PathValue("planID"), Stage: "approval", Outcome: strings.ToLower(request.Decision), Attributes: map[string]any{"reviewer": request.Reviewer, "reason": request.Reason, "runSequence": request.RunSequence}})
	if request.RunSequence && strings.EqualFold(request.Decision, "approve") {
		s.scheduleQueuedAgentTasks("plan_sequence_scheduled", correlationID(r.Context()))
	}
	if strings.EqualFold(request.Decision, "deny") {
		project, projectErr := s.projectService.findProject(projectID)
		if projectErr != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": projectErr.Error()})
			return
		}
		var plan kanban.Plan
		board, plan, err = s.boards.MarkPlanAnalyzing(r.Context(), projectID, r.PathValue("planID"))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		s.boardEvents.publish(board)
		go s.runTeamLeadPlanning(board.ID, project, plan, correlationID(r.Context()))
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *server) retryBoardPlan(w http.ResponseWriter, r *http.Request) {
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	projectID := r.PathValue("projectID")
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, plan, err := s.boards.MarkPlanAnalyzing(r.Context(), projectID, r.PathValue("planID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "planning", Name: "planning.retried", Message: "Team Lead planning retried", CorrelationID: correlationID(r.Context()), ProjectID: projectID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "running"})
	go s.runTeamLeadPlanning(board.ID, project, plan, correlationID(r.Context()))
	writeJSON(w, http.StatusAccepted, board)
}

func (s *server) safeStopBoardQueue(w http.ResponseWriter, r *http.Request) {
	board, err := s.boards.RequestQueueStop(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	outcome := "paused"
	if board.QueueControl != nil {
		outcome = string(board.QueueControl.Status)
	}
	s.observability.Record(observability.Event{
		Category: "task", Name: "task.queue.safe_stop_requested", Message: "Project queue safe stop requested",
		CorrelationID: correlationID(r.Context()), ProjectID: board.ProjectID, EntityType: "board", EntityID: board.ID,
		Stage: "queue", Outcome: outcome,
	})
	writeJSON(w, http.StatusOK, board)
}

func (s *server) continueBoardQueue(w http.ResponseWriter, r *http.Request) {
	board, err := s.boards.ContinueQueue(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{
		Category: "task", Name: "task.queue.continued", Message: "Project queue continued manually",
		CorrelationID: correlationID(r.Context()), ProjectID: board.ProjectID, EntityType: "board", EntityID: board.ID,
		Stage: "queue", Outcome: "running",
	})
	s.scheduleQueuedAgentTasks("queue_continued", correlationID(r.Context()))
	writeJSON(w, http.StatusOK, board)
}

func (s *server) moveBoardTask(w http.ResponseWriter, r *http.Request) {
	var request moveTaskRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	board, err := s.boards.MoveTask(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"), request.TargetColumn)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "task", Name: "task.moved", Message: "Task moved between queues", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "task", EntityID: r.PathValue("taskID"), Stage: string(request.TargetColumn), Outcome: "success"})
	if isAgentQueueColumn(request.TargetColumn) {
		s.scheduleQueuedAgentTasks("task_moved_to_queue", correlationID(r.Context()))
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *server) restartBoardTask(w http.ResponseWriter, r *http.Request) {
	board, err := s.boards.RestartTask(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "task", Name: "task.restarted", Message: "Blocked task restarted with a new AI session", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "task", EntityID: r.PathValue("taskID"), Stage: "queue", Outcome: "success"})
	s.scheduleQueuedAgentTasks("task_restarted", correlationID(r.Context()))
	writeJSON(w, http.StatusOK, board)
}

func (s *server) approveDesignBoardTask(w http.ResponseWriter, r *http.Request) {
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, err := s.boards.ApproveDesignHandoff(r.Context(), project.ID, r.PathValue("taskID"), "PM")
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.persistTaskExecutionArtifact(project, board, r.PathValue("taskID"))
	s.observability.Record(observability.Event{Category: "task", Name: "task.design_approved", Message: "PM approved Designer handoff", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "task", EntityID: r.PathValue("taskID"), Stage: "design_review", Outcome: "approved"})
	s.scheduleQueuedAgentTasks("design_handoff_approved", correlationID(r.Context()))
	writeJSON(w, http.StatusOK, board)
}

func (s *server) submitDesignFeedbackBoardTask(w http.ResponseWriter, r *http.Request) {
	var request designFeedbackRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	board, err := s.boards.SubmitDesignFeedback(r.Context(), project.ID, r.PathValue("taskID"), request.Feedback, request.Reviewer)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.persistTaskExecutionArtifact(project, board, r.PathValue("taskID"))
	s.observability.Record(observability.Event{
		Category:      "task",
		Name:          "task.design_feedback_requested",
		Message:       "PM requested a Designer revision",
		CorrelationID: correlationID(r.Context()),
		ProjectID:     project.ID,
		EntityType:    "task",
		EntityID:      r.PathValue("taskID"),
		Stage:         "design_review",
		Outcome:       "requeued",
		Attributes: map[string]any{
			"reviewer":       request.Reviewer,
			"feedbackLength": len(strings.TrimSpace(request.Feedback)),
		},
	})
	s.scheduleQueuedAgentTasks("design_feedback_requested", correlationID(r.Context()))
	writeJSON(w, http.StatusOK, board)
}

func (s *server) ignoreBoardTask(w http.ResponseWriter, r *http.Request) {
	board, err := s.boards.IgnoreBlockedTask(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	s.observability.Record(observability.Event{Category: "task", Name: "task.ignored", Message: "Blocked task ignored by PM", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "task", EntityID: r.PathValue("taskID"), Stage: "ignore", Outcome: "success"})
	s.scheduleQueuedAgentTasks("task_ignored", correlationID(r.Context()))
	writeJSON(w, http.StatusOK, board)
}

func (s *server) updateBoardTaskStatus(w http.ResponseWriter, r *http.Request) {
	var request updateTaskStatusRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	board, err := s.boards.UpdateTaskStatus(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"), request.Status, request.BlockedReason)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	eventName := "task.status_changed"
	if request.Status == kanban.TaskCompleted {
		eventName = "task.completed"
	}
	s.observability.Record(observability.Event{Category: "task", Name: eventName, Message: "Task status updated", CorrelationID: correlationID(r.Context()), ProjectID: r.PathValue("projectID"), EntityType: "task", EntityID: r.PathValue("taskID"), Stage: string(request.Status), Outcome: "success", Attributes: map[string]any{"blockedReason": request.BlockedReason}})
	if request.Status == kanban.TaskCompleted || request.Status == kanban.TaskBlocked {
		s.scheduleQueuedAgentTasks("task_status_released_active_slot", correlationID(r.Context()))
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *server) streamProjectBoard(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming is not supported by this HTTP server."})
		return
	}
	projectID := r.PathValue("projectID")
	updates, unsubscribe := s.boardEvents.subscribe(projectID)
	defer unsubscribe()
	board, err := s.boards.GetProjectBoard(r.Context(), projectID)
	if errors.Is(err, kanban.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "This project does not have a board yet."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeSSE(w, "board", board)
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case board := <-updates:
			writeSSE(w, "board", board)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *server) runTeamLeadPlanning(boardID string, project project, plan kanban.Plan, traceID string) {
	started := time.Now()
	provider := s.selectedAIProvider()
	reporter := s.taskSessions.startPlan(project.ID, plan.ID, provider)
	reporter.Report(agentstream.Event{Kind: "queue", Message: "Waiting for the sequential agent slot"})
	attributes := s.aiRuntimeAttributes(provider)
	attributes["planRevision"] = plan.Revision
	attributes["title"] = plan.Request.Title
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Team Lead planning queued for selected AI runtime", CorrelationID: traceID, ProjectID: project.ID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "queued", Attributes: attributes})
	// One shared gate keeps every agent operation sequential. Future Designer,
	// Developer, and QA workers must use the same gate.
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	reporter.Report(agentstream.Event{Kind: "lifecycle", Message: "Team Lead acquired the agent slot"})
	s.observability.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Team Lead acquired the sequential agent slot", CorrelationID: traceID, ProjectID: project.ID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "running", DurationMS: time.Since(started).Milliseconds(), Attributes: map[string]any{"cwd": project.Path, "planRevision": plan.Revision}})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	ctx, usageCollector := agentusage.WithCollector(ctx)
	defer s.recordAIUsage(traceID, project.ID, "plan", plan.ID, "team-lead", usageCollector)
	ctx = agentstream.WithReporter(ctx, reporter)
	result, runErr := s.teamLead.Generate(ctx, planning.Request{ProjectPath: project.executionPath(), AdditionalPaths: project.additionalExecutionPaths(), Plan: plan})
	var board kanban.Board
	var persistErr error
	if runErr != nil {
		board, persistErr = s.boards.FailPlanning(context.Background(), boardID, plan.ID, runErr)
	} else if result.Preflight != nil && result.Preflight.Status == "already_implemented" {
		board, persistErr = s.boards.CompletePlanAsAlreadyImplemented(context.Background(), boardID, plan.ID, result.ThreadID, *result.Preflight)
	} else if result.Preflight != nil && result.Preflight.Status == "not_feasible" {
		board, persistErr = s.boards.CompletePlanAsNotFeasible(context.Background(), boardID, plan.ID, result.ThreadID, *result.Preflight)
	} else {
		board, persistErr = s.boards.ApplyDraft(context.Background(), boardID, plan.ID, result.ThreadID, result.Draft)
	}
	if persistErr == nil {
		s.boardEvents.publish(board)
	}
	finalErr := runErr
	if persistErr != nil {
		finalErr = persistErr
	}
	s.taskSessions.finish(project.ID, plan.ID, finalErr)
	event := observability.Event{Category: "ai", Name: "ai.job.completed", Message: "Team Lead planning completed", CorrelationID: traceID, ProjectID: project.ID, EntityType: "plan", EntityID: plan.ID, Agent: "team-lead", Stage: "planning", Outcome: "success", DurationMS: time.Since(started).Milliseconds()}
	if runErr == nil && persistErr == nil && result.Preflight != nil && result.Preflight.Status == "already_implemented" {
		event.Message = "Team Lead verified work was already implemented"
		event.Outcome = "skipped"
	} else if runErr == nil && persistErr == nil && result.Preflight != nil && result.Preflight.Status == "not_feasible" {
		event.Message = "Team Lead found the request is not feasible"
		event.Outcome = "not_feasible"
	}
	if finalErr != nil {
		event.Level = observability.LevelError
		event.Name = "ai.job.failed"
		event.Message = "Team Lead planning failed"
		event.Outcome = "failed"
		event.Attributes = map[string]any{"error": finalErr.Error()}
	}
	s.observability.Record(event)
	if finalErr != nil {
		s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "planning_failed", Title: "Team Lead planning needs attention", Message: finalErr.Error(), ProjectID: project.ID, EntityID: plan.ID, Route: "/work-items"})
	} else if result.Preflight != nil && result.Preflight.Status == "already_implemented" {
		s.notify(notifications.Draft{Level: notifications.LevelSuccess, Kind: "planning_skipped_already_done", Title: "Work already implemented", Message: result.Preflight.Summary, ProjectID: project.ID, ProjectName: project.Name, EntityID: plan.ID, Route: "/work-items"})
		s.scheduleQueuedAgentTasks("planning_preflight_completed", traceID)
	} else if result.Preflight != nil && result.Preflight.Status == "not_feasible" {
		s.notify(notifications.Draft{Level: notifications.LevelWarning, Kind: "planning_not_feasible", Title: "Request is not feasible", Message: result.Preflight.Summary, ProjectID: project.ID, ProjectName: project.Name, EntityID: plan.ID, Route: "/work-items"})
	} else {
		s.notify(notifications.Draft{Level: notifications.LevelSuccess, Kind: "planning_ready", Title: "Plan ready for PM review", Message: plan.Request.Title, ProjectID: project.ID, EntityID: plan.ID, Route: "/work-items"})
	}
	s.scheduleQueuedAgentTasks("planning_finished", traceID)
}
