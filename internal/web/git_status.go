package web

import (
	"bufio"
	"context"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

const (
	gitStatusOK             = "ok"
	gitStatusUnavailable    = "unavailable"
	gitStatusNotRepository  = "not-a-repository"
	gitCommandTimeout       = 10 * time.Second
	gitMutationTimeout      = 5 * time.Minute
	gitCommitFieldSeparator = "\x1f"
	defaultGitCommitsLimit  = 20
	maxGitCommitsLimit      = 200
)

type gitDirtyFile struct {
	Path       string `json:"path"`
	ChangeType string `json:"changeType"`
}

type gitStatusResponse struct {
	Status string         `json:"status"`
	Branch string         `json:"branch,omitempty"`
	Dirty  []gitDirtyFile `json:"dirty"`
}

type gitCommit struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Subject  string `json:"subject"`
}

type gitCommitsResponse struct {
	Status  string      `json:"status"`
	Commits []gitCommit `json:"commits"`
}

// gitCommandRunner runs a read-only git subcommand scoped to dir. Implementations
// must not shell-interpolate args and must respect ctx cancellation/timeout.
type gitCommandRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

func runGitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	commandArgs := append([]string{"-C", dir}, args...)
	return exec.CommandContext(timeoutCtx, "git", commandArgs...).Output()
}

func runGitCommandCombined(ctx context.Context, dir string, args ...string) ([]byte, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitMutationTimeout)
	defer cancel()
	commandArgs := append([]string{"-C", dir}, args...)
	return exec.CommandContext(timeoutCtx, "git", commandArgs...).CombinedOutput()
}

// gitInspector performs read-only inspection of a repository checkout. It never
// runs a command that mutates the working tree, index, or history.
type gitInspector struct {
	run    gitCommandRunner
	mutate gitCommandRunner
}

func newGitInspector() *gitInspector {
	return &gitInspector{run: runGitCommand, mutate: runGitCommandCombined}
}

func (g *gitInspector) isRepository(ctx context.Context, dir string) bool {
	output, err := g.run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func (g *gitInspector) currentBranch(ctx context.Context, dir string) string {
	output, err := g.run(ctx, dir, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (g *gitInspector) dirtyFiles(ctx context.Context, dir string) []gitDirtyFile {
	files := []gitDirtyFile{}
	output, err := g.run(ctx, dir, "status", "--porcelain=v1")
	if err != nil {
		return files
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}
		changeType := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = path[arrow+len(" -> "):]
		}
		files = append(files, gitDirtyFile{Path: path, ChangeType: changeType})
	}
	return files
}

func (g *gitInspector) recentCommits(ctx context.Context, dir string, limit int) []gitCommit {
	commits := []gitCommit{}
	format := strings.Join([]string{"%H", "%h", "%an", "%ad", "%s"}, gitCommitFieldSeparator)
	output, err := g.run(ctx, dir, "log", "-n", strconv.Itoa(limit), "--pretty=format:"+format, "--date=iso-strict")
	if err != nil {
		return commits
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, gitCommitFieldSeparator, 5)
		if len(parts) != 5 {
			continue
		}
		commits = append(commits, gitCommit{SHA: parts[0], ShortSHA: parts[1], Author: parts[2], Date: parts[3], Subject: parts[4]})
	}
	return commits
}

