package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/development"
	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

const maxGitDeliveryEvents = 200

var gitDeliverySequence atomic.Uint64

type gitDeliveryPreview struct {
	ProjectID      string                `json:"projectId"`
	ProjectName    string                `json:"projectName"`
	Branch         string                `json:"branch"`
	Remote         string                `json:"remote"`
	HeadSHA        string                `json:"headSha"`
	Fingerprint    string                `json:"fingerprint"`
	CommitMessage  string                `json:"commitMessage"`
	Tickets        []gitdelivery.Ticket  `json:"tickets"`
	Files          []string              `json:"files"`
	StagedFiles    []string              `json:"stagedFiles"`
	Unattributed   []string              `json:"unattributedFiles"`
	Ready          bool                  `json:"ready"`
	Blockers       []string              `json:"blockers"`
	LatestDelivery *gitdelivery.Delivery `json:"latestDelivery,omitempty"`
}

type startGitDeliveryRequest struct {
	Fingerprint string `json:"fingerprint"`
}

func (s *server) getGitDeliveryPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := s.buildGitDeliveryPreview(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if recent, listErr := s.gitDeliveries.List(r.Context(), preview.ProjectID, 1); listErr == nil && len(recent) > 0 {
		preview.LatestDelivery = &recent[0]
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *server) listGitDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, err := s.projectService.findProject(r.PathValue("projectID")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	limit := 20
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = value
	}
	deliveries, err := s.gitDeliveries.List(r.Context(), r.PathValue("projectID"), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *server) getGitDelivery(w http.ResponseWriter, r *http.Request) {
	delivery, err := s.gitDeliveries.Get(r.Context(), r.PathValue("deliveryID"))
	if errors.Is(err, gitdelivery.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git delivery not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if delivery.ProjectID != r.PathValue("projectID") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git delivery not found"})
		return
	}
	writeJSON(w, http.StatusOK, delivery)
}

func (s *server) stopGitDelivery(w http.ResponseWriter, r *http.Request) {
	delivery, err := s.gitDeliveries.Get(r.Context(), r.PathValue("deliveryID"))
	if err != nil || delivery.ProjectID != r.PathValue("projectID") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git delivery not found"})
		return
	}
	if delivery.Status != gitdelivery.StatusQueued && delivery.Status != gitdelivery.StatusRunning && delivery.Status != gitdelivery.StatusRecovering {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Only an active delivery can be stopped."})
		return
	}
	delivery.StopRequested = true
	s.appendGitDeliveryEvent(&delivery, "warning", delivery.Step, "Safe stop requested", "ProductCrew will stop before the next Git mutation.")
	if err := s.gitDeliveries.Upsert(r.Context(), delivery); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, delivery)
}

func (s *server) retryGitDelivery(w http.ResponseWriter, r *http.Request) {
	delivery, err := s.gitDeliveries.Get(r.Context(), r.PathValue("deliveryID"))
	if err != nil || delivery.ProjectID != r.PathValue("projectID") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git delivery not found"})
		return
	}
	if delivery.Status != gitdelivery.StatusFailed && delivery.Status != gitdelivery.StatusPaused {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Only a failed or safely stopped delivery can be retried."})
		return
	}
	delivery.StopRequested = false
	delivery.CompletedAt = nil
	delivery.Error = ""
	delivery.Status = gitdelivery.StatusQueued
	s.appendGitDeliveryEvent(&delivery, "info", delivery.Step, "Retry queued", "The persisted delivery will resume without creating a duplicate commit.")
	if err := s.gitDeliveries.Upsert(r.Context(), delivery); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	go s.resumeGitDelivery(delivery.ID)
	writeJSON(w, http.StatusAccepted, delivery)
}

