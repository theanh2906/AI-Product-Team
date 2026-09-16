package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

const remoteAPIVersion = "v1"

type remoteProjectReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type remotePlanSummary struct {
	ID        string                `json:"id"`
	Status    kanban.PlanningStatus `json:"status"`
	Revision  int                   `json:"revision"`
	Summary   string                `json:"summary,omitempty"`
	Error     string                `json:"error,omitempty"`
	Preflight *kanban.PlanPreflight `json:"preflight,omitempty"`
	Documents []kanban.Document     `json:"documents"`
	Sequence  *kanban.PlanSequence  `json:"sequence,omitempty"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

type remoteTaskSummary struct {
	ID                 string                `json:"id"`
	Key                string                `json:"key"`
	Title              string                `json:"title"`
	Role               kanban.AgentRole      `json:"role"`
	Column             kanban.Column         `json:"column"`
	Status             kanban.TaskStatus     `json:"status"`
	Priority           kanban.Priority       `json:"priority"`
	Description        string                `json:"description"`
	AcceptanceCriteria []string              `json:"acceptanceCriteria"`
	DependencyIDs      []string              `json:"dependencyIds"`
	BlockedReason      string                `json:"blockedReason,omitempty"`
	SequenceOrder      int                   `json:"sequenceOrder,omitempty"`
	Execution          *kanban.TaskExecution `json:"execution,omitempty"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

type remoteDependencySummary struct {
	ID     string            `json:"id"`
	Key    string            `json:"key"`
	Title  string            `json:"title"`
	Status kanban.TaskStatus `json:"status"`
}

type remoteArtifactSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	Source       string `json:"source"`
}

type remoteExecutionSummary struct {
	State         string `json:"state"`
	ActiveTaskID  string `json:"activeTaskId,omitempty"`
	Completed     int    `json:"completedTasks"`
	Total         int    `json:"totalTasks"`
	BlockedReason string `json:"blockedReason,omitempty"`
}

type remoteTicket struct {
	ID                 string                    `json:"id"`
	Key                string                    `json:"key"`
	Type               kanban.BacklogItemType    `json:"type"`
	Status             kanban.BacklogStatus      `json:"status"`
	Owner              string                    `json:"owner"`
	Title              string                    `json:"title"`
	Description        string                    `json:"description"`
	DeliveryTarget     string                    `json:"deliveryTarget"`
	RequiresUI         bool                      `json:"requiresUI"`
	AcceptanceCriteria []string                  `json:"acceptanceCriteria"`
	Feasibility        int                       `json:"feasibility,omitempty"`
	Severity           string                    `json:"severity,omitempty"`
	Plan               *remotePlanSummary        `json:"plan,omitempty"`
	Tasks              []remoteTaskSummary       `json:"tasks"`
	Dependencies       []remoteDependencySummary `json:"dependencies"`
	Artifacts          []remoteArtifactSummary   `json:"artifacts"`
	Execution          remoteExecutionSummary    `json:"execution"`
	BlockedReason      string                    `json:"blockedReason,omitempty"`
	CreatedAt          time.Time                 `json:"createdAt"`
	UpdatedAt          time.Time                 `json:"updatedAt"`
}

type remoteTicketResponse struct {
	APIVersion string                 `json:"apiVersion"`
	Project    remoteProjectReference `json:"project"`
	Ticket     remoteTicket           `json:"ticket"`
}

type remoteTicketListResponse struct {
	APIVersion string                 `json:"apiVersion"`
	Project    remoteProjectReference `json:"project"`
	Summary    remoteBoardSummary     `json:"summary"`
	Tickets    []remoteTicket         `json:"tickets"`
}

type remoteBoardSummary struct {
	Total     int `json:"total"`
	Backlog   int `json:"backlog"`
	Planning  int `json:"planning"`
	Running   int `json:"running"`
	Blocked   int `json:"blocked"`
	Completed int `json:"completed"`
}

type remoteAutopilotRequest struct {
	ID       string `json:"id"`
	All      bool   `json:"all"`
	Reviewer string `json:"reviewer"`
}

