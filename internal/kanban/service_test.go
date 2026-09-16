package kanban

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMarkBacklogItemReportedAgainPersistsHighlightState(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	now := time.Date(2026, time.August, 22, 2, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	_, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogBug, Source: "qa", SourceReference: "task-1:bug:1",
		Title: "[QA] Existing regression", Description: "A concrete reproducible regression exists.",
	})
	if err != nil {
		t.Fatal(err)
	}

	board, highlighted, err := service.MarkBacklogItemReportedAgain(ctx, "project-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if highlighted.RepeatReports != 1 || highlighted.LastReportedAt == nil || !highlighted.LastReportedAt.Equal(now) {
		t.Fatalf("repeat report state was not persisted: %+v", highlighted)
	}
	if board.Backlog[0].RepeatReports != 1 {
		t.Fatalf("board does not contain highlighted backlog state: %+v", board.Backlog[0])
	}
}

func TestMarkBacklogItemReportedAgainPersistsWaitingQATaskIDs(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)

	_, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogBug, Source: "qa", SourceReference: "original-qa:bug:1",
		Title: "[QA] Existing regression", Description: "A concrete reproducible regression exists.",
	})
	if err != nil {
		t.Fatal(err)
	}

	board, highlighted, err := service.MarkBacklogItemReportedAgain(ctx, "project-1", item.ID, "qa-task")
	if err != nil {
		t.Fatal(err)
	}
	if len(highlighted.WaitingQATaskIDs) != 1 || highlighted.WaitingQATaskIDs[0] != "qa-task" {
		t.Fatalf("waiting QA task IDs were not persisted: %+v", highlighted)
	}
	if len(board.Backlog[0].WaitingQATaskIDs) != 1 || board.Backlog[0].WaitingQATaskIDs[0] != "qa-task" {
		t.Fatalf("board does not contain waiting QA task IDs: %+v", board.Backlog[0])
	}
}

func TestMarkPlanningBacklogItemReportedAgainPersistsHighlightState(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	now := time.Date(2026, time.August, 22, 2, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	_, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogBug, Source: "qa", SourceReference: "task-1:bug:1",
		Title: "[QA] Existing regression", Description: "A concrete reproducible regression exists.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.StartBacklogPlanning(ctx, "project-1", item.ID); err != nil {
		t.Fatal(err)
	}

	board, highlighted, err := service.MarkBacklogItemReportedAgain(ctx, "project-1", item.ID, "qa-task")
	if err != nil {
		t.Fatal(err)
	}
	if highlighted.Status != BacklogPlanning || highlighted.RepeatReports != 1 {
		t.Fatalf("planning backlog item was not highlighted: %+v", highlighted)
	}
	if len(highlighted.WaitingQATaskIDs) != 1 || highlighted.WaitingQATaskIDs[0] != "qa-task" {
		t.Fatalf("planning backlog item did not retain waiting QA task: %+v", highlighted)
	}
	if board.Backlog[0].RepeatReports != 1 {
		t.Fatalf("board does not contain highlighted planning state: %+v", board.Backlog[0])
	}
}

func TestCompletingSharedPlanningQABugRequeuesWaitingQATask(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 15, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "bug-qa",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
			{ID: "plan-bug", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: now.Add(-1 * time.Hour)}},
		},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: BacklogPlanning, PlanID: "plan-bug",
			WaitingQATaskIDs: []string{"qa-task"},
		}},
		Tasks: []Task{
			{ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3, BlockedReason: "QA is waiting on a bug already in planning.", Execution: &TaskExecution{Summary: "Failed."}, ThreadID: "qa-thread"},
			{ID: "bug-qa", PlanID: "plan-bug", Key: "QA-002", Role: RoleQA, Column: ColumnQA, Status: TaskVerifying, SequenceOrder: 2},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.CompleteTask(context.Background(), "project-1", "bug-qa", "bug-qa-thread", TaskExecution{Summary: "Bug fix passed QA."})
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogDone {
		t.Fatalf("QA bug backlog item was not marked done: %+v", board.Backlog[0])
	}
	originalQA := board.Tasks[0]
	if originalQA.Status != TaskQueued || originalQA.Column != ColumnQA || originalQA.BlockedReason != "" || originalQA.Execution != nil {
		t.Fatalf("waiting QA task was not requeued cleanly: %+v", originalQA)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("original QA sequence was not rescheduled: %+v", board.Plans[0].Sequence)
	}
}

func TestCompletingLegacySharedPlanningQABugRequeuesWaitingQATaskFromFindings(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 20, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "bug-qa",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
			{ID: "plan-bug", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: now.Add(-1 * time.Hour)}},
		},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: BacklogPlanning, PlanID: "plan-bug",
		}},
		Tasks: []Task{
			{
				ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3,
				BlockedReason: "QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.",
				Execution:     &TaskExecution{Summary: "Failed.", Findings: []TaskFinding{{Title: "Save fails", Severity: "high"}}},
				ThreadID:      "qa-thread",
			},
			{ID: "bug-qa", PlanID: "plan-bug", Key: "QA-002", Role: RoleQA, Column: ColumnQA, Status: TaskVerifying, SequenceOrder: 2},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.CompleteTask(context.Background(), "project-1", "bug-qa", "bug-qa-thread", TaskExecution{Summary: "Bug fix passed QA."})
	if err != nil {
		t.Fatal(err)
	}
	originalQA := board.Tasks[0]
	if originalQA.Status != TaskQueued || originalQA.Column != ColumnQA || originalQA.BlockedReason != "" || originalQA.Execution != nil {
		t.Fatalf("legacy waiting QA task was not requeued cleanly: %+v", originalQA)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("original QA sequence was not rescheduled: %+v", board.Plans[0].Sequence)
	}
}

