package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

const projectRemovalActiveWorkMessage = "Finish or stop active work before removing this project."

var errProjectRemovalActiveWork = errors.New(projectRemovalActiveWorkMessage)

type removeProjectResult struct {
	Project         project `json:"project"`
	BoardRemoved    bool    `json:"boardRemoved"`
	InsightsRemoved int     `json:"insightsRemoved"`
}

func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	result, err := s.removeProject(r.Context(), r.PathValue("projectID"))
	switch {
	case errors.Is(err, errProjectNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errProjectRemovalActiveWork):
		writeJSON(w, http.StatusConflict, map[string]string{"error": projectRemovalActiveWorkMessage})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *server) removeProject(ctx context.Context, projectID string) (removeProjectResult, error) {
	s.projectWorkMu.Lock()
	defer s.projectWorkMu.Unlock()

	project, err := s.projectService.findProject(projectID)
	if err != nil {
		return removeProjectResult{}, err
	}
	if err := s.rejectProjectRemovalWithActiveWork(ctx, project.ID); err != nil {
		return removeProjectResult{}, err
	}
	boardRemoved, err := s.boards.DeleteProjectBoard(ctx, project.ID)
	if err != nil {
		return removeProjectResult{}, err
	}
	insightsRemoved, err := s.insights.DeleteProject(ctx, project.ID)
	if err != nil {
		return removeProjectResult{}, err
	}
	removed, err := s.projectService.removeProject(project.ID)
	if err != nil {
		return removeProjectResult{}, err
	}
	return removeProjectResult{Project: removed, BoardRemoved: boardRemoved, InsightsRemoved: insightsRemoved}, nil
}

func (s *server) rejectProjectRemovalWithActiveWork(ctx context.Context, projectID string) error {
	board, err := s.boards.GetProjectBoard(ctx, projectID)
	if err != nil && !errors.Is(err, kanban.ErrNotFound) {
		return err
	}
	if err == nil && boardHasActiveWork(board) {
		return errProjectRemovalActiveWork
	}
	if s.sourceScans != nil && s.sourceScans.projectRunning(projectID) {
		return errProjectRemovalActiveWork
	}
	if s.featureRadar != nil && s.featureRadar.projectRunning(projectID) {
		return errProjectRemovalActiveWork
	}
	return nil
}

func boardHasActiveWork(board kanban.Board) bool {
	if board.ActiveTaskID != "" {
		return true
	}
	for _, plan := range board.Plans {
		if plan.Status == kanban.PlanningAnalyzing {
			return true
		}
	}
	for _, task := range board.Tasks {
		if task.Status == kanban.TaskInProgress {
			return true
		}
	}
	return false
}
