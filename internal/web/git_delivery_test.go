package web

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func TestDeliveryTicketsUsesOnlyCompletedDoneTaskEvidence(t *testing.T) {
	root := `C:\workspace\product`
	board := kanban.Board{Tasks: []kanban.Task{
		{ID: "dev-1", Key: "DEV-039", Title: "[Developer] Add installer update flow", Role: kanban.RoleDeveloper, Column: kanban.ColumnDone, Status: kanban.TaskCompleted, Execution: &kanban.TaskExecution{ChangedFiles: []string{"frontend/a.ts", filepath.Join(root, "internal", "b.go")}}},
		{ID: "dev-2", Key: "DEV-040", Title: "Queued work", Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskQueued, Execution: &kanban.TaskExecution{ChangedFiles: []string{"ignored.go"}}},
	}}

	tickets, unattributed := deliveryTickets(board, root, []string{"frontend/a.ts", "internal/b.go", "README.md"})
	if len(tickets) != 1 || tickets[0].Key != "DEV-039" {
		t.Fatalf("expected one completed ticket, got %#v", tickets)
	}
	if !reflect.DeepEqual(tickets[0].Files, []string{"frontend/a.ts", "internal/b.go"}) {
		t.Fatalf("unexpected attributed files: %#v", tickets[0].Files)
	}
	if !reflect.DeepEqual(unattributed, []string{"README.md"}) {
		t.Fatalf("unexpected unattributed files: %#v", unattributed)
	}
}

func TestGitDeliveryPreviewRequiresExactCompletedTicketAttribution(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	initRepoWithCommit(t, repoDir)
	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remoteDir, "init", "--bare")
	runGit(t, repoDir, "remote", "add", "origin", remoteDir)
	if err := os.WriteFile(filepath.Join(repoDir, "feature.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	projects, err := newProjectService(dataDir, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	boardRepo, err := storage.NewJSONBoardRepository(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	board := kanban.Board{ID: "board", ProjectID: project.ID, ProjectName: project.Name, Tasks: []kanban.Task{{ID: "dev-1", Key: "DEV-039", Title: "[Developer] Add feature", Role: kanban.RoleDeveloper, Column: kanban.ColumnDone, Status: kanban.TaskCompleted, Execution: &kanban.TaskExecution{ChangedFiles: []string{"feature.go"}}}}}
	if err := boardRepo.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	srv := &server{projectService: projects, gitInspector: newGitInspector(), gitAvailable: func(context.Context) bool { return true }, boards: kanban.NewService(boardRepo)}

	preview, err := srv.buildGitDeliveryPreview(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Ready {
		t.Fatalf("expected ready preview, blockers: %v", preview.Blockers)
	}
	if preview.CommitMessage != "chore: deliver DEV-039" || len(preview.Tickets) != 1 || len(preview.Files) != 1 {
		t.Fatalf("unexpected preview: %#v", preview)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "unowned.txt"), []byte("unowned"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked, err := srv.buildGitDeliveryPreview(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Ready || !reflect.DeepEqual(blocked.Unattributed, []string{"unowned.txt"}) {
		t.Fatalf("expected unattributed change to block delivery: %#v", blocked)
	}
}

func TestGitDeliveryFingerprintChangesWithApprovedScope(t *testing.T) {
	tickets := []gitdelivery.Ticket{{TaskID: "dev-1", Key: "DEV-039", Files: []string{"a.go"}}}
	first := gitDeliveryFingerprint("head", "main", []string{"a.go"}, tickets)
	second := gitDeliveryFingerprint("head", "main", []string{"a.go", "b.go"}, tickets)
	if first == second {
		t.Fatal("fingerprint must change when approved file scope changes")
	}
}

func TestRepairableGitDeliveryFailureRejectsCredentialAndHistoryFailures(t *testing.T) {
	for _, message := range []string{"authentication failed", "non-fast-forward update rejected", "protected branch hook declined"} {
		if repairableGitDeliveryFailure(message) {
			t.Fatalf("must not send unsafe failure to AI recovery: %q", message)
		}
	}
	if !repairableGitDeliveryFailure("pre-push hook failed: npm test") {
		t.Fatal("expected test-hook failure to be repairable")
	}
}