// commitExists reports whether sha resolves to an object in the repository.
// It never mutates the repository.
func (g *gitInspector) commitExists(ctx context.Context, dir, sha string) bool {
	_, err := g.run(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// branchExists reports whether a local branch with the given name already exists.
func (g *gitInspector) branchExists(ctx context.Context, dir, name string) bool {
	_, err := g.run(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// createBranchFromHead creates a new local branch from the current HEAD. It
// never forces, overwrites, or deletes an existing branch or ref; callers must
// check branchExists first.
func (g *gitInspector) createBranchFromHead(ctx context.Context, dir, name string) error {
	_, err := g.run(ctx, dir, "branch", "--", name)
	return err
}

func parseGitCommitsLimit(raw string) int {
	if raw == "" {
		return defaultGitCommitsLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultGitCommitsLimit
	}
	if value > maxGitCommitsLimit {
		return maxGitCommitsLimit
	}
	return value
}

func (s *server) getProjectGitStatus(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if !s.gitAvailable(r.Context()) {
		writeJSON(w, http.StatusOK, gitStatusResponse{Status: gitStatusUnavailable, Dirty: []gitDirtyFile{}})
		return
	}
	if !s.gitInspector.isRepository(r.Context(), proj.Path) {
		writeJSON(w, http.StatusOK, gitStatusResponse{Status: gitStatusNotRepository, Dirty: []gitDirtyFile{}})
		return
	}
	writeJSON(w, http.StatusOK, gitStatusResponse{
		Status: gitStatusOK,
		Branch: s.gitInspector.currentBranch(r.Context(), proj.Path),
		Dirty:  s.gitInspector.dirtyFiles(r.Context(), proj.Path),
	})
}

func (s *server) getProjectGitCommits(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projectService.findProject(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	limit := parseGitCommitsLimit(r.URL.Query().Get("limit"))
	if !s.gitAvailable(r.Context()) {
		writeJSON(w, http.StatusOK, gitCommitsResponse{Status: gitStatusUnavailable, Commits: []gitCommit{}})
		return
	}
	if !s.gitInspector.isRepository(r.Context(), proj.Path) {
		writeJSON(w, http.StatusOK, gitCommitsResponse{Status: gitStatusNotRepository, Commits: []gitCommit{}})
		return
	}
	writeJSON(w, http.StatusOK, gitCommitsResponse{
		Status:  gitStatusOK,
		Commits: s.gitInspector.recentCommits(r.Context(), proj.Path, limit),
	})
}

// taskGitReferenceRequest carries branch/commit evidence a user explicitly
// supplies for a task. Leaving both fields blank clears any association.
type taskGitReferenceRequest struct {
	Branch    string `json:"branch"`
	CommitSHA string `json:"commitSha"`
	LinkedBy  string `json:"linkedBy"`
}

// taskGitBranchRequest drives the two-step branch-creation flow: an initial
// call with Confirm=false only returns the proposed name; a second call with
// Confirm=true performs the (non-destructive) branch creation.
type taskGitBranchRequest struct {
	Confirm    bool   `json:"confirm"`
	BranchName string `json:"branchName"`
	LinkedBy   string `json:"linkedBy"`
}

const (
	gitBranchStatusProposed = "proposed"
	gitBranchStatusCreated  = "created"
	gitBranchStatusConflict = "conflict"
)

type taskGitBranchResponse struct {
	Status     string `json:"status"`
	BranchName string `json:"branchName"`
}

// taskGitBranchName computes the convention branch name task/{key}-{slug},
// stripping the "[Role]" prefix kanban.FormatTaskTitle adds to task titles.
func taskGitBranchName(task kanban.Task) string {
	title := strings.TrimSpace(task.Title)
	for _, role := range []kanban.AgentRole{kanban.RoleDesigner, kanban.RoleDeveloper, kanban.RoleQA} {
		prefix := "[" + role.Label() + "]"
		if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)) {
			title = strings.TrimSpace(title[len(prefix):])
			break
		}
	}
	return "task/" + task.Key + "-" + workspaceSlug(title)
}

func (s *server) setTaskGitReference(w http.ResponseWriter, r *http.Request) {
	var request taskGitReferenceRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	projectID := r.PathValue("projectID")
	taskID := r.PathValue("taskID")
	branch := strings.TrimSpace(request.Branch)
	commitSHA := strings.TrimSpace(request.CommitSHA)
	if branch == "" && commitSHA == "" {
		s.applyTaskGitReference(w, r, projectID, taskID, nil)
		return
	}
	if commitSHA != "" {
		proj, err := s.projectService.findProject(projectID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if !s.gitAvailable(r.Context()) || !s.gitInspector.isRepository(r.Context(), proj.Path) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Git is unavailable for this project, so a commit SHA cannot be validated."})
			return
		}
		if !s.gitInspector.commitExists(r.Context(), proj.Path, commitSHA) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "That commit SHA was not found in the project repository."})
			return
		}
	}
	s.applyTaskGitReference(w, r, projectID, taskID, &kanban.TaskGitReference{
		Branch:    branch,
		CommitSHA: commitSHA,
		LinkedAt:  time.Now().UTC(),
		LinkedBy:  strings.TrimSpace(request.LinkedBy),
	})
}

func (s *server) clearTaskGitReference(w http.ResponseWriter, r *http.Request) {
	s.applyTaskGitReference(w, r, r.PathValue("projectID"), r.PathValue("taskID"), nil)
}

func (s *server) applyTaskGitReference(w http.ResponseWriter, r *http.Request, projectID, taskID string, reference *kanban.TaskGitReference) {
	board, err := s.boards.SetTaskGitReference(r.Context(), projectID, taskID, reference)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(board)
	writeJSON(w, http.StatusOK, board)
}

// createTaskGitBranch implements the two-step, explicit-confirmation branch
// creation flow. It never forces, overwrites, or deletes an existing branch.
func (s *server) createTaskGitBranch(w http.ResponseWriter, r *http.Request) {
	var request taskGitBranchRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	projectID := r.PathValue("projectID")
	taskID := r.PathValue("taskID")
	proj, err := s.projectService.findProject(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	board, err := s.boards.GetProjectBoard(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	taskIndex := findBoardTask(board, taskID)
	if taskIndex < 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	task := board.Tasks[taskIndex]
	if !s.gitAvailable(r.Context()) || !s.gitInspector.isRepository(r.Context(), proj.Path) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Git is unavailable for this project."})
		return
	}
	proposedName := taskGitBranchName(task)
	if !request.Confirm {
		writeJSON(w, http.StatusOK, taskGitBranchResponse{Status: gitBranchStatusProposed, BranchName: proposedName})
		return
	}
	branchName := strings.TrimSpace(request.BranchName)
	if branchName == "" {
		branchName = proposedName
	}
	if s.gitInspector.branchExists(r.Context(), proj.Path, branchName) {
		writeJSON(w, http.StatusConflict, taskGitBranchResponse{Status: gitBranchStatusConflict, BranchName: branchName})
		return
	}
	if err := s.gitInspector.createBranchFromHead(r.Context(), proj.Path, branchName); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Failed to create the branch: " + err.Error()})
		return
	}
	reference := &kanban.TaskGitReference{
		Branch:   branchName,
		LinkedAt: time.Now().UTC(),
		LinkedBy: strings.TrimSpace(request.LinkedBy),
	}
	if task.GitReference != nil {
		reference.CommitSHA = task.GitReference.CommitSHA
	}
	updatedBoard, err := s.boards.SetTaskGitReference(r.Context(), projectID, taskID, reference)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.boardEvents.publish(updatedBoard)
	writeJSON(w, http.StatusCreated, taskGitBranchResponse{Status: gitBranchStatusCreated, BranchName: branchName})
}