type remoteAutopilotResult struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	PlanID  string `json:"planId,omitempty"`
	Action  string `json:"action"`
	State   string `json:"state"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
}

type remoteAutopilotResponse struct {
	APIVersion  string                  `json:"apiVersion"`
	Project     remoteProjectReference  `json:"project"`
	Results     []remoteAutopilotResult `json:"results"`
	RequestedAt time.Time               `json:"requestedAt"`
}

func (s *server) listRemoteTickets(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "project_not_found"})
		return
	}
	board, err := s.boards.GetProjectBoard(r.Context(), project.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "This project does not have a board yet.", "code": "board_not_found"})
		return
	}
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	rawTypeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	typeFilter := normalizeRemoteType(rawTypeFilter)
	if rawTypeFilter != "" && typeFilter == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalidRemoteTypeMessage(r), "code": "invalid_backlog_type"})
		return
	}
	tickets := make([]remoteTicket, 0, len(board.Backlog))
	for _, item := range board.Backlog {
		ticket := buildRemoteTicket(board, item)
		if statusFilter != "" && statusFilter != strings.ToLower(ticket.Execution.State) && statusFilter != strings.ToLower(string(ticket.Status)) {
			continue
		}
		if typeFilter != "" && item.Type != typeFilter {
			continue
		}
		tickets = append(tickets, ticket)
	}
	sort.SliceStable(tickets, func(i, j int) bool { return tickets[i].Key < tickets[j].Key })
	writeJSON(w, http.StatusOK, remoteTicketListResponse{
		APIVersion: remoteAPIVersion,
		Project:    remoteProjectReference{ID: project.ID, Name: project.Name},
		Summary:    summarizeRemoteTickets(tickets),
		Tickets:    tickets,
	})
}

func (s *server) getRemoteTicket(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "project_not_found"})
		return
	}
	board, err := s.boards.GetProjectBoard(r.Context(), project.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "This project does not have a board yet.", "code": "board_not_found"})
		return
	}
	item, found := findRemoteBacklogItem(board, remoteTicketReference(r))
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found.", "code": "ticket_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, remoteTicketResponse{
		APIVersion: remoteAPIVersion,
		Project:    remoteProjectReference{ID: project.ID, Name: project.Name},
		Ticket:     buildRemoteTicket(board, item),
	})
}

func (s *server) startRemoteAutopilot(w http.ResponseWriter, r *http.Request) {
	var request remoteAutopilotRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	request.ID = strings.TrimSpace(request.ID)
	if request.All == (request.ID != "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provide exactly one of id or all=true.", "code": "invalid_autopilot_target"})
		return
	}
	project, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "project_not_found"})
		return
	}
	reviewer := strings.TrimSpace(request.Reviewer)
	if reviewer == "" {
		reviewer = "Remote Autopilot"
	}

	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()
	board, err := s.boards.GetProjectBoard(r.Context(), project.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "This project does not have a board yet.", "code": "board_not_found"})
		return
	}
	targets := make([]kanban.BacklogItem, 0)
	if request.All {
		for _, item := range board.Backlog {
			if item.Status == kanban.BacklogOpen || item.Status == kanban.BacklogPlanning {
				targets = append(targets, item)
			}
		}
		sort.SliceStable(targets, func(i, j int) bool {
			if targets[i].CreatedAt.Equal(targets[j].CreatedAt) {
				return targets[i].Key < targets[j].Key
			}
			return targets[i].CreatedAt.Before(targets[j].CreatedAt)
		})
	} else if item, found := findRemoteBacklogItem(board, request.ID); found {
		targets = append(targets, item)
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found.", "code": "ticket_not_found"})
		return
	}

	results := make([]remoteAutopilotResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, s.requestRemoteAutopilot(r.Context(), project, target.ID, reviewer, correlationID(r.Context())))
	}
	status := http.StatusAccepted
	if len(results) == 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, remoteAutopilotResponse{
		APIVersion:  remoteAPIVersion,
		Project:     remoteProjectReference{ID: project.ID, Name: project.Name},
		Results:     results,
		RequestedAt: time.Now().UTC(),
	})
}

func (s *server) requestRemoteAutopilot(ctx context.Context, project project, itemID, reviewer, traceID string) remoteAutopilotResult {
	board, err := s.boards.GetProjectBoard(ctx, project.ID)
	if err != nil {
		return remoteAutopilotResult{ID: itemID, Action: "rejected", State: "error", Message: "Unable to load the board.", Code: "board_not_found", Error: err.Error()}
	}
	item, found := findRemoteBacklogItem(board, itemID)
	if !found {
		return remoteAutopilotResult{ID: itemID, Action: "rejected", State: "error", Message: "Ticket not found.", Code: "ticket_not_found", Error: "ticket_not_found"}
	}
	result := remoteAutopilotResult{ID: item.ID, Key: item.Key}

	if item.Status == kanban.BacklogOpen {
		updated, plan, startErr := s.boards.StartBacklogPlanning(ctx, project.ID, item.ID)
		if startErr != nil {
			result.Action, result.State, result.Message, result.Code, result.Error = "rejected", "error", "Autopilot could not start planning.", "ticket_not_eligible", startErr.Error()
			return result
		}
		s.boardEvents.publish(updated)
		s.recordRemoteAutopilotEvent(project.ID, item, plan.ID, "autopilot.planning_started", "Autopilot started Team Lead planning", traceID, "running", nil)
		s.scheduleAutopilotApproval(project, plan.ID, reviewer, traceID)
		go s.runTeamLeadPlanning(updated.ID, project, plan, traceID)
		result.PlanID, result.Action, result.State, result.Message = plan.ID, "planning_started", "planning", "Planning started; Autopilot will approve and run the sequence when the plan is ready."
		return result
	}

	if item.Status == kanban.BacklogDone || item.Status == kanban.BacklogNotFeasible {
		result.Action, result.State, result.Message = "skipped", string(item.Status), "Ticket is already in a terminal state."
		return result
	}
	plan, found := findRemotePlan(board, item.PlanID)
	if !found {
		result.Action, result.State, result.Message, result.Code, result.Error = "rejected", "error", "Ticket is in planning but its plan is missing.", "plan_not_found", "plan_not_found"
		return result
	}
	result.PlanID = plan.ID
	switch plan.Status {
	case kanban.PlanningAnalyzing:
		s.scheduleAutopilotApproval(project, plan.ID, reviewer, traceID)
		result.Action, result.State, result.Message = "autopilot_attached", "planning", "Autopilot will continue when the active planning pass is ready."
	case kanban.PlanningAwaitingApproval, kanban.PlanningApproved:
		updated, approveErr := s.boards.ApprovePlanSequence(ctx, project.ID, plan.ID, reviewer)
		if approveErr != nil {
			result.Action, result.State, result.Message, result.Code, result.Error = "rejected", "error", "Plan cannot start as an Autopilot sequence.", "plan_not_ready", approveErr.Error()
			return result
		}
		s.boardEvents.publish(updated)
		s.recordRemoteAutopilotEvent(project.ID, item, plan.ID, "autopilot.sequence_started", "Autopilot approved and scheduled the plan sequence", traceID, "success", nil)
		s.scheduleQueuedAgentTasks("remote_autopilot_started", traceID)
		result.Action, result.State, result.Message = "sequence_started", "scheduled", "Plan approved and sequence scheduled."
	case kanban.PlanningChangesRequested, kanban.PlanningFailed:
		updated, retryPlan, retryErr := s.boards.MarkPlanAnalyzing(ctx, project.ID, plan.ID)
		if retryErr != nil {
			result.Action, result.State, result.Message, result.Code, result.Error = "rejected", "error", "Autopilot could not retry planning.", "plan_not_ready", retryErr.Error()
			return result
		}
		s.boardEvents.publish(updated)
		s.scheduleAutopilotApproval(project, retryPlan.ID, reviewer, traceID)
		go s.runTeamLeadPlanning(updated.ID, project, retryPlan, traceID)
		result.Action, result.State, result.Message = "planning_retried", "planning", "Planning retry started; Autopilot will continue when it is ready."
	case kanban.PlanningCompleted, kanban.PlanningNotFeasible:
		result.Action, result.State, result.Message = "skipped", string(plan.Status), "Planning finished without an implementation sequence."
	default:
		result.Action, result.State, result.Message, result.Code, result.Error = "rejected", "error", "Plan is not eligible for Autopilot.", "ticket_not_eligible", fmt.Sprintf("unsupported plan status %q", plan.Status)
	}
	return result
}

func (s *server) scheduleAutopilotApproval(project project, planID, reviewer, traceID string) {
	key := project.ID + ":" + planID
	s.autopilotMu.Lock()
	if _, exists := s.autopilotPlans[key]; exists {
		s.autopilotMu.Unlock()
		return
	}
	s.autopilotPlans[key] = struct{}{}
	s.autopilotMu.Unlock()

	go func() {
		defer func() {
			s.autopilotMu.Lock()
			delete(s.autopilotPlans, key)
			s.autopilotMu.Unlock()
		}()
		deadline := time.NewTimer(45 * time.Minute)
		defer deadline.Stop()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline.C:
				s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "autopilot_failed", Title: "Autopilot needs attention", Message: "Timed out waiting for Team Lead planning.", ProjectID: project.ID, ProjectName: project.Name, EntityID: planID, Route: "/work-items"})
				return
			case <-ticker.C:
				board, err := s.boards.GetProjectBoard(context.Background(), project.ID)
				if err != nil {
					continue
				}
				plan, found := findRemotePlan(board, planID)
				if !found {
					return
				}
				switch plan.Status {
				case kanban.PlanningAwaitingApproval:
					s.projectWorkMu.Lock()
					updated, approveErr := s.boards.ApprovePlanSequence(context.Background(), project.ID, planID, reviewer)
					s.projectWorkMu.Unlock()
					if approveErr != nil {
						s.recordRemoteAutopilotEvent(project.ID, kanban.BacklogItem{}, planID, "autopilot.sequence_failed", "Autopilot could not schedule the plan sequence", traceID, "failed", approveErr)
						s.notify(notifications.Draft{Level: notifications.LevelError, Kind: "autopilot_failed", Title: "Autopilot needs attention", Message: approveErr.Error(), ProjectID: project.ID, ProjectName: project.Name, EntityID: planID, Route: "/work-items"})
						return
					}
					s.boardEvents.publish(updated)
					s.recordRemoteAutopilotEvent(project.ID, kanban.BacklogItem{}, planID, "autopilot.sequence_started", "Autopilot approved and scheduled the plan sequence", traceID, "success", nil)
					s.scheduleQueuedAgentTasks("remote_autopilot_plan_ready", traceID)
					return
				case kanban.PlanningApproved, kanban.PlanningCompleted, kanban.PlanningNotFeasible, kanban.PlanningFailed:
					return
				}
			}
		}
	}()
}

func (s *server) recordRemoteAutopilotEvent(projectID string, item kanban.BacklogItem, planID, name, message, traceID, outcome string, eventErr error) {
	event := observability.Event{Category: "autopilot", Name: name, Message: message, CorrelationID: traceID, ProjectID: projectID, EntityType: "plan", EntityID: planID, Stage: "autopilot", Outcome: outcome, Attributes: map[string]any{"ticketId": item.ID, "ticketKey": item.Key}}
	if eventErr != nil {
		event.Level = observability.LevelError
		event.Attributes["error"] = eventErr.Error()
	}
	s.observability.Record(event)
}

func buildRemoteTicket(board kanban.Board, item kanban.BacklogItem) remoteTicket {
	ticket := remoteTicket{
		ID: item.ID, Key: item.Key, Type: item.Type, Status: item.Status, Owner: "PM", Title: item.Title, Description: item.Description,
		DeliveryTarget: item.DeliveryTarget, RequiresUI: item.RequiresUI, AcceptanceCriteria: append([]string{}, item.AcceptanceCriteria...),
		Feasibility: item.Feasibility, Severity: item.Severity, Tasks: []remoteTaskSummary{}, Dependencies: []remoteDependencySummary{}, Artifacts: []remoteArtifactSummary{}, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	for _, attachment := range item.Attachments {
		ticket.Artifacts = append(ticket.Artifacts, remoteArtifactSummary{ID: attachment.ID, Title: attachment.OriginalName, Kind: "request-attachment", RelativePath: attachment.RelativePath, MediaType: attachment.MediaType, Source: "request"})
	}
	if plan, found := findRemotePlan(board, item.PlanID); found {
		ticket.Plan = &remotePlanSummary{ID: plan.ID, Status: plan.Status, Revision: plan.Revision, Summary: plan.Summary, Error: plan.Error, Preflight: plan.Preflight, Documents: append([]kanban.Document{}, plan.Documents...), Sequence: plan.Sequence, UpdatedAt: plan.UpdatedAt}
		switch plan.Status {
		case kanban.PlanningAnalyzing:
			ticket.Owner = "Team Lead"
		case kanban.PlanningAwaitingApproval:
			ticket.Owner = "PM"
		case kanban.PlanningApproved:
			ticket.Owner = "Autopilot"
		}
	}
	taskByID := make(map[string]kanban.Task)
	for _, task := range board.Tasks {
		taskByID[task.ID] = task
	}
	dependencyIDs := make(map[string]struct{})
	for _, task := range board.Tasks {
		if task.PlanID != item.PlanID || item.PlanID == "" {
			continue
		}
		ticket.Tasks = append(ticket.Tasks, remoteTaskSummary{
			ID: task.ID, Key: task.Key, Title: task.Title, Role: task.Role, Column: task.Column, Status: task.Status, Priority: task.Priority,
			Description: task.Description, AcceptanceCriteria: append([]string{}, task.AcceptanceCriteria...), DependencyIDs: append([]string{}, task.DependencyIDs...),
			BlockedReason: task.BlockedReason, SequenceOrder: task.SequenceOrder, Execution: task.Execution, UpdatedAt: task.UpdatedAt,
		})
		for _, dependencyID := range task.DependencyIDs {
			dependencyIDs[dependencyID] = struct{}{}
		}
		if task.Execution != nil {
			for _, artifact := range task.Execution.Artifacts {
				ticket.Artifacts = append(ticket.Artifacts, remoteArtifactSummary{ID: artifact.ID, Title: artifact.Title, Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, Source: task.Key})
			}
		}
		if task.ID == board.ActiveTaskID {
			ticket.Owner = task.Role.Label()
		}
	}
	for dependencyID := range dependencyIDs {
		if dependency, found := taskByID[dependencyID]; found {
			ticket.Dependencies = append(ticket.Dependencies, remoteDependencySummary{ID: dependency.ID, Key: dependency.Key, Title: dependency.Title, Status: dependency.Status})
		}
	}
	sort.SliceStable(ticket.Tasks, func(i, j int) bool {
		left, right := ticket.Tasks[i], ticket.Tasks[j]
		if left.SequenceOrder > 0 || right.SequenceOrder > 0 {
			return left.SequenceOrder < right.SequenceOrder
		}
		return left.Key < right.Key
	})
	sort.SliceStable(ticket.Dependencies, func(i, j int) bool { return ticket.Dependencies[i].Key < ticket.Dependencies[j].Key })
	sort.SliceStable(ticket.Artifacts, func(i, j int) bool {
		if ticket.Artifacts[i].RelativePath == ticket.Artifacts[j].RelativePath {
			return ticket.Artifacts[i].ID < ticket.Artifacts[j].ID
		}
		return ticket.Artifacts[i].RelativePath < ticket.Artifacts[j].RelativePath
	})
	ticket.Execution = summarizeRemoteExecution(board, ticket)
	ticket.BlockedReason = ticket.Execution.BlockedReason
	return ticket
}

func summarizeRemoteExecution(board kanban.Board, ticket remoteTicket) remoteExecutionSummary {
	summary := remoteExecutionSummary{State: string(ticket.Status), Total: len(ticket.Tasks)}
	if ticket.Plan != nil {
		summary.State = string(ticket.Plan.Status)
		if ticket.Plan.Error != "" {
			summary.BlockedReason = ticket.Plan.Error
		}
		if ticket.Plan.Sequence != nil {
			summary.State = string(ticket.Plan.Sequence.Status)
			summary.BlockedReason = ticket.Plan.Sequence.BlockedReason
		}
	}
	for _, task := range ticket.Tasks {
		if task.Status == kanban.TaskCompleted {
			summary.Completed++
		}
		if task.Status == kanban.TaskBlocked && summary.BlockedReason == "" {
			summary.State = "blocked"
			summary.BlockedReason = task.BlockedReason
		}
		if task.ID == board.ActiveTaskID {
			summary.ActiveTaskID = task.ID
			summary.State = "running"
		}
	}
	if summary.Total > 0 && summary.Completed == summary.Total {
		summary.State = "completed"
	}
	return summary
}

func summarizeRemoteTickets(tickets []remoteTicket) remoteBoardSummary {
	summary := remoteBoardSummary{Total: len(tickets)}
	for _, ticket := range tickets {
		switch ticket.Execution.State {
		case "backlog":
			summary.Backlog++
		case "analyzing", "awaiting_approval", "planning":
			summary.Planning++
		case "scheduled", "running":
			summary.Running++
		case "blocked", "failed":
			summary.Blocked++
		case "completed", "done", "not_feasible":
			summary.Completed++
		}
	}
	return summary
}

func findRemoteBacklogItem(board kanban.Board, reference string) (kanban.BacklogItem, bool) {
	reference = strings.TrimSpace(reference)
	for _, item := range board.Backlog {
		if strings.EqualFold(item.ID, reference) || strings.EqualFold(item.Key, reference) {
			return item, true
		}
	}
	return kanban.BacklogItem{}, false
}

func findRemotePlan(board kanban.Board, planID string) (kanban.Plan, bool) {
	for _, plan := range board.Plans {
		if plan.ID == planID {
			return plan, true
		}
	}
	return kanban.Plan{}, false
}

func normalizeRemoteType(value string) kanban.BacklogItemType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "task", "todo":
		return kanban.BacklogTodo
	case "feature":
		return kanban.BacklogFeature
	case "bug":
		return kanban.BacklogBug
	default:
		return ""
	}
}

func writeBacklogAPIError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if !isRemoteAutomationBacklogPath(r.URL.Path) && !strings.HasSuffix(r.URL.Path, "/tickets") {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	message := err.Error()
	code := "invalid_request"
	switch {
	case strings.Contains(message, "project"):
		code = "project_not_found"
	case strings.Contains(message, "backlog type"):
		code = "invalid_backlog_type"
		message = invalidRemoteTypeMessage(r)
	case strings.Contains(message, "backlog title"):
		code = "title_required"
	case strings.Contains(message, "backlog description"):
		code = "description_required"
	}
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

func writeBacklogDecodeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if strings.HasSuffix(r.URL.Path, "/tickets") || isRemoteAutomationBacklogPath(r.URL.Path) {
		writeJSON(w, status, map[string]string{"error": message, "code": code})
		return
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func remoteTicketReference(r *http.Request) string {
	if reference := strings.TrimSpace(r.PathValue("ticketRef")); reference != "" {
		return reference
	}
	return strings.TrimSpace(r.PathValue("ticketID"))
}

func isRemoteAutomationBacklogPath(path string) bool {
	return strings.HasSuffix(path, "/automation/backlog")
}

func invalidRemoteTypeMessage(r *http.Request) string {
	if isRemoteAutomationBacklogPath(r.URL.Path) || strings.Contains(r.URL.Path, "/automation/status") {
		return "type must be todo, feature, or bug"
	}
	return "type must be TASK, FEATURE, or BUG"
}
