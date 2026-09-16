package kanban

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

var fallbackIDSequence atomic.Uint64

type Service struct {
	mu         sync.Mutex
	repository BoardRepository
	now        func() time.Time
}

func NewService(repository BoardRepository) *Service {
	return &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) ListBoards(ctx context.Context) ([]Board, error) {
	return s.repository.List(ctx)
}

func (s *Service) GetProjectBoard(ctx context.Context, projectID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return Board{}, err
	}
	now := s.now()
	changed := markFinishedBacklogItems(&board, now)
	if requeueResolvedWaitingQATasks(&board, now) {
		changed = true
	}
	if changed {
		if err := s.repository.Update(ctx, board); err != nil {
			return Board{}, err
		}
	}
	return board, nil
}

func (s *Service) RequestQueueStop(ctx context.Context, projectID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return Board{}, err
	}
	now := s.now()
	status := QueueControlPaused
	var pausedAt *time.Time
	if board.ActiveTaskID != "" {
		status = QueueControlStopping
	} else {
		pausedAt = &now
	}
	requestedAt := now
	if board.QueueControl != nil && board.QueueControl.RequestedAt != nil {
		requestedAt = *board.QueueControl.RequestedAt
	}
	board.QueueControl = &QueueControl{
		Status:      status,
		RequestedAt: &requestedAt,
		PausedAt:    pausedAt,
		UpdatedAt:   now,
	}
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) ContinueQueue(ctx context.Context, projectID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return Board{}, err
	}
	if board.QueueControl == nil {
		return board, nil
	}
	now := s.now()
	board.QueueControl = nil
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) PauseStoppingQueues(ctx context.Context) ([]Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	boards, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	paused := make([]Board, 0)
	for _, board := range boards {
		if board.ActiveTaskID != "" || board.QueueControl == nil || board.QueueControl.Status != QueueControlStopping {
			continue
		}
		if board.QueueControl.RequestedAt == nil {
			requestedAt := now
			board.QueueControl.RequestedAt = &requestedAt
		}
		pausedAt := now
		board.QueueControl.Status = QueueControlPaused
		board.QueueControl.PausedAt = &pausedAt
		board.QueueControl.UpdatedAt = now
		board.UpdatedAt = now
		if err := s.repository.Update(ctx, board); err != nil {
			return nil, err
		}
		paused = append(paused, board)
	}
	return paused, nil
}

func (s *Service) DeleteProjectBoard(ctx context.Context, projectID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := s.repository.Delete(ctx, board.ID); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) CreatePlan(ctx context.Context, projectID, projectName string, request WorkRequest) (Board, Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID = strings.TrimSpace(projectID)
	projectName = strings.TrimSpace(projectName)
	request.Title = strings.TrimSpace(request.Title)
	request.Description = strings.TrimSpace(request.Description)
	if projectID == "" || projectName == "" {
		return Board{}, Plan{}, fmt.Errorf("project is required")
	}
	if len(request.Title) < 4 || len(request.Title) > 120 {
		return Board{}, Plan{}, fmt.Errorf("request title must contain 4 to 120 characters")
	}
	if len(request.Description) < 10 || len(request.Description) > 6000 {
		return Board{}, Plan{}, fmt.Errorf("request description must contain 10 to 6000 characters")
	}
	if len(request.AcceptanceCriteria) == 0 {
		return Board{}, Plan{}, fmt.Errorf("select at least one acceptance criterion")
	}

	now := s.now()
	if strings.TrimSpace(request.ID) == "" {
		request.ID = newID("request", now)
	}
	request.CreatedAt = now
	request.Attachments = cloneRequestAttachments(request.Attachments)
	plan := Plan{
		ID:        newID("plan", now),
		Request:   request,
		Status:    PlanningAnalyzing,
		Documents: []Document{},
		Reviews:   []PlanReview{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	board, err := s.repository.FindByProject(ctx, projectID)
	switch {
	case err == nil:
		board.ProjectName = projectName
		board.Plans = append(board.Plans, plan)
		board.UpdatedAt = now
		if err := s.repository.Update(ctx, board); err != nil {
			return Board{}, Plan{}, err
		}
	case errors.Is(err, ErrNotFound):
		board = Board{
			ID:          newID("board", now),
			ProjectID:   projectID,
			ProjectName: projectName,
			Plans:       []Plan{plan},
			Backlog:     []BacklogItem{},
			Tasks:       []Task{},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.repository.Create(ctx, board); err != nil {
			return Board{}, Plan{}, err
		}
	default:
		return Board{}, Plan{}, err
	}
	return board, plan, nil
}

func (s *Service) AddBacklogItem(ctx context.Context, projectID, projectName string, draft BacklogDraft) (Board, BacklogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID = strings.TrimSpace(projectID)
	projectName = strings.TrimSpace(projectName)
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Description = strings.TrimSpace(draft.Description)
	if projectID == "" || projectName == "" {
		return Board{}, BacklogItem{}, fmt.Errorf("project is required")
	}
	if !draft.Type.Valid() {
		return Board{}, BacklogItem{}, fmt.Errorf("backlog type must be feature, bug, or todo")
	}
	if len(draft.Title) < 4 || len(draft.Title) > 120 {
		return Board{}, BacklogItem{}, fmt.Errorf("backlog title must contain 4 to 120 characters")
	}
	if len(draft.Description) < 10 || len(draft.Description) > 6000 {
		return Board{}, BacklogItem{}, fmt.Errorf("backlog description must contain 10 to 6000 characters")
	}
	criteria := compactStrings(draft.AcceptanceCriteria)
	if len(criteria) == 0 {
		criteria = []string{"The approved scope works end to end", "Critical behavior has regression coverage"}
	}

	now := s.now()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Board{}, BacklogItem{}, err
	}
	if errors.Is(err, ErrNotFound) {
		board = Board{
			ID: newID("board", now), ProjectID: projectID, ProjectName: projectName,
			Plans: []Plan{}, Backlog: []BacklogItem{}, Tasks: []Task{},
			CreatedAt: now, UpdatedAt: now,
		}
	}
	finishedChanged := !errors.Is(err, ErrNotFound) && markFinishedBacklogItems(&board, now)
	for _, existing := range board.Backlog {
		if draft.SourceReference != "" && existing.Status != BacklogDone && existing.Source == strings.TrimSpace(draft.Source) && existing.SourceReference == strings.TrimSpace(draft.SourceReference) {
			if finishedChanged {
				if updateErr := s.repository.Update(ctx, board); updateErr != nil {
					return Board{}, BacklogItem{}, updateErr
				}
			}
			return board, existing, nil
		}
	}

	requestID := strings.TrimSpace(draft.RequestID)
	if requestID == "" {
		requestID = newID("request", now)
	}
	item := BacklogItem{
		ID: newID("backlog", now), RequestID: requestID, ArtifactPath: strings.TrimSpace(draft.ArtifactPath), Key: backlogKey(draft.Type, board.Backlog), Type: draft.Type,
		Source: strings.TrimSpace(draft.Source), SourceReference: strings.TrimSpace(draft.SourceReference),
		Title: draft.Title, Description: draft.Description, DeliveryTarget: normalizedDeliveryTarget(draft.DeliveryTarget),
		RequiresUI: draft.RequiresUI, AcceptanceCriteria: criteria, Attachments: cloneRequestAttachments(draft.Attachments), Feasibility: clampPercent(draft.Feasibility),
		Intake:   cloneIntakeSubmission(draft.Intake),
		Severity: strings.ToLower(strings.TrimSpace(draft.Severity)), Status: BacklogOpen,
		CreatedAt: now, UpdatedAt: now,
	}
	board.ProjectName = projectName
	board.Backlog = append(board.Backlog, item)
	board.UpdatedAt = now
	if errors.Is(err, ErrNotFound) {
		err = s.repository.Create(ctx, board)
	} else {
		err = s.repository.Update(ctx, board)
	}
	if err != nil {
		return Board{}, BacklogItem{}, err
	}
	return board, item, nil
}

func (s *Service) MarkBacklogItemReportedAgain(ctx context.Context, projectID, itemID string, waitingQATaskIDs ...string) (Board, BacklogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return Board{}, BacklogItem{}, err
	}
	itemIndex := findBacklogItem(board, strings.TrimSpace(itemID))
	if itemIndex < 0 {
		return Board{}, BacklogItem{}, fmt.Errorf("backlog item not found")
	}
	item := &board.Backlog[itemIndex]
	if item.Status != BacklogOpen && item.Status != BacklogPlanning {
		return Board{}, BacklogItem{}, fmt.Errorf("only active backlog items can be highlighted")
	}
	now := s.now()
	item.RepeatReports++
	item.LastReportedAt = &now
	item.WaitingQATaskIDs = appendUnique(item.WaitingQATaskIDs, compactStrings(waitingQATaskIDs)...)
	item.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, BacklogItem{}, err
	}
	return board, *item, nil
}

