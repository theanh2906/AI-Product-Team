package web

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/quality"
)

func TestFindExistingQABugUsesValidatedBacklogID(t *testing.T) {
	board := kanban.Board{Backlog: []kanban.BacklogItem{
		{ID: "qa-bug", Key: "BUG-002", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogOpen, Title: "[QA] Downloader keeps a removed build type"},
		{ID: "manual-bug", Key: "BUG-003", Type: kanban.BacklogBug, Source: "manual", Status: kanban.BacklogOpen, Title: "Manual report"},
	}}

	match, found := findExistingQABug(board, quality.Bug{ExistingBacklogID: "qa-bug", Title: "Semantically equivalent wording"}, "task-2:bug:1")
	if !found || match.ID != "qa-bug" {
		t.Fatalf("expected validated QA backlog match, got %+v, found=%v", match, found)
	}
	if _, found = findExistingQABug(board, quality.Bug{ExistingBacklogID: "manual-bug", Title: "Different title"}, "task-2:bug:1"); found {
		t.Fatal("a non-QA backlog ID must not be accepted as an existing QA bug")
	}
}

func TestFindExistingQABugFallsBackToNormalizedTitle(t *testing.T) {
	board := kanban.Board{Backlog: []kanban.BacklogItem{{
		ID: "qa-bug", Key: "BUG-002", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogOpen,
		Title: "[QA] Build Downloader does not reset a removed selected build type",
	}}}

	match, found := findExistingQABug(board, quality.Bug{Title: "Build Downloader does not reset a removed selected build type!"}, "task-2:bug:1")
	if !found || match.ID != "qa-bug" {
		t.Fatalf("expected normalized title match, got %+v, found=%v", match, found)
	}
}

func TestFindExistingQABugKeepsRequeueSourceReferenceIdempotent(t *testing.T) {
	board := kanban.Board{Backlog: []kanban.BacklogItem{{
		ID: "qa-bug", Key: "BUG-002", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogOpen,
		SourceReference: qaBugReference("task-2", "Original wording"), Title: "Original wording",
	}}}

	match, found := findExistingQABug(board, quality.Bug{ExistingBacklogID: "qa-bug", Title: "Updated wording for the same re-queued finding"}, qaBugReference("task-2", "Updated wording for the same re-queued finding"))
	if !found || match.ID != "qa-bug" {
		t.Fatalf("expected explicit backlog match, got %+v, found=%v", match, found)
	}
}

func TestExistingQABugsIncludesOpenAndPlanningQABugCards(t *testing.T) {
	board := kanban.Board{Backlog: []kanban.BacklogItem{
		{ID: "open", Key: "BUG-001", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogOpen, Title: "Open bug"},
		{ID: "planning", Key: "BUG-002", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogPlanning, Title: "Planning bug"},
		{ID: "done", Key: "BUG-004", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogDone, Title: "Resolved bug"},
		{ID: "manual", Key: "BUG-003", Type: kanban.BacklogBug, Source: "manual", Status: kanban.BacklogOpen, Title: "Manual bug"},
	}}

	bugs := existingQABugs(board)
	if len(bugs) != 2 || bugs[0].BacklogID != "open" || bugs[1].BacklogID != "planning" {
		t.Fatalf("unexpected existing QA bug context: %+v", bugs)
	}
}

func TestExistingQABugsExcludesFinishedPlanningBugCards(t *testing.T) {
	board := kanban.Board{
		Backlog: []kanban.BacklogItem{
			{ID: "finished", Key: "BUG-002", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogPlanning, PlanID: "plan-finished", Title: "Resolved planning bug"},
			{ID: "active", Key: "BUG-003", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogPlanning, PlanID: "plan-active", Title: "Active planning bug"},
		},
		Tasks: []kanban.Task{
			{ID: "finished-dev", PlanID: "plan-finished", Status: kanban.TaskCompleted},
			{ID: "active-dev", PlanID: "plan-active", Status: kanban.TaskCompleted},
			{ID: "active-qa", PlanID: "plan-active", Status: kanban.TaskBlocked},
		},
	}

	bugs := existingQABugs(board)
	if len(bugs) != 1 || bugs[0].BacklogID != "active" {
		t.Fatalf("unexpected existing QA bug context: %+v", bugs)
	}
	if match, found := findExistingQABug(board, quality.Bug{ExistingBacklogID: "finished", Title: "Resolved planning bug"}, "task-2:bug:1"); found {
		t.Fatalf("finished planning bug must not be reused as active duplicate: %+v", match)
	}
}