func (s *server) startGitDelivery(w http.ResponseWriter, r *http.Request) {
	s.gitDeliveryMu.Lock()
	defer s.gitDeliveryMu.Unlock()
	var request startGitDeliveryRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	projectID := r.PathValue("projectID")
	if active, err := s.gitDeliveries.List(r.Context(), projectID, 10); err == nil {
		for _, delivery := range active {
			if delivery.Status == gitdelivery.StatusQueued || delivery.Status == gitdelivery.StatusRunning || delivery.Status == gitdelivery.StatusRecovering {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "A Git delivery is already running for this project.", "deliveryId": delivery.ID})
				return
			}
		}
	}
	preview, err := s.buildGitDeliveryPreview(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if !preview.Ready {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Git delivery preflight is not ready.", "preview": preview})
		return
	}
	if strings.TrimSpace(request.Fingerprint) == "" || request.Fingerprint != preview.Fingerprint {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Repository state changed. Review the refreshed delivery before continuing.", "preview": preview})
		return
	}
	now := time.Now().UTC()
	delivery := gitdelivery.Delivery{
		ID: fmt.Sprintf("DL-%06d-%x", gitDeliverySequence.Add(1), now.UnixNano()), ProjectID: preview.ProjectID,
		ProjectName: preview.ProjectName, Branch: preview.Branch, Remote: preview.Remote,
		Status: gitdelivery.StatusQueued, Step: gitdelivery.StepPreflight, HeadBefore: preview.HeadSHA,
		CommitMessage: preview.CommitMessage, Fingerprint: preview.Fingerprint, Tickets: preview.Tickets,
		Files: preview.Files, Events: []gitdelivery.Event{}, StartedAt: now, UpdatedAt: now,
	}
	s.appendGitDeliveryEvent(&delivery, "info", gitdelivery.StepPreflight, "Delivery queued", "Repository state will be verified again before staging files.")
	if err := s.gitDeliveries.Upsert(r.Context(), delivery); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.recordGitDeliveryEvent(delivery, "git.delivery.queued", "Git delivery queued", "queued")
	go s.runGitDelivery(delivery.ID)
	writeJSON(w, http.StatusAccepted, delivery)
}

func (s *server) buildGitDeliveryPreview(ctx context.Context, projectID string) (gitDeliveryPreview, error) {
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		return gitDeliveryPreview{}, err
	}
	preview := gitDeliveryPreview{ProjectID: project.ID, ProjectName: project.Name, Remote: "origin", Tickets: []gitdelivery.Ticket{}, Files: []string{}, StagedFiles: []string{}, Unattributed: []string{}, Blockers: []string{}}
	if project.Kind == "workspace" {
		preview.Blockers = append(preview.Blockers, "Workspace delivery is not available yet. Open a single project repository.")
		return preview, nil
	}
	if !s.gitAvailable(ctx) || !s.gitInspector.isRepository(ctx, project.Path) {
		preview.Blockers = append(preview.Blockers, "Git is unavailable or this project is not a Git repository.")
		return preview, nil
	}
	preview.Branch = s.gitInspector.currentBranch(ctx, project.Path)
	if preview.Branch == "" {
		preview.Blockers = append(preview.Blockers, "Detached HEAD cannot be delivered safely. Check out a branch first.")
	}
	head, headErr := s.gitInspector.run(ctx, project.Path, "rev-parse", "HEAD")
	if headErr != nil {
		preview.Blockers = append(preview.Blockers, "The repository needs an initial commit before ProductCrew can deliver changes.")
		return preview, nil
	}
	preview.HeadSHA = strings.TrimSpace(string(head))
	if _, remoteErr := s.gitInspector.run(ctx, project.Path, "remote", "get-url", preview.Remote); remoteErr != nil {
		preview.Blockers = append(preview.Blockers, "Remote origin is not configured.")
	}
	staged, unstaged, untracked, pathsErr := s.gitInspector.deliveryPaths(ctx, project.Path)
	if pathsErr != nil {
		return preview, pathsErr
	}
	preview.StagedFiles = staged
	preview.Files = uniqueSortedPaths(append(append([]string{}, unstaged...), untracked...))
	if len(staged) > 0 {
		preview.Blockers = append(preview.Blockers, "Existing staged changes are protected. Unstage or commit them before starting a ProductCrew delivery.")
	}
	if len(preview.Files) == 0 {
		preview.Blockers = append(preview.Blockers, "There are no unstaged or untracked changes to deliver.")
	}
	board, boardErr := s.boards.GetProjectBoard(ctx, projectID)
	if boardErr != nil {
		return preview, fmt.Errorf("load project board: %w", boardErr)
	}
	preview.Tickets, preview.Unattributed = deliveryTickets(board, project.Path, preview.Files)
	if len(preview.Tickets) == 0 && len(preview.Files) > 0 {
		preview.Blockers = append(preview.Blockers, "No completed ticket reports these changed files.")
	}
	if len(preview.Unattributed) > 0 {
		preview.Blockers = append(preview.Blockers, fmt.Sprintf("%d changed file(s) are not attributed to a completed ticket.", len(preview.Unattributed)))
	}
	preview.CommitMessage = gitDeliveryCommitMessage(preview.Tickets)
	preview.Fingerprint = gitDeliveryFingerprint(preview.HeadSHA, preview.Branch, preview.Files, preview.Tickets)
	preview.Ready = len(preview.Blockers) == 0
	return preview, nil
}

