package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func newGitTestServer(t *testing.T, projectDirectory string, gitAvailable func(context.Context) bool) (*server, project) {
	t.Helper()
	dataDirectory := t.TempDir()
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return &server{projectService: projects, gitInspector: newGitInspector(), gitAvailable: gitAvailable}, proj
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}

func initRepoWithCommit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial commit")
}

func doGitRequest(t *testing.T, handler http.HandlerFunc, projectID, query string) (int, map[string]any) {
	t.Helper()
	path := "/git"
	if query != "" {
		path += "?" + query
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("projectID", projectID)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
	}
	return recorder.Code, decoded
}

func TestGitStatusReportsCleanRepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepoWithCommit(t, dir)
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return true })

	code, body := doGitRequest(t, srv.getProjectGitStatus, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusOK {
		t.Fatalf("expected ok status, got %v", body["status"])
	}
	dirty, _ := body["dirty"].([]any)
	if len(dirty) != 0 {
		t.Fatalf("expected no dirty files, got %v", body["dirty"])
	}
	if body["branch"] == nil || body["branch"] == "" {
		t.Fatalf("expected a branch name, got %v", body["branch"])
	}
}

func TestGitStatusReportsDirtyFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dirty-repo")
	initRepoWithCommit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return true })

	code, body := doGitRequest(t, srv.getProjectGitStatus, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusOK {
		t.Fatalf("expected ok status, got %v", body["status"])
	}
	dirty, _ := body["dirty"].([]any)
	if len(dirty) != 2 {
		t.Fatalf("expected 2 dirty files, got %v", body["dirty"])
	}
}

func TestGitStatusReportsNotARepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain-folder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return true })

	code, body := doGitRequest(t, srv.getProjectGitStatus, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusNotRepository {
		t.Fatalf("expected not-a-repository status, got %v", body["status"])
	}
}

func TestGitStatusReportsUnavailableWhenGitMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "any-folder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return false })

	code, body := doGitRequest(t, srv.getProjectGitStatus, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusUnavailable {
		t.Fatalf("expected unavailable status, got %v", body["status"])
	}
}

func TestGitCommitsReturnsRecentHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history-repo")
	initRepoWithCommit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "second.txt")
	runGit(t, dir, "commit", "-m", "second commit")
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return true })

	code, body := doGitRequest(t, srv.getProjectGitCommits, proj.ID, "limit=1")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusOK {
		t.Fatalf("expected ok status, got %v", body["status"])
	}
	commits, _ := body["commits"].([]any)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit due to limit, got %v", body["commits"])
	}
	first, _ := commits[0].(map[string]any)
	if first["subject"] != "second commit" {
		t.Fatalf("expected most recent commit first, got %v", first["subject"])
	}
}

func TestGitCommitsReportsNotARepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain-folder-2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return true })

	code, body := doGitRequest(t, srv.getProjectGitCommits, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusNotRepository {
		t.Fatalf("expected not-a-repository status, got %v", body["status"])
	}
}

func TestGitCommitsReportsUnavailableWhenGitMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history-repo-2")
	initRepoWithCommit(t, dir)
	srv, proj := newGitTestServer(t, dir, func(context.Context) bool { return false })

	code, body := doGitRequest(t, srv.getProjectGitCommits, proj.ID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	if body["status"] != gitStatusUnavailable {
		t.Fatalf("expected unavailable status, got %v", body["status"])
	}
}

// newTaskGitTestServer builds a server backed by a real git repository and a
// board containing a single task, for exercising task-git association and
// branch-creation endpoints.
func newTaskGitTestServer(t *testing.T, repoDirectory string) (*server, project, string) {
	t.Helper()
	dataDirectory := t.TempDir()
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := projects.importLocal(repoDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	taskID := "task-git-1"
	board := kanban.Board{
		ID: "board-git", ProjectID: proj.ID, ProjectName: proj.Name, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Backlog: []kanban.BacklogItem{}, Plans: []kanban.Plan{},
		Tasks: []kanban.Task{{
			ID: taskID, Key: "DEV-040", Title: "[Developer] Add task-to-branch association",
			Role: kanban.RoleDeveloper, Column: kanban.ColumnDeveloper, Status: kanban.TaskInProgress,
		}},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}
	srv := &server{
		projectService: projects,
		gitInspector:   newGitInspector(),
		gitAvailable:   func(context.Context) bool { return true },
		boards:         kanban.NewService(repository),
		boardEvents:    newBoardEventHub(),
	}
	return srv, proj, taskID
}

func doTaskGitJSONRequest(t *testing.T, handler http.HandlerFunc, method, projectID, taskID string, payload any) (int, map[string]any) {
	t.Helper()
	var body *bytes.Buffer
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewBuffer(encoded)
	} else {
		body = bytes.NewBuffer(nil)
	}
	request := httptest.NewRequest(method, "/git", body)
	request.SetPathValue("projectID", projectID)
	request.SetPathValue("taskID", taskID)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	decoded := map[string]any{}
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
		}
	}
	return recorder.Code, decoded
}