func TestCompletingOneLegacySharedPlanningQABugWaitsForRemainingFindings(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 25, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "bug-qa",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
			{ID: "plan-bug-a", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: now.Add(-1 * time.Hour)}},
			{ID: "plan-bug-b", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceScheduled, ScheduledAt: now.Add(-30 * time.Minute)}},
		},
		Backlog: []BacklogItem{
			{ID: "bug-a", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1", Title: "[QA] Save fails", Status: BacklogPlanning, PlanID: "plan-bug-a"},
			{ID: "bug-b", Key: "BUG-002", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:2", Title: "[QA] Load fails", Status: BacklogPlanning, PlanID: "plan-bug-b"},
		},
		Tasks: []Task{
			{
				ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3,
				BlockedReason: "QA is waiting on 2 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.",
				Execution:     &TaskExecution{Summary: "Failed.", Findings: []TaskFinding{{Title: "Save fails"}, {Title: "Load fails"}}},
			},
			{ID: "bug-qa", PlanID: "plan-bug-a", Key: "QA-002", Role: RoleQA, Column: ColumnQA, Status: TaskVerifying, SequenceOrder: 2},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.CompleteTask(context.Background(), "project-1", "bug-qa", "bug-qa-thread", TaskExecution{Summary: "First bug fix passed QA."})
	if err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].Status != TaskBlocked {
		t.Fatalf("QA should keep waiting for the remaining planning bug: %+v", board.Tasks[0])
	}
}

func TestGetProjectBoardRequeuesLegacyWaitingQAWhenMatchedBugAlreadyDone(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 35, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA is waiting", ScheduledAt: now.Add(-time.Hour)}}},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Status: BacklogDone, PlanID: "plan-bug",
		}},
		Tasks: []Task{{
			ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3,
			BlockedReason: "QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.",
			Execution:     &TaskExecution{Summary: "Failed.", Findings: []TaskFinding{{Title: "Save fails"}}},
		}},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.GetProjectBoard(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	qaTask := board.Tasks[0]
	if qaTask.Status != TaskQueued || qaTask.BlockedReason != "" || qaTask.Execution != nil {
		t.Fatalf("legacy waiting QA was not recovered after matched bug was done: %+v", qaTask)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("sequence was not rescheduled: %+v", board.Plans[0].Sequence)
	}
}

func TestGetProjectBoardKeepsLegacyWaitingQAWhenMatchedBugStillPlanning(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 40, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA is waiting", ScheduledAt: now.Add(-time.Hour)}}},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Status: BacklogPlanning, PlanID: "plan-bug",
		}},
		Tasks: []Task{{
			ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3,
			BlockedReason: "QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created.",
			Execution:     &TaskExecution{Summary: "Failed.", Findings: []TaskFinding{{Title: "Save fails"}}},
		}},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.GetProjectBoard(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].Status != TaskBlocked {
		t.Fatalf("QA should keep waiting while the matching bug is still planning: %+v", board.Tasks[0])
	}
}

func TestCompletePlanAsAlreadyImplementedMarksBacklogDone(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	board, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogBug, Source: "manual", Title: "Settings save bug", Description: "Settings save behavior already has a bug card.",
		AcceptanceCriteria: []string{"Settings save succeeds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, plan, err := service.StartBacklogPlanning(ctx, "project-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}

	board, err = service.CompletePlanAsAlreadyImplemented(ctx, board.ID, plan.ID, "thread-team-lead", PlanPreflight{
		Summary:      "Repository preflight found the requested settings save fix already implemented.",
		Evidence:     []string{"src/settings.ts normalizes the empty list before persistence"},
		Verification: []string{"Focused settings save regression test already exists"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogDone {
		t.Fatalf("backlog item was not marked done: %+v", board.Backlog[0])
	}
	if board.Plans[0].Status != PlanningCompleted || board.Plans[0].Preflight == nil || board.Plans[0].ThreadID != "thread-team-lead" {
		t.Fatalf("plan verification was not persisted: %+v", board.Plans[0])
	}
	if len(board.Tasks) != 0 {
		t.Fatalf("already implemented work must not create downstream tasks: %+v", board.Tasks)
	}
}

func TestCompletePlanAsNotFeasibleMarksBacklogNotFeasible(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	board, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogFeature, Source: "manual", Title: "Unsupported operating system integration",
		Description:        "Add a platform integration that this local desktop app cannot access safely.",
		AcceptanceCriteria: []string{"Integration runs without elevated system permissions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, plan, err := service.StartBacklogPlanning(ctx, "project-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}

	board, err = service.CompletePlanAsNotFeasible(ctx, board.ID, plan.ID, "thread-team-lead", PlanPreflight{
		Summary:       "Repository preflight found the requested integration cannot be implemented as stated.",
		Evidence:      []string{"The app has no safe permission boundary for the requested OS-level integration"},
		Verification:  []string{"Inspected the Tauri command surface and settings schema"},
		RemainingWork: []string{"Re-scope the request to a user-approved local command integration"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogNotFeasible {
		t.Fatalf("backlog item was not marked not feasible: %+v", board.Backlog[0])
	}
	if board.Plans[0].Status != PlanningNotFeasible || board.Plans[0].Preflight == nil || board.Plans[0].Preflight.Status != "not_feasible" {
		t.Fatalf("plan feasibility result was not persisted: %+v", board.Plans[0])
	}
	if len(board.Tasks) != 0 {
		t.Fatalf("not feasible work must not create downstream tasks: %+v", board.Tasks)
	}
}

func TestCompletingQABugPlanRequeuesOriginalQAWhenAllBugsResolved(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "bug-qa",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
			{ID: "plan-bug", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: now.Add(-1 * time.Hour)}},
		},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "qa-task:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: BacklogPlanning, PlanID: "plan-bug",
		}},
		Tasks: []Task{
			{ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3, BlockedReason: "QA found one bug.", Execution: &TaskExecution{Summary: "Failed."}, ThreadID: "qa-thread"},
			{ID: "bug-qa", PlanID: "plan-bug", Key: "QA-002", Role: RoleQA, Column: ColumnQA, Status: TaskVerifying, SequenceOrder: 2},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.CompleteTask(context.Background(), "project-1", "bug-qa", "bug-qa-thread", TaskExecution{Summary: "Bug fix passed QA."})
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogDone {
		t.Fatalf("QA bug backlog item was not marked done: %+v", board.Backlog[0])
	}
	originalQA := board.Tasks[0]
	if originalQA.Status != TaskQueued || originalQA.Column != ColumnQA || originalQA.BlockedReason != "" || originalQA.Execution != nil {
		t.Fatalf("original QA task was not requeued cleanly: %+v", originalQA)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("original QA sequence was not rescheduled: %+v", board.Plans[0].Sequence)
	}
}

func TestCompletingManualQABugPlanMarksBacklogDone(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 30, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "bug-qa",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved},
			{ID: "plan-bug", Status: PlanningApproved},
		},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "qa-task:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: BacklogPlanning, PlanID: "plan-bug",
		}},
		Tasks: []Task{
			{ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, BlockedReason: "QA found one bug."},
			{ID: "bug-dev", PlanID: "plan-bug", Key: "DEV-001", Role: RoleDeveloper, Column: ColumnDone, Status: TaskCompleted},
			{ID: "bug-qa", PlanID: "plan-bug", Key: "QA-002", Role: RoleQA, Column: ColumnQA, Status: TaskVerifying},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.CompleteTask(context.Background(), "project-1", "bug-qa", "bug-qa-thread", TaskExecution{Summary: "Bug fix passed QA."})
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogDone {
		t.Fatalf("manual QA bug backlog item was not marked done: %+v", board.Backlog[0])
	}
	if board.Tasks[0].Status != TaskBlocked {
		t.Fatalf("manual original QA should still wait for explicit PM verify action: %+v", board.Tasks[0])
	}
}