func (g *gitInspector) deliveryPaths(ctx context.Context, dir string) ([]string, []string, []string, error) {
	read := func(args ...string) ([]string, error) {
		output, err := g.run(ctx, dir, args...)
		if err != nil {
			return nil, err
		}
		return splitNullPaths(output), nil
	}
	staged, err := read("diff", "--cached", "--name-only", "-z", "--diff-filter=ACDMRTUXB")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("inspect staged changes: %w", err)
	}
	unstaged, err := read("diff", "--name-only", "-z", "--diff-filter=ACDMRTUXB")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("inspect unstaged changes: %w", err)
	}
	untracked, err := read("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("inspect untracked files: %w", err)
	}
	return uniqueSortedPaths(staged), uniqueSortedPaths(unstaged), uniqueSortedPaths(untracked), nil
}

func splitNullPaths(output []byte) []string {
	parts := strings.Split(string(output), "\x00")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if path := normalizeGitPath(part); path != "" {
			result = append(result, path)
		}
	}
	return result
}

func normalizeGitPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if value == "." || strings.HasPrefix(value, "../") {
		return ""
	}
	return value
}

func uniqueSortedPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if normalized := normalizeGitPath(value); normalized != "" {
			seen[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func deliveryTickets(board kanban.Board, projectPath string, changed []string) ([]gitdelivery.Ticket, []string) {
	changedSet := make(map[string]struct{}, len(changed))
	for _, path := range changed {
		changedSet[path] = struct{}{}
	}
	attributed := make(map[string]struct{})
	tickets := make([]gitdelivery.Ticket, 0)
	for _, task := range board.Tasks {
		if task.Status != kanban.TaskCompleted || task.Column != kanban.ColumnDone || task.Execution == nil {
			continue
		}
		files := make([]string, 0)
		for _, raw := range task.Execution.ChangedFiles {
			path := normalizeTaskChangedFile(projectPath, raw)
			if _, exists := changedSet[path]; exists {
				files = append(files, path)
				attributed[path] = struct{}{}
			}
		}
		files = uniqueSortedPaths(files)
		if len(files) == 0 {
			continue
		}
		tickets = append(tickets, gitdelivery.Ticket{TaskID: task.ID, Key: task.Key, Title: strings.TrimSpace(strings.TrimPrefix(task.Title, "["+task.Role.Label()+"]")), Files: files})
	}
	sort.Slice(tickets, func(i, j int) bool { return tickets[i].Key < tickets[j].Key })
	unattributed := make([]string, 0)
	for _, path := range changed {
		if _, exists := attributed[path]; !exists {
			unattributed = append(unattributed, path)
		}
	}
	return tickets, unattributed
}

func normalizeTaskChangedFile(projectPath, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		if relative, err := filepath.Rel(projectPath, value); err == nil {
			value = relative
		}
	}
	return normalizeGitPath(value)
}

func gitDeliveryCommitMessage(tickets []gitdelivery.Ticket) string {
	keys := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		keys = append(keys, ticket.Key)
	}
	return "chore: deliver " + strings.Join(keys, ", ")
}

