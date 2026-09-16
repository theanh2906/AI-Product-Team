package web

import (
	"context"
	"sort"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

type queuedAgentTask struct {
	projectID           string
	taskID              string
	role                kanban.AgentRole
	priority            kanban.Priority
	queuedAt            time.Time
	sequenceOrder       int
	sequenceScheduledAt time.Time
}

func (s *server) scheduleQueuedAgentTasks(reason, traceID string) {
	s.taskQueueMu.Lock()
	if s.taskQueueRunning {
		s.taskQueueRequested = true
		s.taskQueueMu.Unlock()
		return
	}
	s.taskQueueRunning = true
	s.taskQueueRequested = false
	s.taskQueueMu.Unlock()

	go s.runQueuedAgentTasks(reason, traceID)
}

func (s *server) runQueuedAgentTasks(reason, traceID string) {
	for {
		s.drainQueuedAgentTasks(reason, traceID)

		s.taskQueueMu.Lock()
		if s.taskQueueRequested {
			s.taskQueueRequested = false
			s.taskQueueMu.Unlock()
			continue
		}
		s.taskQueueRunning = false
		s.taskQueueMu.Unlock()
		return
	}
}

func (s *server) drainQueuedAgentTasks(reason, traceID string) {
	for {
		task, ok := s.nextQueuedAgentTask()
		if !ok {
			return
		}
		taskTraceID := traceID
		if taskTraceID == "" {
			taskTraceID = "queue-" + task.taskID
		}
		s.observability.Record(observability.Event{
			Category: "task", Name: "task.queue.dispatched", Message: "Queued task dispatched to the next available agent",
			CorrelationID: taskTraceID, ProjectID: task.projectID, EntityType: "task", EntityID: task.taskID,
			Agent: string(task.role), Stage: "queue", Outcome: "dispatched", Attributes: map[string]any{"reason": reason},
		})
		switch task.role {
		case kanban.RoleDesigner:
			s.runDesignerTask(task.projectID, task.taskID, taskTraceID)
		case kanban.RoleDeveloper:
			s.runDeveloperTask(task.projectID, task.taskID, taskTraceID)
		case kanban.RoleQA:
			s.runQATask(task.projectID, task.taskID, taskTraceID)
		default:
			return
		}
	}
}

func (s *server) nextQueuedAgentTask() (queuedAgentTask, bool) {
	boards, err := s.boards.ListBoards(context.Background())
	if err != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "task", Name: "task.queue.inspect_failed",
			Message: "Could not inspect queued agent tasks", Stage: "queue", Outcome: "failed",
			Attributes: map[string]any{"error": err.Error()},
		})
		return queuedAgentTask{}, false
	}
	for _, board := range boards {
		if board.ActiveTaskID != "" {
			return queuedAgentTask{}, false
		}
	}
	if pausedBoards := s.pauseStoppingQueues(); len(pausedBoards) > 0 {
		refreshedBoards, listErr := s.boards.ListBoards(context.Background())
		if listErr != nil {
			s.observability.Record(observability.Event{
				Level: observability.LevelError, Category: "task", Name: "task.queue.inspect_failed",
				Message: "Could not inspect queued agent tasks after pausing queues", Stage: "queue", Outcome: "failed",
				Attributes: map[string]any{"error": listErr.Error()},
			})
			return queuedAgentTask{}, false
		}
		boards = refreshedBoards
	}

	sequenceCandidates := make([]queuedAgentTask, 0)
	manualCandidates := make([]queuedAgentTask, 0)
	for _, board := range boards {
		if boardQueueDispatchPaused(board) {
			continue
		}
		for _, plan := range board.Plans {
			if plan.Sequence == nil || (plan.Sequence.Status != kanban.PlanSequenceScheduled && plan.Sequence.Status != kanban.PlanSequenceRunning) {
				continue
			}
			var next *kanban.Task
			for index := range board.Tasks {
				task := &board.Tasks[index]
				if task.PlanID != plan.ID || task.SequenceOrder <= 0 || task.Status == kanban.TaskCompleted {
					continue
				}
				if next == nil || task.SequenceOrder < next.SequenceOrder {
					next = task
				}
			}
			if next == nil || next.Status != kanban.TaskQueued || next.Column != next.Role.QueueColumn() || !taskDependenciesCompleted(board, *next) || !s.taskWorkerAvailable(next.Role) {
				continue
			}
			sequenceCandidates = append(sequenceCandidates, queuedAgentTask{
				projectID: board.ProjectID, taskID: next.ID, role: next.Role, priority: next.Priority,
				queuedAt: next.UpdatedAt, sequenceOrder: next.SequenceOrder, sequenceScheduledAt: plan.Sequence.ScheduledAt,
			})
		}
		for _, task := range board.Tasks {
			if task.SequenceOrder > 0 || task.Status != kanban.TaskQueued || task.Column != task.Role.QueueColumn() {
				continue
			}
			if !taskDependenciesCompleted(board, task) {
				continue
			}
			if !s.taskWorkerAvailable(task.Role) {
				continue
			}
			manualCandidates = append(manualCandidates, queuedAgentTask{
				projectID: board.ProjectID,
				taskID:    task.ID,
				role:      task.Role,
				priority:  task.Priority,
				queuedAt:  task.UpdatedAt,
			})
		}
	}
	if len(sequenceCandidates) > 0 {
		sort.SliceStable(sequenceCandidates, func(left, right int) bool {
			if !sequenceCandidates[left].sequenceScheduledAt.Equal(sequenceCandidates[right].sequenceScheduledAt) {
				return sequenceCandidates[left].sequenceScheduledAt.Before(sequenceCandidates[right].sequenceScheduledAt)
			}
			if sequenceCandidates[left].projectID != sequenceCandidates[right].projectID {
				return sequenceCandidates[left].projectID < sequenceCandidates[right].projectID
			}
			return sequenceCandidates[left].taskID < sequenceCandidates[right].taskID
		})
		return sequenceCandidates[0], true
	}
	if len(manualCandidates) == 0 {
		return queuedAgentTask{}, false
	}
	sort.SliceStable(manualCandidates, func(left, right int) bool {
		leftRank := agentQueuePriorityRank(manualCandidates[left].priority)
		rightRank := agentQueuePriorityRank(manualCandidates[right].priority)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if !manualCandidates[left].queuedAt.Equal(manualCandidates[right].queuedAt) {
			return manualCandidates[left].queuedAt.Before(manualCandidates[right].queuedAt)
		}
		return manualCandidates[left].taskID < manualCandidates[right].taskID
	})
	return manualCandidates[0], true
}