func TestFindExistingQABugDoesNotUseFindingPositionAsIdentity(t *testing.T) {
	board := kanban.Board{Backlog: []kanban.BacklogItem{{
		ID: "old", Key: "BUG-001", Type: kanban.BacklogBug, Source: "qa", Status: kanban.BacklogOpen,
		SourceReference: "task-2:bug:1", Title: "Old resolved behavior",
	}}}

	if match, found := findExistingQABug(board, quality.Bug{Title: "Different newly discovered behavior"}, "task-2:bug:1"); found {
		t.Fatalf("finding position incorrectly matched a different bug: %+v", match)
	}
}

func TestQABugReferenceIsStableByNormalizedTitle(t *testing.T) {
	first := qaBugReference("task-2", "Check connection ignores edited IDs")
	second := qaBugReference("task-2", "[QA] Check connection ignores edited IDs!")
	if first != second {
		t.Fatalf("normalized bug reference changed: %q != %q", first, second)
	}
}

func TestQABacklogTitleFitsBoardLimitWithoutBreakingUTF8(t *testing.T) {
	title := qaBacklogTitle(strings.Repeat("Thiếu kiểm thử ", 20))
	if len(title) > 120 || !utf8.ValidString(title) || !strings.HasPrefix(title, "[QA] ") {
		t.Fatalf("invalid QA backlog title: bytes=%d valid=%v title=%q", len(title), utf8.ValidString(title), title)
	}
}

func TestQABugResultMessageSeparatesBacklogHighlightsFromPlanningMatches(t *testing.T) {
	message := qaBugResultMessage(2, 0, 1, 1)
	expected := "QA found 2 unresolved reproducible bug(s). Added 0 new Backlog card(s), highlighted 1 existing Backlog card(s), and matched 1 bug(s) already in planning."
	if message != expected {
		t.Fatalf("unexpected QA bug message:\nwant %q\n got %q", expected, message)
	}
}

func TestQABugResultMessageExplainsPlanningOnlyMatch(t *testing.T) {
	message := qaBugResultMessage(1, 0, 0, 1)
	expected := "QA is waiting on 1 unresolved bug(s) already in Team Lead planning. No duplicate Backlog card was created."
	if message != expected {
		t.Fatalf("unexpected QA bug message:\nwant %q\n got %q", expected, message)
	}
}

func TestQAExecutionPersistsBlockedVerdictWithoutFindings(t *testing.T) {
	execution := qaExecution(quality.Result{
		Outcome:      quality.OutcomeBlocked,
		Summary:      "Node 18 runtime is unavailable in the QA environment.",
		Verification: []string{"npm test could not start because the required runtime is unavailable."},
	}, nil)
	if execution.Verdict != "blocked" || len(execution.Findings) != 0 || len(execution.RemainingRisks) != 0 {
		t.Fatalf("unexpected blocked QA execution: %+v", execution)
	}
}

func TestQAExecutionLinksFindingsToBacklogBugTickets(t *testing.T) {
	linked := kanban.BacklogItem{
		ID: "backlog-qa-bug", Key: "BUG-005", Type: kanban.BacklogBug, Source: "qa",
		Status: kanban.BacklogPlanning, PlanID: "plan-bug-fix", Title: "[QA] Approved automation contract is missing",
	}
	execution := qaExecution(quality.Result{
		Outcome: quality.OutcomeFailed,
		Summary: "One reproducible bug remains.",
		Bugs: []quality.Bug{{
			Severity: "high",
			Title:    "Approved automation contract is missing",
		}},
	}, []*kanban.BacklogItem{&linked})

	if len(execution.Findings) != 1 {
		t.Fatalf("expected one linked finding, got %+v", execution.Findings)
	}
	finding := execution.Findings[0]
	if finding.LinkedBacklogID != linked.ID || finding.LinkedBacklogKey != linked.Key || finding.LinkedBacklogTitle != linked.Title || finding.LinkedBacklogStatus != linked.Status || finding.LinkedBacklogPlanID != linked.PlanID {
		t.Fatalf("finding was not linked to the backlog bug ticket: %+v", finding)
	}
}