func gitDeliveryCommitBody(delivery gitdelivery.Delivery) string {
	lines := []string{"Delivered by ProductCrew.", ""}
	for _, ticket := range delivery.Tickets {
		lines = append(lines, "- "+ticket.Key+": "+ticket.Title)
	}
	lines = append(lines, "", "ProductCrew-Delivery: "+delivery.ID)
	for _, ticket := range delivery.Tickets {
		lines = append(lines, "ProductCrew-Task: "+ticket.Key)
	}
	return strings.Join(lines, "\n")
}

func gitDeliveryFingerprint(head, branch string, files []string, tickets []gitdelivery.Ticket) string {
	data, _ := json.Marshal(struct {
		Head    string
		Branch  string
		Files   []string
		Tickets []gitdelivery.Ticket
	}{head, branch, files, tickets})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *server) runGitDelivery(deliveryID string) {
	s.gitDeliveryMu.Lock()
	defer s.gitDeliveryMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	delivery, err := s.gitDeliveries.Get(ctx, deliveryID)
	if err != nil {
		return
	}
	project, err := s.projectService.findProject(delivery.ProjectID)
	if err != nil {
		s.failGitDelivery(ctx, &delivery, gitdelivery.StepPreflight, err)
		return
	}
	delivery.Status = gitdelivery.StatusRunning
	s.appendGitDeliveryEvent(&delivery, "info", gitdelivery.StepPreflight, "Preflight started", "Validating repository state, tickets, and protected staged work.")
	_ = s.gitDeliveries.Upsert(ctx, delivery)
	preview, err := s.buildGitDeliveryPreview(ctx, delivery.ProjectID)
	if err != nil || !preview.Ready || preview.Fingerprint != delivery.Fingerprint {
		if err == nil {
			err = fmt.Errorf("repository state changed after delivery approval")
		}
		s.failGitDelivery(ctx, &delivery, gitdelivery.StepPreflight, err)
		return
	}
	s.appendGitDeliveryEvent(&delivery, "info", gitdelivery.StepPreflight, "Preflight completed", "Repository state matches the approved delivery fingerprint.")
	if err := s.gitDeliveries.Upsert(ctx, delivery); err != nil {
		return
	}
	if s.pauseGitDeliveryIfRequested(ctx, &delivery) {
		return
	}

	if err := s.createGitDeliveryCommit(ctx, project, &delivery, false); err != nil {
		if s.tryGitDeliveryRecovery(ctx, project, &delivery, gitdelivery.StepCommit, err) {
			if retryErr := s.createGitDeliveryCommit(ctx, project, &delivery, false); retryErr == nil {
				s.pushGitDelivery(ctx, project, &delivery)
				return
			} else {
				err = retryErr
			}
		}
		s.failGitDelivery(ctx, &delivery, gitdelivery.StepCommit, err)
		return
	}
	if s.pauseGitDeliveryIfRequested(ctx, &delivery) {
		return
	}
	s.pushGitDelivery(ctx, project, &delivery)
}