func (s *server) pauseStoppingQueues() []kanban.Board {
	boards, err := s.boards.PauseStoppingQueues(context.Background())
	if err != nil {
		s.observability.Record(observability.Event{
			Level: observability.LevelError, Category: "task", Name: "task.queue.pause_failed",
			Message: "Could not pause a queue after the active task finished", Stage: "queue", Outcome: "failed",
			Attributes: map[string]any{"error": err.Error()},
		})
		return nil
	}
	for _, board := range boards {
		if s.boardEvents != nil {
			s.boardEvents.publish(board)
		}
		if s.observability != nil {
			s.observability.Record(observability.Event{
				Category: "task", Name: "task.queue.paused", Message: "Queue paused after the active task finished",
				ProjectID: board.ProjectID, EntityType: "board", EntityID: board.ID, Stage: "queue", Outcome: "paused",
			})
		}
	}
	return boards
}

func boardQueueDispatchPaused(board kanban.Board) bool {
	return board.QueueControl != nil && (board.QueueControl.Status == kanban.QueueControlPaused || board.QueueControl.Status == kanban.QueueControlStopping)
}

func (s *server) taskWorkerAvailable(role kanban.AgentRole) bool {
	switch role {
	case kanban.RoleDesigner:
		return s.designer != nil
	case kanban.RoleDeveloper:
		return s.developer != nil
	case kanban.RoleQA:
		return s.qa != nil
	default:
		return false
	}
}

func taskDependenciesCompleted(board kanban.Board, task kanban.Task) bool {
	for _, dependencyID := range task.DependencyIDs {
		found := false
		for _, dependency := range board.Tasks {
			if dependency.ID != dependencyID {
				continue
			}
			found = true
			if dependency.Status != kanban.TaskCompleted {
				return false
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func agentQueuePriorityRank(priority kanban.Priority) int {
	switch priority {
	case kanban.PriorityCritical:
		return 0
	case kanban.PriorityHigh:
		return 1
	case kanban.PriorityMedium:
		return 2
	case kanban.PriorityLow:
		return 3
	default:
		return 4
	}
}

func isAgentQueueColumn(column kanban.Column) bool {
	return column == kanban.ColumnDesigner || column == kanban.ColumnDeveloper || column == kanban.ColumnQA
}