func TestSetTaskGitReferenceAcceptsValidCommitSHA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-git-valid")
	initRepoWithCommit(t, dir)
	srv, proj, taskID := newTaskGitTestServer(t, dir)
	sha := strings.TrimSpace(runGitOutput(t, dir, "rev-parse", "HEAD"))

	code, body := doTaskGitJSONRequest(t, srv.setTaskGitReference, http.MethodPut, proj.ID, taskID, taskGitReferenceRequest{
		Branch: "main", CommitSHA: sha, LinkedBy: "tester",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	task := findTaskInBoardBody(t, body, taskID)
	gitReference, _ := task["gitReference"].(map[string]any)
	if gitReference["commitSha"] != sha {
		t.Fatalf("expected commitSha %q, got %v", sha, gitReference["commitSha"])
	}
	if gitReference["branch"] != "main" {
		t.Fatalf("expected branch main, got %v", gitReference["branch"])
	}
}

func TestSetTaskGitReferenceRejectsUnknownCommitSHA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-git-invalid")
	initRepoWithCommit(t, dir)
	srv, proj, taskID := newTaskGitTestServer(t, dir)

	code, body := doTaskGitJSONRequest(t, srv.setTaskGitReference, http.MethodPut, proj.ID, taskID, taskGitReferenceRequest{
		CommitSHA: "0000000000000000000000000000000000000000",
	})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %v", code, body)
	}
	if body["error"] == nil {
		t.Fatalf("expected an error message, got %v", body)
	}

	// Nothing should have been persisted.
	board, err := srv.boards.GetProjectBoard(context.Background(), proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range board.Tasks {
		if task.ID == taskID && task.GitReference != nil {
			t.Fatalf("expected no git reference to be persisted, got %+v", task.GitReference)
		}
	}
}

func TestClearTaskGitReferenceRemovesAssociation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-git-clear")
	initRepoWithCommit(t, dir)
	srv, proj, taskID := newTaskGitTestServer(t, dir)

	code, _ := doTaskGitJSONRequest(t, srv.setTaskGitReference, http.MethodPut, proj.ID, taskID, taskGitReferenceRequest{Branch: "main"})
	if code != http.StatusOK {
		t.Fatalf("expected 200 setting up the association, got %d", code)
	}

	code, body := doTaskGitJSONRequest(t, srv.clearTaskGitReference, http.MethodDelete, proj.ID, taskID, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, body)
	}
	task := findTaskInBoardBody(t, body, taskID)
	if task["gitReference"] != nil {
		t.Fatalf("expected git reference to be cleared, got %v", task["gitReference"])
	}
}

func TestCreateTaskGitBranchProposesThenCreatesOnConfirm(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-git-branch")
	initRepoWithCommit(t, dir)
	srv, proj, taskID := newTaskGitTestServer(t, dir)

	code, body := doTaskGitJSONRequest(t, srv.createTaskGitBranch, http.MethodPost, proj.ID, taskID, taskGitBranchRequest{})
	if code != http.StatusOK {
		t.Fatalf("expected 200 proposing branch name, got %d: %v", code, body)
	}
	if body["status"] != gitBranchStatusProposed {
		t.Fatalf("expected proposed status, got %v", body["status"])
	}
	branchName, _ := body["branchName"].(string)
	if branchName != "task/DEV-040-add-task-to-branch-association" {
		t.Fatalf("unexpected proposed branch name: %q", branchName)
	}
	if branchExists(t, dir, branchName) {
		t.Fatalf("branch must not be created before confirmation")
	}

	code, body = doTaskGitJSONRequest(t, srv.createTaskGitBranch, http.MethodPost, proj.ID, taskID, taskGitBranchRequest{Confirm: true, BranchName: branchName})
	if code != http.StatusCreated {
		t.Fatalf("expected 201 creating branch, got %d: %v", code, body)
	}
	if body["status"] != gitBranchStatusCreated {
		t.Fatalf("expected created status, got %v", body["status"])
	}
	if !branchExists(t, dir, branchName) {
		t.Fatalf("expected branch %q to exist", branchName)
	}
}

func TestCreateTaskGitBranchReportsCollisionWithoutOverwriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "task-git-branch-collision")
	initRepoWithCommit(t, dir)
	srv, proj, taskID := newTaskGitTestServer(t, dir)
	existingBranchName := "task/DEV-040-add-task-to-branch-association"
	runGit(t, dir, "branch", existingBranchName)
	preCollisionSHA := strings.TrimSpace(runGitOutput(t, dir, "rev-parse", existingBranchName))

	code, body := doTaskGitJSONRequest(t, srv.createTaskGitBranch, http.MethodPost, proj.ID, taskID, taskGitBranchRequest{Confirm: true, BranchName: existingBranchName})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d: %v", code, body)
	}
	if body["status"] != gitBranchStatusConflict {
		t.Fatalf("expected conflict status, got %v", body["status"])
	}
	if got := strings.TrimSpace(runGitOutput(t, dir, "rev-parse", existingBranchName)); got != preCollisionSHA {
		t.Fatalf("expected existing branch to be untouched, sha changed from %q to %q", preCollisionSHA, got)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return string(output)
}

func branchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

func findTaskInBoardBody(t *testing.T, body map[string]any, taskID string) map[string]any {
	t.Helper()
	tasks, _ := body["tasks"].([]any)
	for _, candidate := range tasks {
		task, _ := candidate.(map[string]any)
		if task["id"] == taskID {
			return task
		}
	}
	t.Fatalf("task %q not found in board response: %v", taskID, body)
	return nil
}