func (s *server) resumeGitDelivery(deliveryID string) {
	s.gitDeliveryMu.Lock()
	defer s.gitDeliveryMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	delivery, err := s.gitDeliveries.Get(ctx, deliveryID)
	if err != nil {
		return
	}
	project, err := s.projectService.findProject(delivery.ProjectID)
	if err != nil {
		s.failGitDelivery(ctx, &delivery, delivery.Step, err)
		return
	}
	if delivery.CommitSHA == "" {
		preview, previewErr := s.buildGitDeliveryPreview(ctx, delivery.ProjectID)
		if previewErr != nil || preview.Fingerprint != delivery.Fingerprint {
			if previewErr == nil {
				previewErr = fmt.Errorf("repository state changed; start a new reviewed delivery")
			}
			s.failGitDelivery(ctx, &delivery, gitdelivery.StepPreflight, previewErr)
			return
		}
		s.runGitDeliveryUnlocked(ctx, project, &delivery)
		return
	}
	head, headErr := s.gitInspector.run(ctx, project.Path, "rev-parse", "HEAD")
	if headErr != nil || strings.TrimSpace(string(head)) != delivery.CommitSHA {
		s.failGitDelivery(ctx, &delivery, gitdelivery.StepCommit, fmt.Errorf("current HEAD no longer matches delivery commit %s", delivery.ShortSHA))
		return
	}
	s.pushGitDelivery(ctx, project, &delivery)
}

func (s *server) runGitDeliveryUnlocked(ctx context.Context, project project, delivery *gitdelivery.Delivery) {
	delivery.Status = gitdelivery.StatusRunning
	if err := s.createGitDeliveryCommit(ctx, project, delivery, false); err != nil {
		s.failGitDelivery(ctx, delivery, gitdelivery.StepCommit, err)
		return
	}
	if s.pauseGitDeliveryIfRequested(ctx, delivery) {
		return
	}
	s.pushGitDelivery(ctx, project, delivery)
}

func (s *server) pauseGitDeliveryIfRequested(ctx context.Context, delivery *gitdelivery.Delivery) bool {
	current, err := s.gitDeliveries.Get(ctx, delivery.ID)
	if err != nil || !current.StopRequested {
		return false
	}
	delivery.StopRequested = true
	delivery.Status = gitdelivery.StatusPaused
	delivery.Error = ""
	now := time.Now().UTC()
	delivery.CompletedAt = &now
	s.appendGitDeliveryEvent(delivery, "warning", delivery.Step, "Delivery safely stopped", "No next Git mutation was started. Use Retry to continue this delivery.")
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	return true
}

func (s *server) createGitDeliveryCommit(ctx context.Context, project project, delivery *gitdelivery.Delivery, amend bool) error {
	delivery.Step = gitdelivery.StepCommit
	s.appendGitDeliveryEvent(delivery, "info", gitdelivery.StepCommit, "Creating commit", fmt.Sprintf("Staging %d approved file(s).", len(delivery.Files)))
	if err := s.gitDeliveries.Upsert(ctx, *delivery); err != nil {
		return err
	}
	args := append([]string{"add", "--"}, delivery.Files...)
	if output, err := s.gitInspector.mutate(ctx, project.Path, args...); err != nil {
		return fmt.Errorf("stage approved files: %s", safeGitDetail(output, err))
	}
	commitArgs := []string{"commit", "-m", delivery.CommitMessage, "-m", gitDeliveryCommitBody(*delivery)}
	if amend {
		commitArgs = []string{"commit", "--amend", "--no-edit"}
	}
	output, err := s.gitInspector.mutate(ctx, project.Path, commitArgs...)
	if err != nil {
		_, _ = s.gitInspector.mutate(ctx, project.Path, append([]string{"reset", "-q", "HEAD", "--"}, delivery.Files...)...)
		return fmt.Errorf("create commit: %s", safeGitDetail(output, err))
	}
	shaOutput, err := s.gitInspector.run(ctx, project.Path, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read created commit: %w", err)
	}
	delivery.CommitSHA = strings.TrimSpace(string(shaOutput))
	if len(delivery.CommitSHA) > 7 {
		delivery.ShortSHA = delivery.CommitSHA[:7]
	} else {
		delivery.ShortSHA = delivery.CommitSHA
	}
	s.appendGitDeliveryEvent(delivery, "info", gitdelivery.StepCommit, "Commit created", "SHA "+delivery.ShortSHA)
	return s.gitDeliveries.Upsert(ctx, *delivery)
}