func TestGetProjectBoardNormalizesFinishedPlanningBacklog(t *testing.T) {
	now := time.Date(2026, time.August, 24, 9, 45, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Backlog: []BacklogItem{
			{ID: "finished-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", Status: BacklogPlanning, PlanID: "plan-finished"},
			{ID: "active-item", Key: "BUG-002", Type: BacklogBug, Source: "qa", Status: BacklogPlanning, PlanID: "plan-active"},
		},
		Tasks: []Task{
			{ID: "finished-dev", PlanID: "plan-finished", Status: TaskCompleted},
			{ID: "finished-qa", PlanID: "plan-finished", Status: TaskCompleted},
			{ID: "active-dev", PlanID: "plan-active", Status: TaskCompleted},
			{ID: "active-qa", PlanID: "plan-active", Status: TaskBlocked},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.GetProjectBoard(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if board.Backlog[0].Status != BacklogDone {
		t.Fatalf("finished planning backlog was not normalized: %+v", board.Backlog[0])
	}
	if board.Backlog[1].Status != BacklogPlanning {
		t.Fatalf("active planning backlog should stay planning: %+v", board.Backlog[1])
	}
}

func TestQueueControlSafeStopAndContinue(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce", ActiveTaskID: "task-active",
		Tasks: []Task{{ID: "task-active", Role: RoleDeveloper, Column: ColumnDeveloper, Status: TaskInProgress, UpdatedAt: now}},
	}}}
	service := NewService(repository)

	stopping, err := service.RequestQueueStop(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if stopping.QueueControl == nil || stopping.QueueControl.Status != QueueControlStopping || stopping.QueueControl.RequestedAt == nil {
		t.Fatalf("active board should request a stopping queue control state: %+v", stopping.QueueControl)
	}

	continued, err := service.ContinueQueue(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if continued.QueueControl != nil {
		t.Fatalf("continue should clear queue control state: %+v", continued.QueueControl)
	}
}

func TestPauseStoppingQueuesOnlyAfterActiveTaskIsReleased(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	requestedAt := now.Add(-time.Minute)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "Commerce", ActiveTaskID: "task-active",
		QueueControl: &QueueControl{Status: QueueControlStopping, RequestedAt: &requestedAt, UpdatedAt: requestedAt},
		Tasks:        []Task{{ID: "task-active", Role: RoleDeveloper, Column: ColumnDeveloper, Status: TaskInProgress, UpdatedAt: now}},
	}}}
	service := NewService(repository)

	paused, err := service.PauseStoppingQueues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paused) != 0 {
		t.Fatalf("queue with an active task must keep stopping state, paused=%d", len(paused))
	}

	board, err := service.UpdateTaskStatus(ctx, "project-1", "task-active", TaskCompleted, "")
	if err != nil {
		t.Fatal(err)
	}
	if board.QueueControl == nil || board.QueueControl.Status != QueueControlStopping {
		t.Fatalf("completing the task should not resume or pause before the scheduler drain: %+v", board.QueueControl)
	}
	paused, err = service.PauseStoppingQueues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paused) != 1 || paused[0].QueueControl == nil || paused[0].QueueControl.Status != QueueControlPaused || paused[0].QueueControl.PausedAt == nil {
		t.Fatalf("released stopping queue should become paused: %+v", paused)
	}
}

func TestRestartTaskQueuesBlockedTaskWithFreshSession(t *testing.T) {
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{{ID: "plan-1", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "DSN-001 failed", RoleThreadIDs: map[AgentRole]string{RoleDesigner: "stale-codex-thread"}}}},
		Tasks: []Task{{
			ID: "task-1", PlanID: "plan-1", Key: "DSN-001", Role: RoleDesigner, Column: ColumnDesigner,
			Status: TaskBlocked, SequenceOrder: 1, ThreadID: "stale-codex-thread", BlockedReason: "Codex usage limit reached",
			Execution: &TaskExecution{Summary: "Interrupted attempt"},
		}},
	}}}
	service := NewService(repository)

	restarted, err := service.RestartTask(context.Background(), "project-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	task := restarted.Tasks[0]
	if task.Status != TaskQueued || task.Column != ColumnDesigner || task.ThreadID != "" || task.Execution != nil || task.BlockedReason != "" {
		t.Fatalf("task was not restarted with clean execution state: %+v", task)
	}
	if restarted.Plans[0].Sequence.Status != PlanSequenceScheduled || restarted.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("sequence was not rescheduled: %+v", restarted.Plans[0].Sequence)
	}
	if len(restarted.Plans[0].Sequence.RoleThreadIDs) != 0 {
		t.Fatalf("restart must clear the sequence role session for a fresh run: %+v", restarted.Plans[0].Sequence.RoleThreadIDs)
	}
	if _, err = service.RestartTask(context.Background(), "project-1", "task-1"); err == nil || !strings.Contains(err.Error(), "only a blocked task") {
		t.Fatalf("queued task restart should be rejected, got %v", err)
	}
}

func TestIgnoreBlockedTaskCompletesAndResumesSequence(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{{ID: "plan-1", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "DSN-001 failed", ScheduledAt: now.Add(-time.Hour)}}},
		Tasks: []Task{{
			ID: "design", PlanID: "plan-1", Key: "DSN-001", Role: RoleDesigner, Column: ColumnDesigner,
			Status: TaskBlocked, SequenceOrder: 1, BlockedReason: "Design artifact validation failed",
		}, {
			ID: "develop", PlanID: "plan-1", Key: "DEV-001", Role: RoleDeveloper, Column: ColumnDeveloper,
			Status: TaskQueued, SequenceOrder: 2, DependencyIDs: []string{"design"},
		}},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.IgnoreBlockedTask(context.Background(), "project-1", "design")
	if err != nil {
		t.Fatal(err)
	}
	task := board.Tasks[0]
	if task.Status != TaskCompleted || task.Column != ColumnDone || task.BlockedReason != "" || task.Execution == nil {
		t.Fatalf("ignored task was not completed with execution report: %+v", task)
	}
	if task.Execution.Verdict != "ignored" || len(task.Execution.RemainingRisks) != 1 {
		t.Fatalf("ignored execution report did not preserve risk: %+v", task.Execution)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("ignore did not resume sequence: %+v", board.Plans[0].Sequence)
	}
	if _, err = service.IgnoreBlockedTask(context.Background(), "project-1", "design"); err == nil || !strings.Contains(err.Error(), "only a blocked task") {
		t.Fatalf("completed task ignore should be rejected, got %v", err)
	}
}