func (s *Service) RemoveBacklogItem(ctx context.Context, projectID, itemID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID = strings.TrimSpace(projectID)
	itemID = strings.TrimSpace(itemID)
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	itemIndex := findBacklogItem(board, itemID)
	if itemIndex < 0 {
		return Board{}, fmt.Errorf("backlog item not found")
	}
	removedItem := board.Backlog[itemIndex]
	board.Backlog = append(board.Backlog[:itemIndex], board.Backlog[itemIndex+1:]...)
	now := s.now()
	board.UpdatedAt = now
	requeueResolvedQATaskIfReady(&board, &removedItem, now)
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) StartBacklogPlanning(ctx context.Context, projectID, itemID string) (Board, Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, item, err := s.findBoardBacklogItem(ctx, projectID, itemID)
	if err != nil {
		return Board{}, Plan{}, err
	}
	if item.Status != BacklogOpen {
		return Board{}, Plan{}, fmt.Errorf("only an open backlog item can enter Team Lead planning")
	}
	plan := s.beginBacklogPlanningLocked(&board, item, s.now())
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, Plan{}, err
	}
	return board, plan, nil
}

// EnsureBacklogPlanning idempotently moves a backlog item into Team Lead
// planning. When the item is already in planning, it returns the existing
// plan without mutating the board and reports started=false so callers do
// not start a duplicate Team Lead run.
func (s *Service) EnsureBacklogPlanning(ctx context.Context, projectID, itemID string) (Board, Plan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, item, err := s.findBoardBacklogItem(ctx, projectID, itemID)
	if err != nil {
		return Board{}, Plan{}, false, err
	}
	if item.Status == BacklogPlanning {
		planIndex := findPlan(board, item.PlanID)
		if planIndex < 0 {
			return Board{}, Plan{}, false, fmt.Errorf("backlog item is in planning but its plan was not found")
		}
		return board, board.Plans[planIndex], false, nil
	}
	if item.Status != BacklogOpen {
		return Board{}, Plan{}, false, fmt.Errorf("only an open backlog item can enter Team Lead planning")
	}
	plan := s.beginBacklogPlanningLocked(&board, item, s.now())
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, Plan{}, false, err
	}
	return board, plan, true, nil
}

func (s *Service) findBoardBacklogItem(ctx context.Context, projectID, itemID string) (Board, *BacklogItem, error) {
	board, err := s.repository.FindByProject(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return Board{}, nil, err
	}
	itemIndex := findBacklogItem(board, strings.TrimSpace(itemID))
	if itemIndex < 0 {
		return Board{}, nil, fmt.Errorf("backlog item not found")
	}
	return board, &board.Backlog[itemIndex], nil
}

// beginBacklogPlanningLocked creates a new Team Lead plan from an open
// backlog item and mutates board and item in place. Callers must hold s.mu
// and confirm item.Status == BacklogOpen before calling.
func (s *Service) beginBacklogPlanningLocked(board *Board, item *BacklogItem, now time.Time) Plan {
	request := WorkRequest{
		ID: item.RequestID, ArtifactPath: item.ArtifactPath, Title: item.Title, Description: item.Description,
		WorkType: string(item.Type), DeliveryTarget: item.DeliveryTarget, RequiresUI: item.RequiresUI,
		AcceptanceCriteria: append([]string{}, item.AcceptanceCriteria...), CreatedAt: now,
		Attachments: cloneRequestAttachments(item.Attachments), Intake: cloneIntakeSubmission(item.Intake),
	}
	if strings.TrimSpace(request.ID) == "" {
		request.ID = newID("request", now)
		item.RequestID = request.ID
	}
	plan := Plan{
		ID: newID("plan", now), Request: request, Status: PlanningAnalyzing,
		Documents: []Document{}, Reviews: []PlanReview{}, CreatedAt: now, UpdatedAt: now,
	}
	item.Status = BacklogPlanning
	item.PlanID = plan.ID
	item.UpdatedAt = now
	board.Plans = append(board.Plans, plan)
	board.UpdatedAt = now
	return plan
}