func (s *server) pushGitDelivery(ctx context.Context, project project, delivery *gitdelivery.Delivery) {
	delivery.Status = gitdelivery.StatusRunning
	delivery.Step = gitdelivery.StepPush
	s.appendGitDeliveryEvent(delivery, "info", gitdelivery.StepPush, "Push started", "Pushing the recorded commit to origin/"+delivery.Branch+" without force.")
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	output, err := s.gitInspector.mutate(ctx, project.Path, "push", "--porcelain", delivery.Remote, "HEAD:refs/heads/"+delivery.Branch)
	if err != nil && !s.remoteHasGitDeliveryCommit(ctx, project.Path, delivery) {
		pushErr := fmt.Errorf("push commit: %s", safeGitDetail(output, err))
		if s.tryGitDeliveryRecovery(ctx, project, delivery, gitdelivery.StepPush, pushErr) {
			if amendErr := s.createGitDeliveryCommit(ctx, project, delivery, true); amendErr != nil {
				s.failGitDelivery(ctx, delivery, gitdelivery.StepCommit, amendErr)
				return
			}
			delivery.Step = gitdelivery.StepPush
			s.appendGitDeliveryEvent(delivery, "info", gitdelivery.StepPush, "Push retry started", "Retrying the same delivery after verified recovery changes.")
			_ = s.gitDeliveries.Upsert(ctx, *delivery)
			output, err = s.gitInspector.mutate(ctx, project.Path, "push", "--porcelain", delivery.Remote, "HEAD:refs/heads/"+delivery.Branch)
			if err != nil && !s.remoteHasGitDeliveryCommit(ctx, project.Path, delivery) {
				s.failGitDelivery(ctx, delivery, gitdelivery.StepPush, fmt.Errorf("push retry: %s", safeGitDetail(output, err)))
				return
			}
		} else {
			s.failGitDelivery(ctx, delivery, gitdelivery.StepPush, pushErr)
			return
		}
	}
	s.completeGitDelivery(ctx, delivery)
}

func (s *server) remoteHasGitDeliveryCommit(ctx context.Context, dir string, delivery *gitdelivery.Delivery) bool {
	output, err := s.gitInspector.run(ctx, dir, "ls-remote", delivery.Remote, "refs/heads/"+delivery.Branch)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(output))
	return len(fields) > 0 && fields[0] == delivery.CommitSHA
}