func TestCompleteSequenceTaskPersistsRoleThreadForNextTask(t *testing.T) {
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew", ActiveTaskID: "task-1",
		Plans: []Plan{{ID: "plan-1", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: time.Now().UTC()}}},
		Tasks: []Task{{
			ID: "task-1", PlanID: "plan-1", Key: "DEV-001", Role: RoleDeveloper, Column: ColumnDeveloper,
			Status: TaskInProgress, SequenceOrder: 1,
		}, {
			ID: "task-2", PlanID: "plan-1", Key: "DEV-002", Role: RoleDeveloper, Column: ColumnDeveloper,
			Status: TaskQueued, SequenceOrder: 2,
		}},
	}}}
	service := NewService(repository)

	board, err := service.CompleteTask(context.Background(), "project-1", "task-1", "developer-sequence-thread", TaskExecution{Summary: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	if board.Plans[0].Sequence.RoleThreadIDs[RoleDeveloper] != "developer-sequence-thread" {
		t.Fatalf("developer sequence session was not persisted: %+v", board.Plans[0].Sequence.RoleThreadIDs)
	}
	if board.Tasks[0].ThreadID != "developer-sequence-thread" {
		t.Fatalf("completed task thread ID was not stored: %+v", board.Tasks[0])
	}
}

func TestBacklogKeyUsesHighestExistingSequenceAfterDeletion(t *testing.T) {
	items := []BacklogItem{
		{Type: BacklogBug, Key: "BUG-001"},
		{Type: BacklogBug, Key: "BUG-009"},
		{Type: BacklogFeature, Key: "FEAT-015"},
	}

	if key := backlogKey(BacklogBug, items); key != "BUG-010" {
		t.Fatalf("expected next bug key after highest sequence, got %s", key)
	}
	if key := backlogKey(BacklogFeature, items); key != "FEAT-016" {
		t.Fatalf("expected independent feature sequence, got %s", key)
	}
}

func TestRemoveBacklogItem(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	_, item, err := service.AddBacklogItem(ctx, "project-1", "ProductCrew", BacklogDraft{
		Type: BacklogBug, Source: "qa", SourceReference: "task-1:bug:1",
		Title: "[QA] Save fails", Description: "A concrete reproducible regression exists.",
	})
	if err != nil {
		t.Fatal(err)
	}

	board, err := service.RemoveBacklogItem(ctx, "project-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(board.Backlog) != 0 {
		t.Fatalf("expected backlog to be empty, got: %d elements", len(board.Backlog))
	}
}

func TestRemoveBacklogItemRequeuesWaitingQATask(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{
			{ID: "plan-original", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
		},
		Backlog: []BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: BacklogPlanning, PlanID: "plan-bug",
			WaitingQATaskIDs: []string{"qa-task"},
		}},
		Tasks: []Task{
			{ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: RoleQA, Column: ColumnQA, Status: TaskBlocked, SequenceOrder: 3, BlockedReason: "QA is waiting on a bug already in planning.", Execution: &TaskExecution{Summary: "Failed."}, ThreadID: "qa-thread"},
		},
	}}}
	service := NewService(repository)
	service.now = func() time.Time { return now }

	board, err := service.RemoveBacklogItem(ctx, "project-1", "bug-item")
	if err != nil {
		t.Fatal(err)
	}

	if len(board.Backlog) != 0 {
		t.Fatalf("expected backlog to be empty, got: %d elements", len(board.Backlog))
	}

	originalQA := board.Tasks[0]
	if originalQA.Status != TaskQueued || originalQA.Column != ColumnQA || originalQA.BlockedReason != "" || originalQA.Execution != nil {
		t.Fatalf("waiting QA task was not requeued cleanly: %+v", originalQA)
	}
	if board.Plans[0].Sequence.Status != PlanSequenceScheduled || board.Plans[0].Sequence.BlockedReason != "" {
		t.Fatalf("original QA sequence was not rescheduled: %+v", board.Plans[0].Sequence)
	}
}

type memoryBoardRepository struct {
	boards []Board
}

func (r *memoryBoardRepository) Create(_ context.Context, board Board) error {
	for _, existing := range r.boards {
		if existing.ID == board.ID || existing.ProjectID == board.ProjectID {
			return ErrConflict
		}
	}
	r.boards = append(r.boards, board)
	return nil
}

func (r *memoryBoardRepository) Get(_ context.Context, id string) (Board, error) {
	for _, board := range r.boards {
		if board.ID == id {
			return board, nil
		}
	}
	return Board{}, ErrNotFound
}

func (r *memoryBoardRepository) List(context.Context) ([]Board, error) {
	return append([]Board(nil), r.boards...), nil
}

func (r *memoryBoardRepository) Update(_ context.Context, board Board) error {
	for index := range r.boards {
		if r.boards[index].ID == board.ID {
			r.boards[index] = board
			return nil
		}
	}
	return ErrNotFound
}

