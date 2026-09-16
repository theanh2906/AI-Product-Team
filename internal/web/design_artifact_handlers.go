package web

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/designartifact"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

func (s *server) getTaskDesignArtifact(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	taskID := r.PathValue("taskID")
	artifactID := r.PathValue("artifactID")
	board, err := s.boards.GetProjectBoard(context.Background(), projectID)
	if errors.Is(err, kanban.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var artifact *kanban.DesignArtifact
	for _, task := range board.Tasks {
		if task.ID != taskID || task.Execution == nil {
			continue
		}
		for index := range task.Execution.Artifacts {
			if task.Execution.Artifacts[index].ID == artifactID {
				artifact = &task.Execution.Artifacts[index]
				break
			}
		}
		break
	}
	if artifact == nil || artifact.MediaType != "image/png" {
		http.NotFound(w, r)
		return
	}
	project, err := s.projectService.findProject(projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root, err := designartifact.TaskDirectory(project.Path, taskID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	candidate := filepath.Join(project.Path, filepath.FromSlash(artifact.RelativePath))
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, candidate)
}

func (s *server) getTaskDesignSource(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	taskID := r.PathValue("taskID")
	requestedPath := filepath.ToSlash(strings.TrimSpace(r.URL.Query().Get("path")))
	if requestedPath == "" || !strings.EqualFold(filepath.Ext(requestedPath), ".html") {
		http.NotFound(w, r)
		return
	}

	board, err := s.boards.GetProjectBoard(r.Context(), projectID)
	if errors.Is(err, kanban.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var task *kanban.Task
	for index := range board.Tasks {
		if board.Tasks[index].ID == taskID && board.Tasks[index].Role == kanban.RoleDesigner {
			task = &board.Tasks[index]
			break
		}
	}
	if task == nil || !taskReportsDesignSource(*task, requestedPath) {
		http.NotFound(w, r)
		return
	}

	project, err := s.projectService.findProject(projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root, err := designartifact.TaskDirectory(project.Path, taskID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	candidate := filepath.Join(project.Path, filepath.FromSlash(requestedPath))
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > 5<<20 {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:")
	http.ServeFile(w, r, candidate)
}

func taskReportsDesignSource(task kanban.Task, requestedPath string) bool {
	matches := func(changedFiles []string) bool {
		for _, changedFile := range changedFiles {
			if filepath.ToSlash(strings.TrimSpace(changedFile)) == requestedPath {
				return true
			}
		}
		return false
	}
	if task.Execution != nil && matches(task.Execution.ChangedFiles) {
		return true
	}
	for _, revision := range task.RevisionHistory {
		if matches(revision.Execution.ChangedFiles) {
			return true
		}
	}
	return false
}