func (s *Service) ApplyDraft(ctx context.Context, boardID, planID, threadID string, draft PlanDraft) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.Get(ctx, boardID)
	if err != nil {
		return Board{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, fmt.Errorf("plan not found")
	}
	if strings.TrimSpace(draft.Summary) == "" || len(draft.Tasks) == 0 {
		return Board{}, fmt.Errorf("Team Lead returned an incomplete plan")
	}

	now := s.now()
	plan := &board.Plans[planIndex]
	plan.Revision++
	plan.Summary = strings.TrimSpace(draft.Summary)
	plan.Error = ""
	plan.ThreadID = strings.TrimSpace(threadID)
	plan.Status = PlanningAwaitingApproval
	plan.UpdatedAt = now

	documentByRef := make(map[string]string, len(draft.Documents))
	plan.Documents = make([]Document, 0, len(draft.Documents))
	for index, candidate := range draft.Documents {
		ref := strings.TrimSpace(candidate.Ref)
		if ref == "" {
			ref = fmt.Sprintf("document-%d", index+1)
		}
		if _, duplicated := documentByRef[ref]; duplicated {
			return Board{}, fmt.Errorf("duplicate document reference %q", ref)
		}
		if strings.TrimSpace(candidate.Title) == "" || strings.TrimSpace(candidate.Content) == "" {
			return Board{}, fmt.Errorf("document %q must include a title and content", ref)
		}
		audience := validAudience(candidate.Audience)
		if len(audience) == 0 {
			return Board{}, fmt.Errorf("document %q must target at least one agent", ref)
		}
		id := newSequenceID("document", now, index+1)
		documentByRef[ref] = id
		plan.Documents = append(plan.Documents, Document{
			ID: id, PlanID: plan.ID, Title: strings.TrimSpace(candidate.Title), Kind: strings.TrimSpace(candidate.Kind),
			Audience: audience, Content: strings.TrimSpace(candidate.Content), Version: plan.Revision, CreatedAt: now,
		})
	}

	board.Tasks = removePlanTasks(board.Tasks, plan.ID)
	taskByRef := make(map[string]string, len(draft.Tasks))
	newTasks := make([]Task, 0, len(draft.Tasks))
	roleSequences := map[AgentRole]int{}
	for _, existingTask := range board.Tasks {
		roleSequences[existingTask.Role]++
	}
	for index, candidate := range draft.Tasks {
		if !candidate.Role.Valid() {
			return Board{}, fmt.Errorf("task %d has unsupported agent role %q", index+1, candidate.Role)
		}
		if candidate.Role == RoleDesigner && !plan.Request.RequiresUI {
			return Board{}, fmt.Errorf("Team Lead created a Designer task for an engineering-only request")
		}
		ref := strings.TrimSpace(candidate.Ref)
		if ref == "" {
			ref = fmt.Sprintf("task-%d", index+1)
		}
		if _, duplicated := taskByRef[ref]; duplicated {
			return Board{}, fmt.Errorf("duplicate task reference %q", ref)
		}
		if strings.TrimSpace(candidate.Title) == "" || strings.TrimSpace(candidate.Description) == "" || len(compactStrings(candidate.AcceptanceCriteria)) == 0 {
			return Board{}, fmt.Errorf("task %q must include a title, description, and acceptance criteria", ref)
		}
		id := newSequenceID("task", now, index+1)
		taskByRef[ref] = id
		roleSequences[candidate.Role]++
		priority := candidate.Priority
		if !priority.Valid() {
			priority = PriorityMedium
		}
		documentIDs := make([]string, 0, len(candidate.Documents))
		for _, documentRef := range candidate.Documents {
			if documentID := documentByRef[strings.TrimSpace(documentRef)]; documentID != "" {
				documentIDs = append(documentIDs, documentID)
			}
		}
		criteria := compactStrings(candidate.AcceptanceCriteria)
		if candidate.Role == RoleQA {
			criteria = appendUnique(criteria,
				"Implementation matches the approved design or specification",
				"No unresolved reproducible bugs remain within the approved scope and test coverage",
			)
		}
		newTasks = append(newTasks, Task{
			ID: id, PlanID: plan.ID, Key: taskKey(candidate.Role, roleSequences[candidate.Role]),
			Title: FormatTaskTitle(candidate.Role, candidate.Title), Role: candidate.Role, Column: ColumnPlanning,
			Status: TaskPlanned, Priority: priority, Description: strings.TrimSpace(candidate.Description),
			AcceptanceCriteria: criteria, DependencyIDs: []string{}, DocumentIDs: documentIDs, CreatedAt: now, UpdatedAt: now,
		})
	}
	for index, candidate := range draft.Tasks {
		for _, dependencyRef := range candidate.Dependencies {
			dependencyID := taskByRef[strings.TrimSpace(dependencyRef)]
			if dependencyID == "" {
				return Board{}, fmt.Errorf("task %q references unknown dependency %q", candidate.Ref, dependencyRef)
			}
			if dependencyID == newTasks[index].ID {
				return Board{}, fmt.Errorf("task %q cannot depend on itself", candidate.Ref)
			}
			newTasks[index].DependencyIDs = append(newTasks[index].DependencyIDs, dependencyID)
		}
	}
	if err := validateDependencyGraph(newTasks); err != nil {
		return Board{}, err
	}
	board.Tasks = append(board.Tasks, newTasks...)
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) CompletePlanAsAlreadyImplemented(ctx context.Context, boardID, planID, threadID string, preflight PlanPreflight) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.Get(ctx, boardID)
	if err != nil {
		return Board{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, fmt.Errorf("plan not found")
	}
	if strings.TrimSpace(preflight.Summary) == "" {
		return Board{}, fmt.Errorf("Team Lead verification summary is required")
	}
	now := s.now()
	completedAt := now
	preflight.Status = "already_implemented"
	preflight.Summary = strings.TrimSpace(preflight.Summary)
	preflight.Evidence = compactStrings(preflight.Evidence)
	preflight.Verification = compactStrings(preflight.Verification)
	preflight.RemainingWork = compactStrings(preflight.RemainingWork)
	preflight.CompletedAt = &completedAt

	plan := &board.Plans[planIndex]
	plan.Revision++
	plan.Status = PlanningCompleted
	plan.Summary = preflight.Summary
	plan.Error = ""
	plan.ThreadID = strings.TrimSpace(threadID)
	plan.Preflight = &preflight
	plan.Documents = nil
	plan.Sequence = nil
	plan.UpdatedAt = now
	board.Tasks = removePlanTasks(board.Tasks, plan.ID)
	completedItem := completeBacklogForPlan(&board, plan.ID, now)
	requeueResolvedQATaskIfReady(&board, completedItem, now)
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) CompletePlanAsNotFeasible(ctx context.Context, boardID, planID, threadID string, preflight PlanPreflight) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.Get(ctx, boardID)
	if err != nil {
		return Board{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, fmt.Errorf("plan not found")
	}
	if strings.TrimSpace(preflight.Summary) == "" {
		return Board{}, fmt.Errorf("Team Lead feasibility summary is required")
	}
	now := s.now()
	completedAt := now
	preflight.Status = "not_feasible"
	preflight.Summary = strings.TrimSpace(preflight.Summary)
	preflight.Evidence = compactStrings(preflight.Evidence)
	preflight.Verification = compactStrings(preflight.Verification)
	preflight.RemainingWork = compactStrings(preflight.RemainingWork)
	preflight.CompletedAt = &completedAt

	plan := &board.Plans[planIndex]
	plan.Revision++
	plan.Status = PlanningNotFeasible
	plan.Summary = preflight.Summary
	plan.Error = ""
	plan.ThreadID = strings.TrimSpace(threadID)
	plan.Preflight = &preflight
	plan.Documents = nil
	plan.Sequence = nil
	plan.UpdatedAt = now
	board.Tasks = removePlanTasks(board.Tasks, plan.ID)
	markBacklogForPlan(&board, plan.ID, BacklogNotFeasible, now)
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) FailPlanning(ctx context.Context, boardID, planID string, planningError error) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.Get(ctx, boardID)
	if err != nil {
		return Board{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, fmt.Errorf("plan not found")
	}
	now := s.now()
	board.Plans[planIndex].Status = PlanningFailed
	board.Plans[planIndex].Error = strings.TrimSpace(planningError.Error())
	board.Plans[planIndex].UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) ReviewPlan(ctx context.Context, projectID, planID, decision, reason, reviewer string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	if err := reviewPlanLocked(&board, planID, decision, reason, reviewer, s.now()); err != nil {
		return Board{}, err
	}
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) ApprovePlanSequence(ctx context.Context, projectID, planID, reviewer string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, fmt.Errorf("plan not found")
	}
	if board.Plans[planIndex].Sequence != nil {
		return board, nil
	}
	taskIndices, err := validatePlanSequence(board, board.Plans[planIndex])
	if err != nil {
		return Board{}, err
	}
	now := s.now()
	if board.Plans[planIndex].Status == PlanningAwaitingApproval {
		if err := reviewPlanLocked(&board, planID, "approve", "", reviewer, now); err != nil {
			return Board{}, err
		}
	} else if board.Plans[planIndex].Status != PlanningApproved {
		return Board{}, fmt.Errorf("only a plan awaiting approval or an approved plan can start a sequence")
	}
	plan := &board.Plans[planIndex]
	plan.Sequence = &PlanSequence{Status: PlanSequenceScheduled, ScheduledAt: now}
	for order, taskIndex := range taskIndices {
		task := &board.Tasks[taskIndex]
		task.SequenceOrder = order + 1
		task.Column = task.Role.QueueColumn()
		task.Status = TaskQueued
		task.BlockedReason = ""
		task.Execution = nil
		task.BuildVerification = nil
		task.UpdatedAt = now
		if order > 0 {
			task.DependencyIDs = appendUnique(task.DependencyIDs, board.Tasks[taskIndices[order-1]].ID)
		}
	}
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func reviewPlanLocked(board *Board, planID, decision, reason, reviewer string, now time.Time) error {
	planIndex := findPlan(*board, planID)
	if planIndex < 0 {
		return fmt.Errorf("plan not found")
	}
	plan := &board.Plans[planIndex]
	if plan.Status != PlanningAwaitingApproval {
		return fmt.Errorf("only a plan awaiting approval can be reviewed")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	reason = strings.TrimSpace(reason)
	if decision != "approve" && decision != "deny" {
		return fmt.Errorf("decision must be approve or deny")
	}
	if decision == "deny" && len(reason) < 5 {
		return fmt.Errorf("provide a clear reason when denying a plan")
	}
	plan.Reviews = append(plan.Reviews, PlanReview{Decision: decision, Reason: reason, Reviewer: defaultReviewer(reviewer), Revision: plan.Revision, CreatedAt: now})
	if decision == "approve" {
		plan.Status = PlanningApproved
	} else {
		plan.Status = PlanningChangesRequested
	}
	plan.UpdatedAt = now
	board.UpdatedAt = now
	return nil
}

func validatePlanSequence(board Board, plan Plan) ([]int, error) {
	taskIndices := make([]int, 0)
	for index := range board.Tasks {
		if board.Tasks[index].PlanID == plan.ID {
			taskIndices = append(taskIndices, index)
		}
	}
	if len(taskIndices) == 0 {
		return nil, fmt.Errorf("the plan has no tasks to run")
	}
	if board.Tasks[taskIndices[len(taskIndices)-1]].Role != RoleQA {
		return nil, fmt.Errorf("a plan sequence must end with a QA task")
	}
	hasDeveloper := false
	phase := 0
	orderByID := make(map[string]int, len(taskIndices))
	for order, taskIndex := range taskIndices {
		task := board.Tasks[taskIndex]
		orderByID[task.ID] = order
		currentPhase := sequenceRolePhase(task.Role)
		if currentPhase < phase {
			return nil, fmt.Errorf("plan tasks must be ordered Designer, Developer, then QA")
		}
		phase = currentPhase
		if task.Role == RoleDeveloper {
			hasDeveloper = true
		}
		if task.Role == RoleQA && order != len(taskIndices)-1 {
			return nil, fmt.Errorf("only the final task in a plan sequence can be QA")
		}
	}
	if !hasDeveloper {
		return nil, fmt.Errorf("a plan sequence requires at least one Developer task before QA")
	}
	for order, taskIndex := range taskIndices {
		for _, dependencyID := range board.Tasks[taskIndex].DependencyIDs {
			dependencyOrder, found := orderByID[dependencyID]
			if !found || dependencyOrder >= order {
				return nil, fmt.Errorf("task %s depends on work that does not precede it in the sequence", board.Tasks[taskIndex].Key)
			}
		}
	}
	return taskIndices, nil
}

func sequenceRolePhase(role AgentRole) int {
	switch role {
	case RoleDesigner:
		return 0
	case RoleDeveloper:
		return 1
	case RoleQA:
		return 2
	default:
		return 3
	}
}

func (s *Service) MarkPlanAnalyzing(ctx context.Context, projectID, planID string) (Board, Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, Plan{}, err
	}
	planIndex := findPlan(board, planID)
	if planIndex < 0 {
		return Board{}, Plan{}, fmt.Errorf("plan not found")
	}
	plan := &board.Plans[planIndex]
	if plan.Status != PlanningChangesRequested && plan.Status != PlanningFailed {
		return Board{}, Plan{}, fmt.Errorf("this plan is not ready for another Team Lead pass")
	}
	now := s.now()
	plan.Status = PlanningAnalyzing
	plan.Error = ""
	plan.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, Plan{}, err
	}
	return board, *plan, nil
}