func (s *server) tryGitDeliveryRecovery(ctx context.Context, project project, delivery *gitdelivery.Delivery, step gitdelivery.Step, cause error) bool {
	if s.developer == nil || !repairableGitDeliveryFailure(cause.Error()) || len(delivery.Tickets) == 0 {
		return false
	}
	board, err := s.boards.GetProjectBoard(ctx, delivery.ProjectID)
	if err != nil {
		return false
	}
	taskIndex := findBoardTask(board, delivery.Tickets[0].TaskID)
	if taskIndex < 0 {
		return false
	}
	task := board.Tasks[taskIndex]
	delivery.Status = gitdelivery.StatusRecovering
	delivery.Recovery = &gitdelivery.Recovery{Status: "running", Provider: s.selectedAIProvider(), Attempt: 1}
	s.appendGitDeliveryEvent(delivery, "warning", step, "Developer recovery session started", "AI may edit only files already approved in this delivery.")
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	guard, guardErr := s.beginTaskSourceGuard(ctx, project, delivery.ID, delivery.ID, "git-recovery")
	if guardErr != nil {
		return false
	}
	reporter := s.taskSessions.startEntity(delivery.ProjectID, delivery.ID, "git_delivery", s.selectedAIProvider(), "git-recovery")
	repairCtx := agentstream.WithReporter(ctx, reporter)
	result, repairErr := s.developer.RepairGitDelivery(repairCtx, development.GitRepairRequest{Request: development.Request{ProjectPath: project.Path, Plan: taskPlan(board, task.PlanID), Task: task}, DeliveryID: delivery.ID, Step: string(step), GitError: cause.Error(), AllowedFiles: delivery.Files, Attempt: 1})
	s.taskSessions.finish(delivery.ProjectID, delivery.ID, repairErr)
	guardReport, guardVerifyErr := guard.VerifyAndRestore(context.Background())
	if guardVerifyErr != nil {
		repairErr = fmt.Errorf("source guard restored an unsafe recovery change: %w", guardVerifyErr)
	}
	_ = guardReport
	if repairErr == nil && !pathsWithinDelivery(result.ChangedFiles, project.Path, delivery.Files) {
		repairErr = fmt.Errorf("AI recovery changed files outside the approved delivery scope")
	}
	staged, _, _, pathErr := s.gitInspector.deliveryPaths(ctx, project.Path)
	if repairErr == nil && pathErr == nil && len(staged) > 0 {
		repairErr = fmt.Errorf("AI recovery staged files, which is not permitted")
	}
	if repairErr != nil || pathErr != nil {
		if pathErr != nil && repairErr == nil {
			repairErr = pathErr
		}
		delivery.Recovery.Status = "failed"
		delivery.Recovery.ThreadID = result.ThreadID
		delivery.Recovery.Summary = safeGitDetail(nil, repairErr)
		s.appendGitDeliveryEvent(delivery, "error", step, "Developer recovery failed", delivery.Recovery.Summary)
		_ = s.gitDeliveries.Upsert(ctx, *delivery)
		return false
	}
	delivery.Recovery.Status = "completed"
	delivery.Recovery.ThreadID = result.ThreadID
	delivery.Recovery.Summary = result.Summary
	s.appendGitDeliveryEvent(delivery, "info", step, "Developer recovery completed", strings.Join(result.Verification, " · "))
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	return true
}

func pathsWithinDelivery(changed []string, projectPath string, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = struct{}{}
	}
	for _, raw := range changed {
		if _, exists := allowedSet[normalizeTaskChangedFile(projectPath, raw)]; !exists {
			return false
		}
	}
	return true
}

func repairableGitDeliveryFailure(message string) bool {
	normalized := strings.ToLower(message)
	for _, blocked := range []string{"authentication", "permission denied", "could not resolve host", "failed to connect", "protected branch", "non-fast-forward", "fetch first", "rejected", "merge conflict"} {
		if strings.Contains(normalized, blocked) {
			return false
		}
	}
	for _, repairable := range []string{"hook", "npm test", "typecheck", "lint", "test failed", "verification"} {
		if strings.Contains(normalized, repairable) {
			return true
		}
	}
	return false
}

func (s *server) completeGitDelivery(ctx context.Context, delivery *gitdelivery.Delivery) {
	taskIDs := make([]string, 0, len(delivery.Tickets))
	for _, ticket := range delivery.Tickets {
		taskIDs = append(taskIDs, ticket.TaskID)
	}
	board, err := s.boards.SetTasksGitReference(ctx, delivery.ProjectID, taskIDs, kanban.TaskGitReference{Branch: delivery.Branch, CommitSHA: delivery.CommitSHA, LinkedAt: time.Now().UTC(), LinkedBy: "ProductCrew Git delivery"})
	if err != nil {
		s.failGitDelivery(ctx, delivery, gitdelivery.StepComplete, fmt.Errorf("commit was pushed but ticket association failed: %w", err))
		return
	}
	s.boardEvents.publish(board)
	now := time.Now().UTC()
	delivery.Status = gitdelivery.StatusCompleted
	delivery.Step = gitdelivery.StepComplete
	delivery.Error = ""
	delivery.CompletedAt = &now
	s.appendGitDeliveryEvent(delivery, "info", gitdelivery.StepComplete, "Delivery completed", "Commit "+delivery.ShortSHA+" was pushed and linked to every included ticket.")
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	s.recordGitDeliveryEvent(*delivery, "git.delivery.completed", "Git delivery completed", "success")
}