func (r *memoryBoardRepository) Delete(_ context.Context, id string) error {
	for index := range r.boards {
		if r.boards[index].ID == id {
			r.boards = append(r.boards[:index], r.boards[index+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (r *memoryBoardRepository) FindByProject(_ context.Context, projectID string) (Board, error) {
	for _, board := range r.boards {
		if board.ProjectID == projectID {
			return board, nil
		}
	}
	return Board{}, ErrNotFound
}

func TestRoleSafeSequentialTaskFlow(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Saved search filters", Description: "Persist and restore selected product search filters.",
		WorkType: "feature", DeliveryTarget: "fullstack", RequiresUI: true, AcceptanceCriteria: []string{"Core flow works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "thread-1", PlanDraft{
		Summary:   "Design the restore interaction, implement persistence, then verify the approved behavior.",
		Documents: []DraftDocument{{Ref: "spec", Title: "Feature specification", Kind: "product-spec", Audience: []AgentRole{RoleDesigner, RoleDeveloper, RoleQA}, Content: "Approved scope."}},
		Tasks: []DraftTask{
			{Ref: "design", Role: RoleDesigner, Title: "Design saved filter restore flow", Priority: PriorityHigh, Description: "Create the interaction specification.", AcceptanceCriteria: []string{"Design is implementation-ready"}, Documents: []string{"spec"}},
			{Ref: "implement", Role: RoleDeveloper, Title: "Implement saved filter persistence", Priority: PriorityHigh, Description: "Implement the approved design.", AcceptanceCriteria: []string{"Filters restore correctly"}, Dependencies: []string{"design"}, Documents: []string{"spec"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].Title != "[Designer] Design saved filter restore flow" || board.Tasks[1].Title != "[Developer] Implement saved filter persistence" {
		t.Fatalf("task titles were not normalized: %+v", board.Tasks)
	}
	if board.Tasks[0].DependencyIDs == nil {
		t.Fatal("tasks without dependencies must expose an empty collection, not nil")
	}
	board, err = service.ReviewPlan(ctx, "project-1", plan.ID, "approve", "", "PM")
	if err != nil {
		t.Fatal(err)
	}
	designerTask := board.Tasks[0]
	developerTask := board.Tasks[1]
	if _, err := service.MoveTask(ctx, "project-1", developerTask.ID, ColumnDesigner); err == nil || !strings.Contains(err.Error(), "Developer tasks") {
		t.Fatalf("expected role-safe queue rejection, got %v", err)
	}
	board, err = service.MoveTask(ctx, "project-1", developerTask.ID, ColumnDeveloper)
	if err != nil {
		t.Fatalf("dependent tasks should be accepted into the queue: %v", err)
	}
	if board.Tasks[1].Status != TaskQueued || board.Tasks[1].Column != ColumnDeveloper {
		t.Fatalf("dependent Developer task was not queued: %+v", board.Tasks[1])
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-1", developerTask.ID, TaskInProgress, ""); err == nil || !strings.Contains(err.Error(), designerTask.Key) {
		t.Fatalf("expected execution to wait for %s, got %v", designerTask.Key, err)
	}
	if _, err := service.MoveTask(ctx, "project-1", designerTask.ID, ColumnDesigner); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-1", designerTask.ID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-1", designerTask.ID, TaskCompleted, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-1", developerTask.ID, TaskInProgress, ""); err != nil {
		t.Fatalf("dependent task should start after its prerequisite completes: %v", err)
	}
}

func TestDenyPlanningRequiresReason(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Saved filters", Description: "Persist selected product search filters.", AcceptanceCriteria: []string{"Core flow works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{Summary: "A complete planning summary for PM review.", Tasks: []DraftTask{{Ref: "implementation", Role: RoleDeveloper, Title: "Implement filters", Description: "Implement persistence.", AcceptanceCriteria: []string{"Works"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, "project-1", plan.ID, "deny", "", "PM"); err == nil {
		t.Fatal("expected deny without feedback to fail")
	}
	updated, err := service.ReviewPlan(ctx, "project-1", plan.ID, "deny", "Cover migration behavior", "PM")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Plans[0].Status != PlanningChangesRequested || updated.Plans[0].Reviews[0].Reason == "" {
		t.Fatalf("feedback was not persisted: %+v", updated.Plans[0])
	}
}

func TestApprovePlanSequenceQueuesStrictOrderedChain(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-sequence", "Sequence project", WorkRequest{
		Title: "Ordered delivery", Description: "Design, implement, and verify an ordered feature.", RequiresUI: true, AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Deliver the feature through one strict Designer to Developer to QA sequence.",
		Tasks: []DraftTask{
			{Ref: "design", Role: RoleDesigner, Title: "Design flow", Description: "Design the approved flow.", AcceptanceCriteria: []string{"Flow documented"}},
			{Ref: "implement-ui", Role: RoleDeveloper, Title: "Implement UI", Description: "Implement the approved UI.", AcceptanceCriteria: []string{"UI works"}},
			{Ref: "implement-api", Role: RoleDeveloper, Title: "Implement API", Description: "Implement the supporting API.", AcceptanceCriteria: []string{"API works"}},
			{Ref: "verify", Role: RoleQA, Title: "Verify delivery", Description: "Verify the complete delivery.", AcceptanceCriteria: []string{"No bugs"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.ApprovePlanSequence(ctx, "project-sequence", plan.ID, "PM")
	if err != nil {
		t.Fatal(err)
	}
	if queued.Plans[0].Status != PlanningApproved || queued.Plans[0].Sequence == nil || queued.Plans[0].Sequence.Status != PlanSequenceScheduled {
		t.Fatalf("sequence approval was not persisted: %+v", queued.Plans[0])
	}
	for index, task := range queued.Tasks {
		if task.SequenceOrder != index+1 || task.Status != TaskQueued || task.Column != task.Role.QueueColumn() {
			t.Fatalf("task %d was not queued in sequence order: %+v", index+1, task)
		}
		if index > 0 {
			foundPredecessor := false
			for _, dependencyID := range task.DependencyIDs {
				foundPredecessor = foundPredecessor || dependencyID == queued.Tasks[index-1].ID
			}
			if !foundPredecessor {
				t.Fatalf("task %s does not depend on its sequence predecessor", task.Key)
			}
		}
	}
	started, err := service.UpdateTaskStatus(ctx, "project-sequence", queued.Tasks[0].ID, TaskInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Plans[0].Sequence.Status != PlanSequenceRunning || started.Plans[0].Sequence.StartedAt == nil {
		t.Fatalf("sequence did not enter running state: %+v", started.Plans[0].Sequence)
	}
	completedFirst, err := service.UpdateTaskStatus(ctx, "project-sequence", queued.Tasks[0].ID, TaskCompleted, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-sequence", completedFirst.Tasks[1].ID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	blocked, err := service.UpdateTaskStatus(ctx, "project-sequence", completedFirst.Tasks[1].ID, TaskBlocked, "Implementation needs attention")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Plans[0].Sequence.Status != PlanSequenceBlocked {
		t.Fatalf("blocked task did not pause its sequence: %+v", blocked.Plans[0].Sequence)
	}
	requeued, err := service.MoveTask(ctx, "project-sequence", completedFirst.Tasks[1].ID, ColumnDeveloper)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Plans[0].Sequence.Status != PlanSequenceScheduled || requeued.Tasks[1].Status != TaskQueued {
		t.Fatalf("re-queue did not resume the sequence: plan=%+v task=%+v", requeued.Plans[0].Sequence, requeued.Tasks[1])
	}
}

func TestManualDesignerCompletionRequiresPMApprovalBeforeDependenciesStart(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-design-review", "Design review project", WorkRequest{
		Title: "Designed delivery", Description: "Design, approve, and implement a visible feature.", RequiresUI: true, AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Design first, then implement after PM approves the handoff.",
		Tasks: []DraftTask{
			{Ref: "design", Role: RoleDesigner, Title: "Design flow", Description: "Create the handoff.", AcceptanceCriteria: []string{"Mockup is reviewable"}},
			{Ref: "implement", Role: RoleDeveloper, Title: "Implement flow", Description: "Implement the approved handoff.", AcceptanceCriteria: []string{"UI works"}, Dependencies: []string{"design"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-design-review", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	designerID := board.Tasks[0].ID
	developerID := board.Tasks[1].ID
	if _, err = service.MoveTask(ctx, "project-design-review", developerID, ColumnDeveloper); err != nil {
		t.Fatal(err)
	}
	if _, err = service.MoveTask(ctx, "project-design-review", designerID, ColumnDesigner); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-design-review", designerID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	reviewing, err := service.CompleteTask(ctx, "project-design-review", designerID, "designer-thread", TaskExecution{
		Summary: "Design handoff ready.", ChangedFiles: []string{".productcrew/design-artifacts/task/handoff.md"}, Verification: []string{"Mockup rendered"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewing.Tasks[0].Status != TaskDesignReview || reviewing.Tasks[0].Column != ColumnDesigner || reviewing.ActiveTaskID != "" || reviewing.Tasks[0].Execution == nil {
		t.Fatalf("manual Designer task must wait for PM review with its report retained: %+v", reviewing.Tasks[0])
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-design-review", developerID, TaskInProgress, ""); err == nil || !strings.Contains(err.Error(), reviewing.Tasks[0].Key) {
		t.Fatalf("dependent task should wait for PM design approval, got %v", err)
	}
	approved, err := service.ApproveDesignHandoff(ctx, "project-design-review", designerID, "PM")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Tasks[0].Status != TaskCompleted || approved.Tasks[0].Column != ColumnDone {
		t.Fatalf("approved handoff should complete the Designer task: %+v", approved.Tasks[0])
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-design-review", developerID, TaskInProgress, ""); err != nil {
		t.Fatalf("dependent task should start after PM approval: %v", err)
	}
}

func TestSubmitDesignFeedbackRequeuesSameManualDesignerTaskAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	now := time.Date(2026, time.August, 28, 6, 15, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	board, plan, err := service.CreatePlan(ctx, "project-design-feedback", "Design feedback project", WorkRequest{
		Title: "Designer feedback loop", Description: "Revise the existing mockup in place.", RequiresUI: true, AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Deliver one manual Designer handoff for PM review.",
		Tasks:   []DraftTask{{Ref: "design", Role: RoleDesigner, Title: "Design flow", Description: "Create the handoff.", AcceptanceCriteria: []string{"Mockup is reviewable"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-design-feedback", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	designerID := board.Tasks[0].ID
	if _, err = service.MoveTask(ctx, "project-design-feedback", designerID, ColumnDesigner); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-design-feedback", designerID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	reviewing, err := service.CompleteTask(ctx, "project-design-feedback", designerID, "designer-thread", TaskExecution{
		Summary:      "Initial mockup ready.",
		ChangedFiles: []string{".productcrew/design-artifacts/task-design/overview.html"},
		Verification: []string{"Reviewed the task drawer structure."},
		Artifacts:    []DesignArtifact{{ID: "artifact-1", RelativePath: ".productcrew/design-artifacts/task-design/overview.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewing.Tasks[0].Execution == nil {
		t.Fatalf("manual Designer review state should expose the active execution before PM feedback")
	}

	requeued, err := service.SubmitDesignFeedback(ctx, "project-design-feedback", designerID, "Keep Approve primary and refine the feedback composer hierarchy.", "PM")
	if err != nil {
		t.Fatal(err)
	}
	task := requeued.Tasks[0]
	if task.ID != designerID || task.Status != TaskQueued || task.Column != ColumnDesigner {
		t.Fatalf("manual Designer task was not requeued in place: %+v", task)
	}
	if task.ThreadID != "designer-thread" || task.Execution != nil || task.BlockedReason != "" {
		t.Fatalf("requeued task should preserve the resumable thread and clear only active execution: %+v", task)
	}
	if len(task.RevisionHistory) != 1 {
		t.Fatalf("revision history was not archived: %+v", task.RevisionHistory)
	}
	revision := task.RevisionHistory[0]
	if revision.Revision != 1 || revision.Feedback != "Keep Approve primary and refine the feedback composer hierarchy." || revision.Reviewer != "PM" {
		t.Fatalf("PM feedback metadata was not preserved: %+v", revision)
	}
	if revision.RequestedAt != now || revision.Execution.Summary != "Initial mockup ready." || len(revision.Execution.Artifacts) != 1 {
		t.Fatalf("prior Designer execution was not preserved in history: %+v", revision)
	}
}

func TestSubmitDesignFeedbackRejectsSequenceDesignerTask(t *testing.T) {
	now := time.Date(2026, time.August, 28, 6, 30, 0, 0, time.UTC)
	repository := &memoryBoardRepository{boards: []Board{{
		ID: "board-1", ProjectID: "project-1", ProjectName: "ProductCrew",
		Plans: []Plan{{ID: "plan-1", Status: PlanningApproved, Sequence: &PlanSequence{Status: PlanSequenceRunning, ScheduledAt: now}}},
		Tasks: []Task{{
			ID: "task-design", PlanID: "plan-1", Key: "DSN-001", Role: RoleDesigner,
			Column: ColumnDesigner, Status: TaskDesignReview, SequenceOrder: 1,
			Execution: &TaskExecution{Summary: "Sequence mockup ready.", CompletedAt: now},
		}},
	}}}
	service := NewService(repository)

	if _, err := service.SubmitDesignFeedback(context.Background(), "project-1", "task-design", "Revise the header.", "PM"); err == nil || !strings.Contains(err.Error(), "only manual Designer handoffs") {
		t.Fatalf("expected sequence Designer feedback to be rejected, got %v", err)
	}
}

func TestSequenceDesignerCompletionBypassesPMDesignApproval(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-sequence-design", "Sequence design project", WorkRequest{
		Title: "Autopilot delivery", Description: "Run the designed feature as one automated sequence.", RequiresUI: true, AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Autopilot handles the ordered delivery.",
		Tasks: []DraftTask{
			{Ref: "design", Role: RoleDesigner, Title: "Design flow", Description: "Create the handoff.", AcceptanceCriteria: []string{"Mockup rendered"}},
			{Ref: "implement", Role: RoleDeveloper, Title: "Implement flow", Description: "Implement the handoff.", AcceptanceCriteria: []string{"UI works"}},
			{Ref: "verify", Role: RoleQA, Title: "Verify flow", Description: "Verify the delivery.", AcceptanceCriteria: []string{"No bugs"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.ApprovePlanSequence(ctx, "project-sequence-design", plan.ID, "PM")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-sequence-design", queued.Tasks[0].ID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteTask(ctx, "project-sequence-design", queued.Tasks[0].ID, "designer-thread", TaskExecution{Summary: "Design handoff ready."})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Tasks[0].Status != TaskCompleted || completed.Tasks[0].Column != ColumnDone {
		t.Fatalf("sequence Designer task should bypass PM design review: %+v", completed.Tasks[0])
	}
}

func TestApprovePlanSequenceRequiresFinalQA(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-invalid-sequence", "Invalid sequence", WorkRequest{
		Title: "Missing QA", Description: "Implement without a final QA task.", AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "This intentionally incomplete sequence does not contain final QA verification.",
		Tasks:   []DraftTask{{Ref: "implement", Role: RoleDeveloper, Title: "Implement", Description: "Implement it.", AcceptanceCriteria: []string{"Works"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApprovePlanSequence(ctx, "project-invalid-sequence", plan.ID, "PM"); err == nil || !strings.Contains(err.Error(), "end with a QA task") {
		t.Fatalf("expected final QA validation, got %v", err)
	}
}

func TestQABugPlanCanRunAsSequenceAfterPlanning(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, item, err := service.AddBacklogItem(ctx, "project-qa-bug", "QA project", BacklogDraft{
		Type: BacklogBug, Source: "qa", SourceReference: "qa-task:bug:1", Title: "Fix QA regression",
		Description: "Fix the concrete regression reported by QA.", AcceptanceCriteria: []string{"Regression fixed"},
		RequiresUI: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	board, plan, err := service.StartBacklogPlanning(ctx, "project-qa-bug", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Fix the QA regression and independently verify the corrected behavior.",
		Tasks: []DraftTask{
			{Ref: "fix", Role: RoleDeveloper, Title: "Fix regression", Description: "Fix the reported regression.", AcceptanceCriteria: []string{"Fixed"}},
			{Ref: "verify", Role: RoleQA, Title: "Verify regression", Description: "Verify the regression fix.", AcceptanceCriteria: []string{"No regression"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sequenced, err := service.ApprovePlanSequence(ctx, "project-qa-bug", plan.ID, "PM")
	if err != nil {
		t.Fatal(err)
	}
	if sequenced.Plans[0].Sequence == nil || sequenced.Plans[0].Sequence.Status != PlanSequenceScheduled {
		t.Fatalf("QA bug plan was not scheduled as a sequence: %+v", sequenced.Plans[0])
	}
}

func TestOnlyOneTaskCanRunAcrossProjectBoards(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	var queuedTasks []Task
	for _, projectID := range []string{"project-1", "project-2"} {
		board, plan, err := service.CreatePlan(ctx, projectID, projectID, WorkRequest{
			Title: "Implement feature", Description: "Implement one isolated feature for this project.", AcceptanceCriteria: []string{"Works"},
		})
		if err != nil {
			t.Fatal(err)
		}
		board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
			Summary: "Implement and verify one concrete task without parallel agent execution.",
			Tasks:   []DraftTask{{Ref: "implementation", Role: RoleDeveloper, Title: "Implement feature", Description: "Implement the feature.", AcceptanceCriteria: []string{"Works"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ReviewPlan(ctx, projectID, plan.ID, "approve", "", "PM"); err != nil {
			t.Fatal(err)
		}
		board, err = service.MoveTask(ctx, projectID, board.Tasks[0].ID, ColumnDeveloper)
		if err != nil {
			t.Fatal(err)
		}
		queuedTasks = append(queuedTasks, board.Tasks[0])
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-1", queuedTasks[0].ID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTaskStatus(ctx, "project-2", queuedTasks[1].ID, TaskInProgress, ""); err == nil || !strings.Contains(err.Error(), "project-1") {
		t.Fatalf("expected global sequential guard, got %v", err)
	}
}

func TestPMCanQueueMultipleTasksWhileAnotherTaskIsActive(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Implement feature set", Description: "Implement two independent feature tasks sequentially.", AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Two independent developer tasks can be queued while execution remains sequential.",
		Tasks: []DraftTask{
			{Ref: "first", Role: RoleDeveloper, Title: "Implement first slice", Description: "Implement the first feature slice.", AcceptanceCriteria: []string{"Works"}},
			{Ref: "second", Role: RoleDeveloper, Title: "Implement second slice", Description: "Implement the second feature slice.", AcceptanceCriteria: []string{"Works"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-1", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	firstTaskID := board.Tasks[0].ID
	secondTaskID := board.Tasks[1].ID
	if _, err = service.MoveTask(ctx, "project-1", firstTaskID, ColumnDeveloper); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-1", firstTaskID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	queued, err := service.MoveTask(ctx, "project-1", secondTaskID, ColumnDeveloper)
	if err != nil {
		t.Fatalf("PM should be able to queue another task while one is active: %v", err)
	}
	if queued.ActiveTaskID != firstTaskID || queued.Tasks[1].Status != TaskQueued {
		t.Fatalf("queueing another task must not steal the active slot: %+v", queued)
	}
}

func TestAgentStatusTransitionsStayInsideAssignedQueue(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Implement feature", Description: "Implement one isolated feature for this project.", AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Implement and verify one concrete task without parallel agent execution.",
		Tasks:   []DraftTask{{Ref: "implementation", Role: RoleDeveloper, Title: "Implement feature", Description: "Implement the feature.", AcceptanceCriteria: []string{"Works"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, "project-1", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	task := board.Tasks[0]
	if _, err := service.UpdateTaskStatus(ctx, "project-1", task.ID, TaskBlocked, "Waiting for input"); err == nil {
		t.Fatal("planned task must not be blocked before PM queues it")
	}
	board, err = service.MoveTask(ctx, "project-1", task.ID, ColumnDeveloper)
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.UpdateTaskStatus(ctx, "project-1", task.ID, TaskBlocked, "Waiting for input")
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.UpdateTaskStatus(ctx, "project-1", task.ID, TaskInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].BlockedReason != "" {
		t.Fatalf("blocked reason must clear when work resumes: %+v", board.Tasks[0])
	}
}

func TestCompleteTaskPersistsDeveloperDeliveryReport(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Implement feature", Description: "Implement one isolated feature for this project.", AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{
		Summary: "Implement and verify one concrete task without parallel execution.",
		Tasks:   []DraftTask{{Ref: "implementation", Role: RoleDeveloper, Title: "Implement feature", Description: "Implement the feature.", AcceptanceCriteria: []string{"Works"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-1", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	taskID := board.Tasks[0].ID
	if _, err = service.MoveTask(ctx, "project-1", taskID, ColumnDeveloper); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-1", taskID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteTask(ctx, "project-1", taskID, "thread-123", TaskExecution{
		Summary: "Implemented the feature.", ChangedFiles: []string{"feature.go"}, Verification: []string{"go test ./... passed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := completed.Tasks[0]
	if task.Status != TaskCompleted || task.Column != ColumnDone || completed.ActiveTaskID != "" || task.ThreadID != "thread-123" || task.Execution == nil || task.Execution.CompletedAt.IsZero() {
		t.Fatalf("delivery result was not persisted atomically: %+v", task)
	}
}

func TestBuildVerificationKeepsTaskActiveUntilPassed(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-build", "Build project", WorkRequest{Title: "Implement feature", Description: "Implement and verify the feature.", AcceptanceCriteria: []string{"Build passes"}})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{Summary: "Implement one verified task.", Tasks: []DraftTask{{Ref: "implementation", Role: RoleDeveloper, Title: "Implement feature", Description: "Implement it.", AcceptanceCriteria: []string{"Build passes"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-build", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	taskID := board.Tasks[0].ID
	if _, err = service.MoveTask(ctx, "project-build", taskID, ColumnDeveloper); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-build", taskID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	verifying, err := service.StartBuildVerification(ctx, "project-build", taskID, BuildVerification{RunID: "run-1", ActionID: "check", ActionLabel: "npm run check", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if verifying.Tasks[0].Status != TaskVerifying || verifying.ActiveTaskID != taskID {
		t.Fatalf("verification must retain active slot: %+v", verifying)
	}
	retryVerifying, err := service.StartBuildVerification(ctx, "project-build", taskID, BuildVerification{RunID: "run-2", ActionID: "check", ActionLabel: "npm run check", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if retryVerifying.Tasks[0].BuildVerification == nil || retryVerifying.Tasks[0].BuildVerification.RunID != "run-2" {
		t.Fatalf("active verification should support a build repair retry: %+v", retryVerifying.Tasks[0].BuildVerification)
	}
	completed, err := service.CompleteTask(ctx, "project-build", taskID, "thread-1", TaskExecution{Summary: "Implemented.", BuildVerification: &BuildVerification{RunID: "run-2", ActionID: "check", ActionLabel: "npm run check", Status: "passed", ExitCode: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Tasks[0].Status != TaskCompleted || completed.Tasks[0].BuildVerification == nil || completed.Tasks[0].BuildVerification.Status != "passed" {
		t.Fatalf("passed verification was not persisted: %+v", completed.Tasks[0])
	}
}

func TestBlockTaskWithExecutionPersistsQAFindings(t *testing.T) {
	ctx := context.Background()
	service := NewService(&memoryBoardRepository{})
	board, plan, err := service.CreatePlan(ctx, "project-qa", "Commerce", WorkRequest{Title: "Verify feature", Description: "Verify the completed implementation independently.", AcceptanceCriteria: []string{"No bugs"}})
	if err != nil {
		t.Fatal(err)
	}
	board, err = service.ApplyDraft(ctx, board.ID, plan.ID, "", PlanDraft{Summary: "Verify the approved implementation against the design.", Tasks: []DraftTask{{Ref: "qa", Role: RoleQA, Title: "Verify feature", Description: "Run acceptance checks.", AcceptanceCriteria: []string{"No bugs"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewPlan(ctx, "project-qa", plan.ID, "approve", "", "PM"); err != nil {
		t.Fatal(err)
	}
	taskID := board.Tasks[0].ID
	if _, err = service.MoveTask(ctx, "project-qa", taskID, ColumnQA); err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateTaskStatus(ctx, "project-qa", taskID, TaskInProgress, ""); err != nil {
		t.Fatal(err)
	}
	blocked, err := service.BlockTaskWithExecution(ctx, "project-qa", taskID, "thread-qa", "QA found one reproducible bug.", TaskExecution{Verdict: "failed", Summary: "One regression remains.", Findings: []TaskFinding{{Severity: "high", Title: "Save fails"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := blocked.Tasks[0]
	if task.Status != TaskBlocked || blocked.ActiveTaskID != "" || task.Execution == nil || len(task.Execution.Findings) != 1 || task.Execution.CompletedAt.IsZero() {
		t.Fatalf("QA report was not persisted: %+v", task)
	}
}

func TestRecoveryConvertsInterruptedWorkIntoExplicitCardState(t *testing.T) {
	ctx := context.Background()
	repository := &memoryBoardRepository{}
	service := NewService(repository)
	board, plan, err := service.CreatePlan(ctx, "project-1", "Commerce", WorkRequest{
		Title: "Implement feature", Description: "Implement one isolated feature for this project.", AcceptanceCriteria: []string{"Works"},
	})
	if err != nil {
		t.Fatal(err)
	}
	board.Tasks = []Task{{ID: "task-1", PlanID: plan.ID, Key: "DEV-001", Title: "[Developer] Implement feature", Role: RoleDeveloper, Column: ColumnDeveloper, Status: TaskInProgress}}
	board.ActiveTaskID = "task-1"
	if err := repository.Update(ctx, board); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverInterruptedState(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.GetProjectBoard(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Plans[0].Status != PlanningFailed || recovered.Tasks[0].Status != TaskBlocked || recovered.ActiveTaskID != "" {
		t.Fatalf("interrupted state was not made explicit: %+v", recovered)
	}
}

func TestRepositoryNotFoundContract(t *testing.T) {
	repository := &memoryBoardRepository{}
	_, err := repository.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
