package web

import (
	"context"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/design"
	"github.com/theanh2906/AI-Product-Team/internal/development"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

type noopDeveloper struct{}

func (noopDeveloper) Implement(context.Context, development.Request) (development.Result, error) {
	return development.Result{}, nil
}

func (noopDeveloper) RepairBuild(context.Context, development.BuildRepairRequest) (development.Result, error) {
	return development.Result{}, nil
}

func (noopDeveloper) RepairGitDelivery(context.Context, development.GitRepairRequest) (development.Result, error) {
	return development.Result{}, nil
}

type noopDesigner struct{}

func (noopDesigner) Design(context.Context, design.Request) (design.Result, error) {
	return design.Result{}, nil
}

func TestNextQueuedAgentTaskUsesPriorityThenQueueTime(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce",
		Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningApproved}},
		Tasks: []kanban.Task{
			{ID: "task-low", PlanID: "plan-1", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityLow, UpdatedAt: now.Add(-2 * time.Minute)},
			{ID: "task-high", PlanID: "plan-1", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityHigh, UpdatedAt: now},
			{ID: "task-older-high", PlanID: "plan-1", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityHigh, UpdatedAt: now.Add(-time.Minute)},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	next, ok := server.nextQueuedAgentTask()
	if !ok {
		t.Fatal("expected a queued task")
	}
	if next.taskID != "task-older-high" {
		t.Fatalf("expected highest visible priority and oldest queue time first, got %s", next.taskID)
	}
}

func TestNextQueuedAgentTaskSkipsTaskWithIncompleteDependencies(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce",
		Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningApproved}},
		Tasks: []kanban.Task{
			{ID: "task-first", PlanID: "plan-1", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityLow, UpdatedAt: now},
			{ID: "task-dependent", PlanID: "plan-1", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityCritical, DependencyIDs: []string{"task-first"}, UpdatedAt: now.Add(-time.Minute)},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	next, ok := server.nextQueuedAgentTask()
	if !ok || next.taskID != "task-first" {
		t.Fatalf("expected runnable prerequisite task, got %+v, ok=%v", next, ok)
	}
}

func TestNextQueuedAgentTaskPrioritizesOldestPlanSequenceAndItsOrder(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-sequence", ProjectID: "project-sequence", ProjectName: "Sequence",
		Plans: []kanban.Plan{{
			ID: "plan-sequence", Status: kanban.PlanningApproved,
			Sequence: &kanban.PlanSequence{Status: kanban.PlanSequenceScheduled, ScheduledAt: now},
		}},
		Tasks: []kanban.Task{
			{ID: "sequence-first", PlanID: "plan-sequence", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityLow, SequenceOrder: 1, UpdatedAt: now},
			{ID: "sequence-second", PlanID: "plan-sequence", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityCritical, SequenceOrder: 2, UpdatedAt: now},
			{ID: "manual-critical", PlanID: "manual-plan", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityCritical, UpdatedAt: now.Add(-time.Hour)},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	next, ok := server.nextQueuedAgentTask()
	if !ok || next.taskID != "sequence-first" {
		t.Fatalf("expected first sequence task ahead of manual priority, got %+v, ok=%v", next, ok)
	}
}

func TestTaskDependenciesCompletedRejectsMissingDependency(t *testing.T) {
	board := kanban.Board{Tasks: []kanban.Task{{ID: "task-1", Status: kanban.TaskCompleted}}}
	task := kanban.Task{ID: "task-2", DependencyIDs: []string{"missing-task"}}
	if taskDependenciesCompleted(board, task) {
		t.Fatal("a missing dependency must not be treated as completed")
	}
}

func TestNextQueuedAgentTaskIncludesDesignerQueue(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce",
		Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningApproved}},
		Tasks: []kanban.Task{
			{ID: "task-designer", PlanID: "plan-1", Role: kanban.RoleDesigner, Column: kanban.ColumnDesigner, Status: kanban.TaskQueued, Priority: kanban.PriorityHigh, UpdatedAt: now},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), designer: noopDesigner{}}

	next, ok := server.nextQueuedAgentTask()
	if !ok {
		t.Fatal("expected a queued Designer task")
	}
	if next.taskID != "task-designer" || next.role != kanban.RoleDesigner {
		t.Fatalf("expected Designer task, got %+v", next)
	}
}

func TestAgentQueueColumnIncludesDesignerDeveloperAndQA(t *testing.T) {
	for _, column := range []kanban.Column{kanban.ColumnDesigner, kanban.ColumnDeveloper, kanban.ColumnQA} {
		if !isAgentQueueColumn(column) {
			t.Fatalf("expected %s to schedule queued agent work", column)
		}
	}
	for _, column := range []kanban.Column{kanban.ColumnBacklog, kanban.ColumnPlanning, kanban.ColumnDone} {
		if isAgentQueueColumn(column) {
			t.Fatalf("expected %s to not schedule queued agent work", column)
		}
	}
}