func (s *server) failGitDelivery(ctx context.Context, delivery *gitdelivery.Delivery, step gitdelivery.Step, cause error) {
	delivery.Status = gitdelivery.StatusFailed
	delivery.Step = step
	delivery.Error = safeGitDetail(nil, cause)
	now := time.Now().UTC()
	delivery.CompletedAt = &now
	s.appendGitDeliveryEvent(delivery, "error", step, "Delivery needs attention", delivery.Error)
	_ = s.gitDeliveries.Upsert(ctx, *delivery)
	s.recordGitDeliveryEvent(*delivery, "git.delivery.failed", "Git delivery failed", "failed")
}

func (s *server) appendGitDeliveryEvent(delivery *gitdelivery.Delivery, level string, step gitdelivery.Step, message, detail string) {
	now := time.Now().UTC()
	event := gitdelivery.Event{ID: fmt.Sprintf("event-%d", len(delivery.Events)+1), Timestamp: now, Level: level, Step: step, Message: message, Detail: safeGitDetail([]byte(detail), nil)}
	delivery.Events = append(delivery.Events, event)
	if len(delivery.Events) > maxGitDeliveryEvents {
		delivery.Events = append([]gitdelivery.Event(nil), delivery.Events[len(delivery.Events)-maxGitDeliveryEvents:]...)
	}
	delivery.UpdatedAt = now
}

func (s *server) recordGitDeliveryEvent(delivery gitdelivery.Delivery, name, message, outcome string) {
	if s.observability == nil {
		return
	}
	s.observability.Record(observability.Event{Category: "git", Name: name, Message: message, CorrelationID: delivery.ID, ProjectID: delivery.ProjectID, EntityType: "git_delivery", EntityID: delivery.ID, Agent: "git-delivery", Stage: string(delivery.Step), Outcome: outcome, Attributes: map[string]any{"branch": delivery.Branch, "commitSha": delivery.CommitSHA, "ticketCount": len(delivery.Tickets), "fileCount": len(delivery.Files)}})
}

func (s *server) recoverInterruptedGitDeliveries() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deliveries, err := s.gitDeliveries.List(ctx, "", 0)
	if err != nil {
		return
	}
	for index := range deliveries {
		delivery := &deliveries[index]
		if delivery.Status != gitdelivery.StatusQueued && delivery.Status != gitdelivery.StatusRunning && delivery.Status != gitdelivery.StatusRecovering {
			continue
		}
		delivery.Status = gitdelivery.StatusFailed
		delivery.Error = "ProductCrew restarted before this delivery reached a terminal state. Review the persisted trace and retry safely."
		now := time.Now().UTC()
		delivery.CompletedAt = &now
		s.appendGitDeliveryEvent(delivery, "error", delivery.Step, "Interrupted delivery recovered", delivery.Error)
		_ = s.gitDeliveries.Upsert(ctx, *delivery)
	}
}

func safeGitDetail(output []byte, err error) string {
	value := strings.TrimSpace(string(output))
	if value == "" && err != nil {
		value = err.Error()
	}
	for _, marker := range []string{"https://", "http://"} {
		start := 0
		for {
			index := strings.Index(value[start:], marker)
			if index < 0 {
				break
			}
			index += start
			end := index + len(marker)
			for end < len(value) && !strings.ContainsRune(" \t\r\n", rune(value[end])) {
				end++
			}
			segment := value[index:end]
			if at := strings.Index(segment, "@"); at >= 0 {
				value = value[:index] + marker + "[redacted]@" + segment[at+1:] + value[end:]
			}
			start = index + len(marker)
		}
	}
	if len(value) > 4000 {
		value = value[:4000] + "…"
	}
	return value
}