func (s *Service) MoveTask(ctx context.Context, projectID, taskID string, target Column) (Board, error) {
	return s.routeTask(ctx, projectID, taskID, target, false)
}

func (s *Service) RestartTask(ctx context.Context, projectID, taskID string) (Board, error) {
	return s.routeTask(ctx, projectID, taskID, "", true)
}

func (s *Service) ApproveDesignHandoff(ctx context.Context, projectID, taskID, reviewer string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if task.Role != RoleDesigner || task.SequenceOrder > 0 {
		return Board{}, fmt.Errorf("only manual Designer handoffs require PM approval")
	}
	if task.Status != TaskDesignReview {
		return Board{}, fmt.Errorf("only a Designer handoff awaiting PM approval can be approved")
	}
	if task.Execution == nil {
		return Board{}, fmt.Errorf("Designer handoff approval requires a delivery report")
	}
	now := s.now()
	task.Status = TaskCompleted
	task.Column = ColumnDone
	task.BlockedReason = ""
	task.UpdatedAt = now
	completeBacklogForFinishedPlan(&board, task.PlanID, now)
	board.UpdatedAt = now
	_ = defaultReviewer(reviewer)
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) SubmitDesignFeedback(ctx context.Context, projectID, taskID, feedback, reviewer string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if task.Role != RoleDesigner || task.SequenceOrder > 0 {
		return Board{}, fmt.Errorf("only manual Designer handoffs can receive PM feedback")
	}
	if task.Status != TaskDesignReview {
		return Board{}, fmt.Errorf("only a Designer handoff awaiting PM review can receive feedback")
	}
	if task.Execution == nil {
		return Board{}, fmt.Errorf("Designer feedback requires an existing delivery report")
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return Board{}, fmt.Errorf("Designer feedback requires a reason")
	}
	now := s.now()
	task.RevisionHistory = append(task.RevisionHistory, TaskRevision{
		Revision:    nextTaskRevision(*task),
		Feedback:    feedback,
		Reviewer:    defaultReviewer(reviewer),
		RequestedAt: now,
		ThreadID:    strings.TrimSpace(task.ThreadID),
		Execution:   cloneTaskExecution(*task.Execution),
	})
	task.Status = TaskQueued
	task.Column = ColumnDesigner
	task.BlockedReason = ""
	task.Execution = nil
	task.BuildVerification = nil
	task.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// IgnoreBlockedTask lets PM intentionally accept a blocked agent task as skipped
// without weakening artifact or execution validation rules.
func (s *Service) IgnoreBlockedTask(ctx context.Context, projectID, taskID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if task.Status != TaskBlocked {
		return Board{}, fmt.Errorf("only a blocked task can be ignored")
	}
	now := s.now()
	reason := strings.TrimSpace(task.BlockedReason)
	if reason == "" {
		reason = "Blocked task was ignored by PM."
	}
	task.Status = TaskCompleted
	task.Column = ColumnDone
	task.BlockedReason = ""
	task.Execution = &TaskExecution{
		Verdict:        "ignored",
		Summary:        "PM ignored this blocked task and accepted the workflow risk.",
		ChangedFiles:   []string{},
		Verification:   []string{"No additional agent execution was run after PM ignored the blocked task."},
		RemainingRisks: []string{reason},
		CompletedAt:    now,
	}
	task.BuildVerification = nil
	task.UpdatedAt = now
	if board.ActiveTaskID == task.ID {
		board.ActiveTaskID = ""
	}
	markSequenceTaskIgnored(&board, *task, now)
	if task.SequenceOrder <= 0 {
		completeBacklogForFinishedPlan(&board, task.PlanID, now)
	}
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) routeTask(ctx context.Context, projectID, taskID string, target Column, restart bool) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !restart && !target.Valid() {
		return Board{}, fmt.Errorf("unknown board column")
	}
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if restart {
		if task.Status != TaskBlocked {
			return Board{}, fmt.Errorf("only a blocked task can be restarted")
		}
		target = task.Role.QueueColumn()
	}
	planIndex := findPlan(board, task.PlanID)
	if planIndex < 0 || board.Plans[planIndex].Status != PlanningApproved {
		return Board{}, fmt.Errorf("approve the Team Lead plan before queueing its tasks")
	}
	sequence := board.Plans[planIndex].Sequence
	if task.SequenceOrder > 0 && sequence != nil && sequence.Status != PlanSequenceCompleted {
		if task.Status != TaskBlocked || target != task.Role.QueueColumn() {
			return Board{}, fmt.Errorf("tasks in an active plan sequence are routed automatically")
		}
	}
	if task.Status == TaskInProgress {
		return Board{}, fmt.Errorf("an active task cannot be moved by PM")
	}
	if task.Status == TaskDesignReview {
		return Board{}, fmt.Errorf("approve the Designer handoff before routing this task")
	}
	if task.Status == TaskCompleted {
		return Board{}, fmt.Errorf("completed tasks are controlled by the assigned agent")
	}
	if target == ColumnDone {
		return Board{}, fmt.Errorf("only the assigned agent can complete a task")
	}
	if target != ColumnPlanning && target != task.Role.QueueColumn() {
		return Board{}, fmt.Errorf("%s tasks can only be moved to the %s queue", task.Role.Label(), task.Role.Label())
	}
	now := s.now()
	task.Column = target
	if target == ColumnPlanning {
		task.Status = TaskPlanned
		task.BlockedReason = ""
		task.Execution = nil
		task.BuildVerification = nil
	} else {
		task.Status = TaskQueued
		task.BlockedReason = ""
		task.Execution = nil
		task.BuildVerification = nil
	}
	if restart {
		task.ThreadID = ""
		forgetSequenceRoleThread(&board, *task)
	}
	if task.SequenceOrder > 0 && sequence != nil {
		sequence.Status = PlanSequenceScheduled
		sequence.BlockedReason = ""
		sequence.CompletedAt = nil
	}
	task.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) UpdateTaskStatus(ctx context.Context, projectID, taskID string, status TaskStatus, blockedReason string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if status != TaskInProgress && status != TaskVerifying && status != TaskBlocked && status != TaskCompleted {
		return Board{}, fmt.Errorf("agent status must be in_progress, verifying, blocked, or completed")
	}
	if status == TaskInProgress {
		if task.Status != TaskQueued && task.Status != TaskBlocked {
			return Board{}, fmt.Errorf("only a queued or blocked task can start")
		}
		if task.Column != task.Role.QueueColumn() {
			return Board{}, fmt.Errorf("task must be in the %s queue before it can start", task.Role.Label())
		}
		for _, dependencyID := range task.DependencyIDs {
			dependencyIndex := findTask(board, dependencyID)
			if dependencyIndex < 0 {
				return Board{}, fmt.Errorf("task dependency %s was not found", dependencyID)
			}
			if board.Tasks[dependencyIndex].Status != TaskCompleted {
				return Board{}, fmt.Errorf("task is waiting for dependency %s", board.Tasks[dependencyIndex].Key)
			}
		}
		boards, listErr := s.repository.List(ctx)
		if listErr != nil {
			return Board{}, listErr
		}
		for _, candidate := range boards {
			if candidate.ActiveTaskID != "" && candidate.ActiveTaskID != task.ID {
				return Board{}, fmt.Errorf("another task is already in progress on project %s", candidate.ProjectName)
			}
		}
		if board.ActiveTaskID != "" && board.ActiveTaskID != task.ID {
			return Board{}, fmt.Errorf("another task is already in progress")
		}
		board.ActiveTaskID = task.ID
		task.BlockedReason = ""
		task.BuildVerification = nil
		markSequenceRunning(&board, task.PlanID, task.SequenceOrder, s.now())
	}
	if status == TaskBlocked {
		if task.Status != TaskQueued && task.Status != TaskInProgress && task.Status != TaskVerifying {
			return Board{}, fmt.Errorf("only a queued or active task can be blocked")
		}
		blockedReason = strings.TrimSpace(blockedReason)
		if len(blockedReason) < 5 {
			return Board{}, fmt.Errorf("blocked tasks require a reason")
		}
		if board.ActiveTaskID == task.ID {
			board.ActiveTaskID = ""
		}
		task.BlockedReason = blockedReason
		markSequenceBlocked(&board, task.PlanID, task.SequenceOrder, task.Key+": "+blockedReason)
	}
	if status == TaskCompleted {
		if task.Status != TaskInProgress && task.Status != TaskVerifying {
			return Board{}, fmt.Errorf("only an active task can be completed")
		}
		board.ActiveTaskID = ""
		task.Column = ColumnDone
		task.BlockedReason = ""
	}
	now := s.now()
	task.Status = status
	task.UpdatedAt = now
	if status == TaskCompleted {
		if task.SequenceOrder > 0 && task.Role == RoleQA {
			markSequenceTaskCompleted(&board, *task, now)
		} else {
			completeBacklogForFinishedPlan(&board, task.PlanID, now)
		}
	}
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// SetTaskGitReference sets or clears a task's git evidence (branch/commit).
// Pass a nil reference to clear an existing association. This never runs a
// git command itself; callers are responsible for validating any commit SHA
// or branch name before persisting it here.
func (s *Service) SetTaskGitReference(ctx context.Context, projectID, taskID string, reference *TaskGitReference) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	task.GitReference = reference
	now := s.now()
	task.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// SetTasksGitReference atomically links one verified commit to every task in a
// Git delivery. Either the complete ticket set is persisted or the board is
// left unchanged.
func (s *Service) SetTasksGitReference(ctx context.Context, projectID string, taskIDs []string, reference TaskGitReference) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	if len(taskIDs) == 0 {
		return Board{}, fmt.Errorf("at least one task is required")
	}
	indexes := make([]int, 0, len(taskIDs))
	seen := make(map[string]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		if _, exists := seen[taskID]; exists {
			continue
		}
		seen[taskID] = struct{}{}
		index := findTask(board, taskID)
		if index < 0 {
			return Board{}, fmt.Errorf("task %s not found", taskID)
		}
		indexes = append(indexes, index)
	}
	now := s.now()
	for _, index := range indexes {
		copy := reference
		board.Tasks[index].GitReference = &copy
		board.Tasks[index].UpdatedAt = now
	}
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (s *Service) StartBuildVerification(ctx context.Context, projectID, taskID string, verification BuildVerification) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if (task.Status != TaskInProgress && task.Status != TaskVerifying) || board.ActiveTaskID != task.ID {
		return Board{}, fmt.Errorf("only the active implementation can start build verification")
	}
	now := s.now()
	task.Status = TaskVerifying
	task.BuildVerification = &verification
	task.UpdatedAt = now
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// CompleteTask records the assigned agent's delivery report and atomically
// releases the active slot. Manual Designer handoffs wait for PM approval.
func (s *Service) CompleteTask(ctx context.Context, projectID, taskID, threadID string, execution TaskExecution) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if (task.Status != TaskInProgress && task.Status != TaskVerifying) || board.ActiveTaskID != task.ID {
		return Board{}, fmt.Errorf("only the active task can be completed")
	}
	now := s.now()
	execution.CompletedAt = now
	task.ThreadID = strings.TrimSpace(threadID)
	rememberSequenceRoleThread(&board, *task, task.ThreadID)
	task.Execution = &execution
	if execution.BuildVerification != nil {
		task.BuildVerification = execution.BuildVerification
	}
	if task.Role == RoleDesigner && task.SequenceOrder <= 0 {
		task.Status = TaskDesignReview
		task.Column = ColumnDesigner
	} else {
		task.Status = TaskCompleted
		task.Column = ColumnDone
	}
	task.BlockedReason = ""
	task.UpdatedAt = now
	if task.SequenceOrder > 0 && task.Role == RoleQA {
		markSequenceTaskCompleted(&board, *task, now)
	} else {
		completeBacklogForFinishedPlan(&board, task.PlanID, now)
	}
	board.ActiveTaskID = ""
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// BlockTaskWithExecution persists a QA report while keeping the task available
// for an explicit PM re-queue after discovered bugs are fixed.
func (s *Service) BlockTaskWithExecution(ctx context.Context, projectID, taskID, threadID, reason string, execution TaskExecution) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err := s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	taskIndex := findTask(board, taskID)
	if taskIndex < 0 {
		return Board{}, fmt.Errorf("task not found")
	}
	task := &board.Tasks[taskIndex]
	if (task.Status != TaskInProgress && task.Status != TaskVerifying) || board.ActiveTaskID != task.ID {
		return Board{}, fmt.Errorf("only the active task can be blocked with an execution report")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 {
		return Board{}, fmt.Errorf("blocked tasks require a reason")
	}
	now := s.now()
	execution.CompletedAt = now
	task.ThreadID = strings.TrimSpace(threadID)
	rememberSequenceRoleThread(&board, *task, task.ThreadID)
	task.Execution = &execution
	if execution.BuildVerification != nil {
		task.BuildVerification = execution.BuildVerification
	}
	task.Status = TaskBlocked
	task.BlockedReason = reason
	task.UpdatedAt = now
	markSequenceBlocked(&board, task.PlanID, task.SequenceOrder, task.Key+": "+reason)
	board.ActiveTaskID = ""
	board.UpdatedAt = now
	if err := s.repository.Update(ctx, board); err != nil {
		return Board{}, err
	}
	return board, nil
}

// BlockTask records a resumable Codex thread when execution fails.
func (s *Service) BlockTask(ctx context.Context, projectID, taskID, threadID, reason string) (Board, error) {
	board, err := s.UpdateTaskStatus(ctx, projectID, taskID, TaskBlocked, reason)
	if err != nil || strings.TrimSpace(threadID) == "" {
		return board, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	board, err = s.repository.FindByProject(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	if index := findTask(board, taskID); index >= 0 {
		board.Tasks[index].ThreadID = strings.TrimSpace(threadID)
		rememberSequenceRoleThread(&board, board.Tasks[index], board.Tasks[index].ThreadID)
		if err := s.repository.Update(ctx, board); err != nil {
			return Board{}, err
		}
	}
	return board, nil
}

func (s *Service) RecoverInterruptedState(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	boards, err := s.repository.List(ctx)
	if err != nil {
		return err
	}
	for _, board := range boards {
		changed := false
		now := s.now()
		for index := range board.Plans {
			if board.Plans[index].Status == PlanningAnalyzing {
				board.Plans[index].Status = PlanningFailed
				board.Plans[index].Error = "Planning was interrupted when the local service stopped. Retry this planning item to continue."
				board.Plans[index].UpdatedAt = now
				changed = true
			}
		}
		if board.ActiveTaskID != "" {
			if taskIndex := findTask(board, board.ActiveTaskID); taskIndex >= 0 && (board.Tasks[taskIndex].Status == TaskInProgress || board.Tasks[taskIndex].Status == TaskVerifying) {
				board.Tasks[taskIndex].Status = TaskBlocked
				board.Tasks[taskIndex].BlockedReason = "Execution or build verification was interrupted when the local service stopped. Re-queue or resume this task explicitly."
				board.Tasks[taskIndex].UpdatedAt = now
				markSequenceBlocked(&board, board.Tasks[taskIndex].PlanID, board.Tasks[taskIndex].SequenceOrder, board.Tasks[taskIndex].Key+": "+board.Tasks[taskIndex].BlockedReason)
				changed = true
			}
			board.ActiveTaskID = ""
		}
		if changed {
			board.UpdatedAt = now
			if err := s.repository.Update(ctx, board); err != nil {
				return err
			}
		}
	}
	return nil
}

func markSequenceRunning(board *Board, planID string, sequenceOrder int, now time.Time) {
	if sequenceOrder <= 0 {
		return
	}
	planIndex := findPlan(*board, planID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil {
		return
	}
	sequence := board.Plans[planIndex].Sequence
	sequence.Status = PlanSequenceRunning
	sequence.BlockedReason = ""
	if sequence.StartedAt == nil {
		startedAt := now
		sequence.StartedAt = &startedAt
	}
}

func rememberSequenceRoleThread(board *Board, task Task, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if task.SequenceOrder <= 0 || threadID == "" {
		return
	}
	planIndex := findPlan(*board, task.PlanID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil {
		return
	}
	sequence := board.Plans[planIndex].Sequence
	if sequence.RoleThreadIDs == nil {
		sequence.RoleThreadIDs = make(map[AgentRole]string)
	}
	sequence.RoleThreadIDs[task.Role] = threadID
}

func forgetSequenceRoleThread(board *Board, task Task) {
	if task.SequenceOrder <= 0 {
		return
	}
	planIndex := findPlan(*board, task.PlanID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil || board.Plans[planIndex].Sequence.RoleThreadIDs == nil {
		return
	}
	delete(board.Plans[planIndex].Sequence.RoleThreadIDs, task.Role)
	if len(board.Plans[planIndex].Sequence.RoleThreadIDs) == 0 {
		board.Plans[planIndex].Sequence.RoleThreadIDs = nil
	}
}

func markSequenceBlocked(board *Board, planID string, sequenceOrder int, reason string) {
	if sequenceOrder <= 0 {
		return
	}
	planIndex := findPlan(*board, planID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil {
		return
	}
	sequence := board.Plans[planIndex].Sequence
	sequence.Status = PlanSequenceBlocked
	sequence.BlockedReason = strings.TrimSpace(reason)
}

func markSequenceTaskCompleted(board *Board, task Task, now time.Time) {
	if task.SequenceOrder <= 0 || task.Role != RoleQA {
		return
	}
	planIndex := findPlan(*board, task.PlanID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil {
		return
	}
	sequence := board.Plans[planIndex].Sequence
	sequence.Status = PlanSequenceCompleted
	sequence.BlockedReason = ""
	completedAt := now
	sequence.CompletedAt = &completedAt
	completedItem := completeBacklogForPlan(board, task.PlanID, now)
	requeueResolvedQATaskIfReady(board, completedItem, now)
}

func markSequenceTaskIgnored(board *Board, task Task, now time.Time) {
	if task.SequenceOrder <= 0 {
		return
	}
	planIndex := findPlan(*board, task.PlanID)
	if planIndex < 0 || board.Plans[planIndex].Sequence == nil {
		return
	}
	if task.Role == RoleQA {
		markSequenceTaskCompleted(board, task, now)
		return
	}
	sequence := board.Plans[planIndex].Sequence
	sequence.Status = PlanSequenceScheduled
	sequence.BlockedReason = ""
	sequence.CompletedAt = nil
	if sequence.ScheduledAt.IsZero() {
		sequence.ScheduledAt = now
	}
}

func completeBacklogForPlan(board *Board, planID string, now time.Time) *BacklogItem {
	return markBacklogForPlan(board, planID, BacklogDone, now)
}

func completeBacklogForFinishedPlan(board *Board, planID string, now time.Time) *BacklogItem {
	if !planTasksCompleted(*board, planID) {
		return nil
	}
	return completeBacklogForPlan(board, planID, now)
}

func markFinishedBacklogItems(board *Board, now time.Time) bool {
	changed := false
	for index := range board.Backlog {
		item := &board.Backlog[index]
		if item.Status != BacklogPlanning || !planTasksCompleted(*board, item.PlanID) {
			continue
		}
		item.Status = BacklogDone
		item.UpdatedAt = now
		changed = true
	}
	if changed {
		board.UpdatedAt = now
	}
	return changed
}

func planTasksCompleted(board Board, planID string) bool {
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
		if task.Status != TaskCompleted {
			return false
		}
	}
	return found
}

func markBacklogForPlan(board *Board, planID string, status BacklogStatus, now time.Time) *BacklogItem {
	for index := range board.Backlog {
		item := &board.Backlog[index]
		if item.PlanID != planID || item.Status == status {
			continue
		}
		item.Status = status
		item.UpdatedAt = now
		return item
	}
	return nil
}

func requeueResolvedQATaskIfReady(board *Board, completedItem *BacklogItem, now time.Time) {
	if completedItem == nil || completedItem.Source != "qa" || completedItem.Type != BacklogBug {
		return
	}
	qaTaskIDs := qaBugWaitingTaskIDs(*completedItem)
	qaTaskIDs = appendUnique(qaTaskIDs, inferredWaitingQATaskIDs(*board, *completedItem)...)
	for _, qaTaskID := range qaTaskIDs {
		requeueResolvedQATaskIfNoOpenBugs(board, completedItem.ID, qaTaskID, now)
	}
}

func requeueResolvedQATaskIfNoOpenBugs(board *Board, completedItemID, qaTaskID string, now time.Time) {
	qaTaskID = strings.TrimSpace(qaTaskID)
	if qaTaskID == "" {
		return
	}
	for _, item := range board.Backlog {
		if item.ID == completedItemID || item.Source != "qa" || item.Type != BacklogBug || item.Status == BacklogDone {
			continue
		}
		if qaBugReferencesTask(item, qaTaskID) || qaTaskFindingMatchesBug(*board, qaTaskID, item) {
			return
		}
	}
	taskIndex := findTask(*board, qaTaskID)
	if taskIndex < 0 {
		return
	}
	task := &board.Tasks[taskIndex]
	if task.Role != RoleQA || task.Status != TaskBlocked {
		return
	}
	requeueBlockedQATask(board, taskIndex, now)
}

func requeueBlockedQATask(board *Board, taskIndex int, now time.Time) bool {
	if taskIndex < 0 || taskIndex >= len(board.Tasks) {
		return false
	}
	task := &board.Tasks[taskIndex]
	if task.Role != RoleQA || task.Status != TaskBlocked {
		return false
	}
	task.Column = ColumnQA
	task.Status = TaskQueued
	task.BlockedReason = ""
	task.Execution = nil
	task.BuildVerification = nil
	task.UpdatedAt = now
	if planIndex := findPlan(*board, task.PlanID); planIndex >= 0 && board.Plans[planIndex].Sequence != nil {
		sequence := board.Plans[planIndex].Sequence
		sequence.Status = PlanSequenceScheduled
		sequence.BlockedReason = ""
		sequence.CompletedAt = nil
		if sequence.ScheduledAt.IsZero() {
			sequence.ScheduledAt = now
		}
	}
	return true
}

func requeueResolvedWaitingQATasks(board *Board, now time.Time) bool {
	changed := false
	for taskIndex := range board.Tasks {
		task := board.Tasks[taskIndex]
		if task.Role != RoleQA || task.Status != TaskBlocked || !isQATaskWaitingOnPlanningBugs(task) || !taskHasQAFindings(task) {
			continue
		}
		waitingOnActiveBug := false
		for _, item := range board.Backlog {
			if item.Source != "qa" || item.Type != BacklogBug || item.Status == BacklogDone {
				continue
			}
			if qaBugReferencesTask(item, task.ID) || qaTaskFindingMatchesBug(*board, task.ID, item) {
				waitingOnActiveBug = true
				break
			}
		}
		if !waitingOnActiveBug && requeueBlockedQATask(board, taskIndex, now) {
			changed = true
		}
	}
	return changed
}

func taskHasQAFindings(task Task) bool {
	return task.Execution != nil && len(task.Execution.Findings) > 0
}

func inferredWaitingQATaskIDs(board Board, completedItem BacklogItem) []string {
	if normalizedBacklogBugTitle(completedItem.Title) == "" {
		return nil
	}
	ids := make([]string, 0)
	for _, task := range board.Tasks {
		if task.Role != RoleQA || task.Status != TaskBlocked || !isQATaskWaitingOnPlanningBugs(task) {
			continue
		}
		if qaTaskFindingMatchesBug(board, task.ID, completedItem) {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func isQATaskWaitingOnPlanningBugs(task Task) bool {
	reason := strings.ToLower(strings.TrimSpace(task.BlockedReason))
	return strings.Contains(reason, "team lead planning") &&
		(strings.Contains(reason, "waiting on") || strings.Contains(reason, "matched"))
}

func qaTaskFindingMatchesBug(board Board, qaTaskID string, item BacklogItem) bool {
	targetTitle := normalizedBacklogBugTitle(item.Title)
	if targetTitle == "" {
		return false
	}
	taskIndex := findTask(board, qaTaskID)
	if taskIndex < 0 {
		return false
	}
	task := board.Tasks[taskIndex]
	if task.Execution == nil {
		return false
	}
	for _, finding := range task.Execution.Findings {
		if normalizedBacklogBugTitle(finding.Title) == targetTitle {
			return true
		}
	}
	return false
}

func qaBugReferencesTask(item BacklogItem, qaTaskID string) bool {
	for _, candidate := range qaBugWaitingTaskIDs(item) {
		if candidate == strings.TrimSpace(qaTaskID) {
			return true
		}
	}
	return false
}

func qaBugWaitingTaskIDs(item BacklogItem) []string {
	ids := make([]string, 0, 1+len(item.WaitingQATaskIDs))
	if qaTaskID, ok := qaBugSourceTaskID(item.SourceReference); ok {
		ids = append(ids, qaTaskID)
	}
	ids = appendUnique(ids, compactStrings(item.WaitingQATaskIDs)...)
	return compactStrings(ids)
}

func normalizedBacklogBugTitle(value string) string {
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

func qaBugSourceTaskID(reference string) (string, bool) {
	taskID, suffix, found := strings.Cut(strings.TrimSpace(reference), ":bug:")
	if !found || strings.TrimSpace(taskID) == "" || strings.TrimSpace(suffix) == "" {
		return "", false
	}
	return strings.TrimSpace(taskID), true
}

func findPlan(board Board, planID string) int {
	for index := range board.Plans {
		if board.Plans[index].ID == planID {
			return index
		}
	}
	return -1
}

func findTask(board Board, taskID string) int {
	for index := range board.Tasks {
		if board.Tasks[index].ID == taskID {
			return index
		}
	}
	return -1
}

func findBacklogItem(board Board, itemID string) int {
	for index := range board.Backlog {
		if board.Backlog[index].ID == itemID {
			return index
		}
	}
	return -1
}

func backlogKey(itemType BacklogItemType, items []BacklogItem) string {
	prefix := map[BacklogItemType]string{BacklogFeature: "FEAT", BacklogBug: "BUG", BacklogTodo: "TODO"}[itemType]
	maxSequence := 0
	for _, item := range items {
		if item.Type != itemType {
			continue
		}
		value, found := strings.CutPrefix(strings.TrimSpace(item.Key), prefix+"-")
		if !found {
			continue
		}
		sequence, err := strconv.Atoi(value)
		if err == nil && sequence > maxSequence {
			maxSequence = sequence
		}
	}
	return fmt.Sprintf("%s-%03d", prefix, maxSequence+1)
}

func normalizedDeliveryTarget(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "frontend", "backend", "fullstack":
		return value
	default:
		return "fullstack"
	}
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func removePlanTasks(tasks []Task, planID string) []Task {
	result := tasks[:0]
	for _, task := range tasks {
		if task.PlanID != planID {
			result = append(result, task)
		}
	}
	return result
}

func validAudience(audience []AgentRole) []AgentRole {
	result := make([]AgentRole, 0, len(audience))
	seen := map[AgentRole]bool{}
	for _, role := range audience {
		if role.Valid() && !seen[role] {
			seen[role] = true
			result = append(result, role)
		}
	}
	return result
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	for _, value := range values {
		seen[strings.ToLower(strings.TrimSpace(value))] = true
	}
	for _, value := range additions {
		key := strings.ToLower(strings.TrimSpace(value))
		if !seen[key] {
			seen[key] = true
			values = append(values, value)
		}
	}
	return values
}

func defaultReviewer(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "Product manager"
}

func validateDependencyGraph(tasks []Task) error {
	dependencies := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		dependencies[task.ID] = task.DependencyIDs
	}
	visiting := make(map[string]bool, len(tasks))
	visited := make(map[string]bool, len(tasks))
	var visit func(string) bool
	visit = func(taskID string) bool {
		if visiting[taskID] {
			return false
		}
		if visited[taskID] {
			return true
		}
		visiting[taskID] = true
		for _, dependencyID := range dependencies[taskID] {
			if !visit(dependencyID) {
				return false
			}
		}
		visiting[taskID] = false
		visited[taskID] = true
		return true
	}
	for _, task := range tasks {
		if !visit(task.ID) {
			return fmt.Errorf("task dependencies contain a cycle")
		}
	}
	return nil
}

func taskKey(role AgentRole, sequence int) string {
	prefix := map[AgentRole]string{RoleDesigner: "DSN", RoleDeveloper: "DEV", RoleQA: "QA"}[role]
	return fmt.Sprintf("%s-%03d", prefix, sequence)
}

func newID(prefix string, timestamp time.Time) string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%s-%x-%x", prefix, timestamp.UnixNano(), fallbackIDSequence.Add(1))
}

func cloneRequestAttachments(values []RequestAttachment) []RequestAttachment {
	if len(values) == 0 {
		return []RequestAttachment{}
	}
	return append([]RequestAttachment{}, values...)
}

func cloneTaskExecution(value TaskExecution) TaskExecution {
	value.ChangedFiles = append([]string{}, value.ChangedFiles...)
	value.Verification = append([]string{}, value.Verification...)
	value.RemainingRisks = append([]string{}, value.RemainingRisks...)
	if len(value.Findings) > 0 {
		findings := make([]TaskFinding, 0, len(value.Findings))
		for _, finding := range value.Findings {
			finding.Steps = append([]string{}, finding.Steps...)
			finding.AffectedFiles = append([]string{}, finding.AffectedFiles...)
			findings = append(findings, finding)
		}
		value.Findings = findings
	}
	if len(value.Artifacts) > 0 {
		value.Artifacts = append([]DesignArtifact{}, value.Artifacts...)
	}
	if value.BuildVerification != nil {
		verification := *value.BuildVerification
		value.BuildVerification = &verification
	}
	return value
}

func nextTaskRevision(task Task) int {
	return len(task.RevisionHistory) + 1
}

func newSequenceID(prefix string, timestamp time.Time, sequence int) string {
	return fmt.Sprintf("%s-%02d", newID(prefix, timestamp), sequence)
}