func TestSequenceBuildVerificationRunsOnlyForFinalDeveloper(t *testing.T) {
	board := kanban.Board{Tasks: []kanban.Task{
		{ID: "dev-1", PlanID: "plan-1", Role: kanban.RoleDeveloper, SequenceOrder: 1},
		{ID: "dev-2", PlanID: "plan-1", Role: kanban.RoleDeveloper, SequenceOrder: 2},
		{ID: "qa-1", PlanID: "plan-1", Role: kanban.RoleQA, SequenceOrder: 3},
	}}
	if developerTaskNeedsBuildVerification(board, board.Tasks[0]) {
		t.Fatal("an earlier sequence Developer task must defer build verification")
	}
	if !developerTaskNeedsBuildVerification(board, board.Tasks[1]) {
		t.Fatal("the final sequence Developer task must run build verification before QA")
	}
	manual := kanban.Task{ID: "manual", Role: kanban.RoleDeveloper}
	if !developerTaskNeedsBuildVerification(board, manual) {
		t.Fatal("manual Developer tasks must retain their existing build verification")
	}
}

func TestNextQueuedAgentTaskWaitsWhenAnyTaskIsActive(t *testing.T) {
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, board := range []kanban.Board{
		{ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce", ActiveTaskID: "active-task", Plans: []kanban.Plan{}, Tasks: []kanban.Task{}},
		{ID: "board-2", ProjectID: "project-2", ProjectName: "Admin", Plans: []kanban.Plan{}, Tasks: []kanban.Task{
			{ID: "queued-task", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityHigh, UpdatedAt: time.Now().UTC()},
		}},
	} {
		if err := repository.Create(context.Background(), board); err != nil {
			t.Fatal(err)
		}
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	if next, ok := server.nextQueuedAgentTask(); ok {
		t.Fatalf("queue should wait while another task is active, got %+v", next)
	}
}

func TestNextQueuedAgentTaskSkipsPausedProjectQueue(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, board := range []kanban.Board{
		{
			ID: "board-paused", ProjectID: "project-paused", ProjectName: "Paused",
			QueueControl: &kanban.QueueControl{Status: kanban.QueueControlPaused, RequestedAt: &now, PausedAt: &now, UpdatedAt: now},
			Plans:        []kanban.Plan{{ID: "plan-paused", Status: kanban.PlanningApproved}},
			Tasks: []kanban.Task{
				{ID: "paused-task", PlanID: "plan-paused", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityCritical, UpdatedAt: now.Add(-time.Hour)},
			},
		},
		{
			ID: "board-running", ProjectID: "project-running", ProjectName: "Running",
			Plans: []kanban.Plan{{ID: "plan-running", Status: kanban.PlanningApproved}},
			Tasks: []kanban.Task{
				{ID: "running-task", PlanID: "plan-running", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityLow, UpdatedAt: now},
			},
		},
	} {
		if err := repository.Create(context.Background(), board); err != nil {
			t.Fatal(err)
		}
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	next, ok := server.nextQueuedAgentTask()
	if !ok || next.taskID != "running-task" {
		t.Fatalf("expected dispatch from the running project only, got %+v, ok=%v", next, ok)
	}
}

func TestNextQueuedAgentTaskPausesStoppingQueueAfterActiveTaskFinishes(t *testing.T) {
	now := time.Now().UTC()
	repository, err := storage.NewJSONBoardRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{
		ID: "board-stopping", ProjectID: "project-stopping", ProjectName: "Stopping",
		QueueControl: &kanban.QueueControl{Status: kanban.QueueControlStopping, RequestedAt: &now, UpdatedAt: now},
		Plans:        []kanban.Plan{{ID: "plan-stopping", Status: kanban.PlanningApproved}},
		Tasks: []kanban.Task{
			{ID: "queued-task", PlanID: "plan-stopping", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Priority: kanban.PriorityHigh, UpdatedAt: now},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	server := &server{boards: kanban.NewService(repository), developer: noopDeveloper{}}

	if next, ok := server.nextQueuedAgentTask(); ok {
		t.Fatalf("stopping queue should pause instead of dispatching, got %+v", next)
	}
	paused, err := repository.FindByProject(context.Background(), "project-stopping")
	if err != nil {
		t.Fatal(err)
	}
	if paused.QueueControl == nil || paused.QueueControl.Status != kanban.QueueControlPaused || paused.QueueControl.PausedAt == nil {
		t.Fatalf("expected stopping queue to become paused after active task finished: %+v", paused.QueueControl)
	}
}
