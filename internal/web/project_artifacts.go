package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
)

type taskReportPayload struct {
	TaskID            string                    `json:"taskId"`
	Key               string                    `json:"key"`
	Role              kanban.AgentRole          `json:"role"`
	Title             string                    `json:"title"`
	Status            kanban.TaskStatus         `json:"status"`
	ThreadID          string                    `json:"threadId,omitempty"`
	Execution         *kanban.TaskExecution     `json:"execution,omitempty"`
	RevisionHistory   []kanban.TaskRevision     `json:"revisionHistory,omitempty"`
	BuildVerification *kanban.BuildVerification `json:"buildVerification,omitempty"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

func (s *server) getProjectRequest(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectService.findProject(strings.TrimSpace(r.PathValue("projectID")))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	manifest, err := s.projectArtifacts.LoadRequest(r.Context(), project.Path, r.PathValue("requestID"))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Request history was not found in this project."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *server) getProjectRequestAttachment(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectService.findProject(strings.TrimSpace(r.PathValue("projectID")))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	attachment, file, err := s.projectArtifacts.OpenAttachment(r.Context(), project.Path, r.PathValue("requestID"), r.PathValue("attachmentID"))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Request attachment was not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", attachment.MediaType)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disposition, attachment.OriginalName))
	http.ServeContent(w, r, attachment.OriginalName, info.ModTime(), file)
}

func decodeBacklogRequest(w http.ResponseWriter, r *http.Request) (createBacklogRequest, []projectartifact.Upload, bool) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		var request createBacklogRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeBacklogDecodeError(w, r, http.StatusBadRequest, "invalid_request", "The request body must be a valid JSON object.")
			return createBacklogRequest{}, nil, false
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeBacklogDecodeError(w, r, http.StatusBadRequest, "invalid_request", "The request body must contain one JSON object.")
			return createBacklogRequest{}, nil, false
		}
		return request, nil, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, projectartifact.MaxRequestUploadBytes+(2<<20))
	if err := r.ParseMultipartForm(projectartifact.MaxRequestUploadBytes); err != nil {
		writeBacklogDecodeError(w, r, http.StatusRequestEntityTooLarge, "attachments_too_large", "Request attachments exceed the upload limit.")
		return createBacklogRequest{}, nil, false
	}
	var request createBacklogRequest
	if err := json.Unmarshal([]byte(r.FormValue("request")), &request); err != nil {
		writeBacklogDecodeError(w, r, http.StatusBadRequest, "invalid_request", "Request metadata is invalid.")
		return createBacklogRequest{}, nil, false
	}
	files := r.MultipartForm.File["attachments"]
	if len(files) > projectartifact.MaxAttachments {
		writeBacklogDecodeError(w, r, http.StatusUnprocessableEntity, "too_many_attachments", fmt.Sprintf("A request can contain at most %d attachments.", projectartifact.MaxAttachments))
		return createBacklogRequest{}, nil, false
	}
	uploads := make([]projectartifact.Upload, 0, len(files))
	for _, header := range files {
		header := header
		uploads = append(uploads, uploadFromMultipart(header))
	}
	return request, uploads, true
}

func uploadFromMultipart(header *multipart.FileHeader) projectartifact.Upload {
	return projectartifact.Upload{
		Name: header.Filename, MediaType: header.Header.Get("Content-Type"), Size: header.Size,
		Open: func() (io.ReadCloser, error) { return header.Open() },
	}
}

func (s *server) persistRequestArtifact(ctx context.Context, project project, requestID, title, description, source, sourceReference string, uploads []projectartifact.Upload) (projectartifact.RequestManifest, error) {
	return s.projectArtifacts.SaveRequest(ctx, project.Path, projectartifact.RequestInput{
		RequestID: requestID, ProjectID: project.ID, Title: title, Description: description,
		Source: source, SourceReference: sourceReference, CreatedAt: time.Now().UTC(), Attachments: uploads,
	})
}

func toKanbanAttachments(values []projectartifact.Attachment) []kanban.RequestAttachment {
	result := make([]kanban.RequestAttachment, 0, len(values))
	for _, value := range values {
		result = append(result, kanban.RequestAttachment{
			ID: value.ID, OriginalName: value.OriginalName, RelativePath: value.RelativePath,
			MediaType: value.MediaType, Size: value.Size, SHA256: value.SHA256,
		})
	}
	return result
}

func (s *server) migrateProjectArtifacts() {
	projects, err := s.projectService.listProjects()
	if err != nil {
		return
	}
	for _, project := range projects {
		project := project
		if err := s.migrateProjectRequests(project); err != nil {
			s.recordArtifactMigrationFailure(project.ID, "requests", err)
		}
		if err := s.migrateProjectInsights(project); err != nil {
			s.recordArtifactMigrationFailure(project.ID, "insights", err)
		}
	}
}

func (s *server) migrateProjectRequests(project project) error {
	board, err := s.boards.GetProjectBoard(context.Background(), project.ID)
	if errors.Is(err, kanban.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, item := range board.Backlog {
		requestID := strings.TrimSpace(item.RequestID)
		if requestID == "" {
			continue
		}
		if _, err := s.projectArtifacts.LoadRequest(context.Background(), project.Path, requestID); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := s.persistRequestArtifact(context.Background(), project, requestID, item.Title, item.Description, item.Source, item.SourceReference, nil); err != nil {
			return err
		}
	}
	for _, plan := range board.Plans {
		requestID := strings.TrimSpace(plan.Request.ID)
		if requestID == "" {
			continue
		}
		if _, err := s.projectArtifacts.LoadRequest(context.Background(), project.Path, requestID); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := s.persistRequestArtifact(context.Background(), project, requestID, plan.Request.Title, plan.Request.Description, "planning", plan.ID, nil); err != nil {
			return err
		}
	}
	for _, task := range board.Tasks {
		if task.Execution == nil && len(task.RevisionHistory) == 0 {
			continue
		}
		if _, err := s.projectArtifacts.SaveTaskReport(context.Background(), project.Path, projectartifact.TaskReportInput{
			TaskID: task.ID, Key: task.Key, Role: string(task.Role), Title: task.Title,
			Status: string(task.Status), Summary: taskReportSummary(task),
			CompletedAt: taskReportCompletedAt(task), Payload: buildTaskReportPayload(task),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) migrateProjectInsights(project project) error {
	records, err := s.insights.ListProject(context.Background(), project.ID)
	if err != nil {
		return err
	}
	for _, record := range records {
		runID := insightRunID(record)
		if runID == "" {
			continue
		}
		if _, err := s.projectArtifacts.SaveInsight(context.Background(), project.Path, string(record.Kind), runID, record.Payload); err != nil {
			return err
		}
	}
	return nil
}

func insightRunID(record insights.Record) string {
	var envelope struct {
		AnalysisID string `json:"analysisId"`
		ScanID     string `json:"scanId"`
	}
	if json.Unmarshal(record.Payload, &envelope) != nil {
		return ""
	}
	if record.Kind == insights.KindFeatureRadar {
		return strings.TrimSpace(envelope.AnalysisID)
	}
	return strings.TrimSpace(envelope.ScanID)
}

func (s *server) recordArtifactMigrationFailure(projectID, kind string, err error) {
	s.observability.Record(observability.Event{
		Level: observability.LevelWarn, Category: "storage", Name: "project_artifact.migration_failed",
		Message: "Project-local " + kind + " migration could not be completed", ProjectID: projectID,
		EntityType: "project_artifact", EntityID: kind, Stage: "migration", Outcome: "warning",
		Attributes: map[string]any{"error": err.Error()},
	})
}

func (s *server) persistTaskExecutionArtifact(project project, board kanban.Board, taskID string) {
	for _, task := range board.Tasks {
		if task.ID != taskID || (task.Execution == nil && len(task.RevisionHistory) == 0) {
			continue
		}
		if _, err := s.projectArtifacts.SaveTaskReport(context.Background(), project.Path, projectartifact.TaskReportInput{
			TaskID: task.ID, Key: task.Key, Role: string(task.Role), Title: task.Title,
			Status: string(task.Status), Summary: taskReportSummary(task),
			CompletedAt: taskReportCompletedAt(task), Payload: buildTaskReportPayload(task),
		}); err != nil {
			s.observability.Record(observability.Event{
				Level: observability.LevelWarn, Category: "storage", Name: "task_artifact.write_failed",
				Message: "Task delivery report could not be mirrored into the project", ProjectID: project.ID,
				EntityType: "task", EntityID: task.ID, Agent: string(task.Role), Stage: "persistence", Outcome: "warning",
				Attributes: map[string]any{"error": err.Error()},
			})
		}
		return
	}
}

func buildTaskReportPayload(task kanban.Task) taskReportPayload {
	payload := taskReportPayload{
		TaskID:            task.ID,
		Key:               task.Key,
		Role:              task.Role,
		Title:             task.Title,
		Status:            task.Status,
		ThreadID:          strings.TrimSpace(task.ThreadID),
		RevisionHistory:   append([]kanban.TaskRevision{}, task.RevisionHistory...),
		BuildVerification: task.BuildVerification,
		UpdatedAt:         task.UpdatedAt,
	}
	if task.Execution != nil {
		execution := *task.Execution
		payload.Execution = &execution
	}
	return payload
}

func taskReportSummary(task kanban.Task) string {
	if task.Execution != nil && strings.TrimSpace(task.Execution.Summary) != "" {
		return task.Execution.Summary
	}
	if count := len(task.RevisionHistory); count > 0 {
		latest := task.RevisionHistory[count-1]
		if feedback := strings.TrimSpace(latest.Feedback); feedback != "" {
			return "Awaiting revised Designer handoff after PM feedback: " + feedback
		}
		if strings.TrimSpace(latest.Execution.Summary) != "" {
			return latest.Execution.Summary
		}
	}
	return strings.TrimSpace(task.Title)
}

func taskReportCompletedAt(task kanban.Task) time.Time {
	if task.Execution != nil {
		return task.Execution.CompletedAt
	}
	if count := len(task.RevisionHistory); count > 0 {
		return task.RevisionHistory[count-1].RequestedAt
	}
	return task.UpdatedAt
}
