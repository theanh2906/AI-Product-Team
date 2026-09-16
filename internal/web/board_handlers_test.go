package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/planning"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

type fakeTeamLeadPlanner struct{}

func (fakeTeamLeadPlanner) Generate(context.Context, planning.Request) (planning.Result, error) {
	return planning.Result{
		ThreadID: "thread-test",
		Draft: kanban.PlanDraft{
			Summary:   "Implement the approved feature through one role-safe Developer task and independent QA verification.",
			Documents: []kanban.DraftDocument{{Ref: "spec", Title: "Feature specification", Kind: "product-spec", Audience: []kanban.AgentRole{kanban.RoleDeveloper, kanban.RoleQA}, Content: "Approved scope."}},
			Tasks: []kanban.DraftTask{
				{Ref: "implement", Role: kanban.RoleDeveloper, Title: "Implement saved filters", Priority: kanban.PriorityHigh, Description: "Implement persistence.", AcceptanceCriteria: []string{"Filters restore"}, Documents: []string{"spec"}},
				{Ref: "verify", Role: kanban.RoleQA, Title: "Verify saved filters", Priority: kanban.PriorityHigh, Description: "Verify scope and regressions.", AcceptanceCriteria: []string{"No open reproducible bugs"}, Dependencies: []string{"implement"}, Documents: []string{"spec"}},
			},
		},
	}, nil
}

func TestBoardPlanningReviewAndRoleSafeMoveAPI(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "commerce")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	payload, _ := json.Marshal(createPlanRequest{
		Title: "Saved filters", Description: "Persist and restore selected product search filters.", WorkType: "feature",
		DeliveryTarget: "fullstack", AcceptanceCriteria: []string{"Core flow"},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/plans", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected planning request 202, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var board kanban.Board
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		board, err = boards.GetProjectBoard(context.Background(), project.ID)
		if err == nil && len(board.Plans) == 1 && board.Plans[0].Status == kanban.PlanningAwaitingApproval {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(board.Plans) != 1 || board.Plans[0].Status != kanban.PlanningAwaitingApproval {
		t.Fatalf("planner did not persist an approval checkpoint: %+v %v", board, err)
	}
	qaCriteria := strings.Join(board.Tasks[1].AcceptanceCriteria, " | ")
	if !strings.Contains(qaCriteria, "approved design or specification") || !strings.Contains(qaCriteria, "No unresolved reproducible bugs") {
		t.Fatalf("QA Definition of Done was not enforced: %v", board.Tasks[1].AcceptanceCriteria)
	}

	reviewBody := strings.NewReader(`{"decision":"approve","reviewer":"PM","reason":""}`)
	review := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/plans/"+board.Plans[0].ID+"/review", reviewBody)
	review.Header.Set("Content-Type", "application/json")
	reviewRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reviewRecorder, review)
	if reviewRecorder.Code != http.StatusOK {
		t.Fatalf("expected approval 200, got %d: %s", reviewRecorder.Code, reviewRecorder.Body.String())
	}

	developerTask := board.Tasks[0]
	moveBody := strings.NewReader(`{"targetColumn":"designer"}`)
	move := httptest.NewRequest(http.MethodPatch, "/api/projects/"+project.ID+"/board/tasks/"+developerTask.ID+"/move", moveBody)
	move.Header.Set("Content-Type", "application/json")
	moveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(moveRecorder, move)
	if moveRecorder.Code != http.StatusUnprocessableEntity || !strings.Contains(moveRecorder.Body.String(), "Developer queue") {
		t.Fatalf("expected wrong-agent move rejection, got %d: %s", moveRecorder.Code, moveRecorder.Body.String())
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/plans", bytes.NewReader(payload))
	secondRequest.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected second planning request 202, got %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	var secondPlan kanban.Plan
	for time.Now().Before(deadline) {
		board, err = boards.GetProjectBoard(context.Background(), project.ID)
		if err == nil && len(board.Plans) == 2 && board.Plans[1].Status == kanban.PlanningAwaitingApproval {
			secondPlan = board.Plans[1]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if secondPlan.ID == "" {
		t.Fatalf("second plan did not reach review: %+v %v", board, err)
	}
	denyBody := strings.NewReader(`{"decision":"deny","reviewer":"PM","reason":"Clarify the persistence boundary"}`)
	deny := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/plans/"+secondPlan.ID+"/review", denyBody)
	deny.Header.Set("Content-Type", "application/json")
	denyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(denyRecorder, deny)
	if denyRecorder.Code != http.StatusOK {
		t.Fatalf("expected denial 200, got %d: %s", denyRecorder.Code, denyRecorder.Body.String())
	}
	var deniedBoard kanban.Board
	if err := json.Unmarshal(denyRecorder.Body.Bytes(), &deniedBoard); err != nil {
		t.Fatal(err)
	}
	if deniedBoard.Plans[1].Status != kanban.PlanningAnalyzing {
		t.Fatalf("denial response must reflect the restarted Team Lead pass, got %s", deniedBoard.Plans[1].Status)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		board, err = boards.GetProjectBoard(context.Background(), project.ID)
		if err == nil && board.Plans[1].Revision == 2 && board.Plans[1].Status == kanban.PlanningAwaitingApproval {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("revised plan did not finish before test cleanup: %+v %v", board.Plans[1], err)
}

func TestApproveDesignHandoffAPI(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "design-review")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	now := time.Now().UTC()
	if err := repository.Create(context.Background(), kanban.Board{
		ID: "board-1", ProjectID: project.ID, ProjectName: project.Name,
		Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningApproved, CreatedAt: now, UpdatedAt: now}},
		Tasks: []kanban.Task{{
			ID: "task-design", PlanID: "plan-1", Key: "DSN-001", Role: kanban.RoleDesigner,
			Column: kanban.ColumnDesigner, Status: kanban.TaskDesignReview, Priority: kanban.PriorityHigh,
			Execution: &kanban.TaskExecution{Summary: "Mockup is ready.", CompletedAt: now},
			CreatedAt: now, UpdatedAt: now,
		}},
		Backlog: []kanban.BacklogItem{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(context.Background(), kanban.Board{
		ID: "board-busy", ProjectID: "project-busy", ProjectName: "Busy project", ActiveTaskID: "busy-task",
		Plans: []kanban.Plan{{ID: "plan-busy", Status: kanban.PlanningApproved, CreatedAt: now, UpdatedAt: now}},
		Tasks: []kanban.Task{{
			ID: "busy-task", PlanID: "plan-busy", Key: "DEV-999", Role: kanban.RoleDeveloper,
			Column: kanban.ColumnDeveloper, Status: kanban.TaskInProgress, Priority: kanban.PriorityHigh,
			CreatedAt: now, UpdatedAt: now,
		}},
		Backlog: []kanban.BacklogItem{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/tasks/task-design/approve-design", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected design approval 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var board kanban.Board
	if err := json.Unmarshal(recorder.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].Status != kanban.TaskCompleted || board.Tasks[0].Column != kanban.ColumnDone {
		t.Fatalf("Designer handoff was not completed after approval: %+v", board.Tasks[0])
	}
}

func TestSubmitDesignFeedbackAPIRequeuesSameTaskAndPersistsRevisionHistory(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "design-feedback")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	now := time.Now().UTC()
	if err := repository.Create(context.Background(), kanban.Board{
		ID: "board-1", ProjectID: project.ID, ProjectName: project.Name,
		Plans: []kanban.Plan{{ID: "plan-1", Status: kanban.PlanningApproved, CreatedAt: now, UpdatedAt: now}},
		Tasks: []kanban.Task{{
			ID: "task-design", PlanID: "plan-1", Key: "DSN-001", Role: kanban.RoleDesigner,
			Column: kanban.ColumnDesigner, Status: kanban.TaskDesignReview, Priority: kanban.PriorityHigh,
			ThreadID: "designer-thread",
			Execution: &kanban.TaskExecution{
				Summary:      "Mockup is ready.",
				ChangedFiles: []string{".productcrew/design-artifacts/task-design/overview.html"},
				Verification: []string{"Reviewed the task drawer."},
				Artifacts:    []kanban.DesignArtifact{{ID: "artifact-1", RelativePath: ".productcrew/design-artifacts/task-design/overview.png"}},
				CompletedAt:  now,
			},
			CreatedAt: now, UpdatedAt: now,
		}},
		Backlog: []kanban.BacklogItem{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/tasks/task-design/design-feedback", strings.NewReader(`{"feedback":"Refine the feedback composer and keep Approve design primary.","reviewer":"PM"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected design feedback 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var board kanban.Board
	if err := json.Unmarshal(recorder.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	task := board.Tasks[0]
	if task.ID != "task-design" || task.Status != kanban.TaskQueued || task.Column != kanban.ColumnDesigner || task.ThreadID != "designer-thread" {
		t.Fatalf("Designer feedback did not requeue the same task: %+v", task)
	}
	if task.Execution != nil || len(task.RevisionHistory) != 1 {
		t.Fatalf("Designer feedback did not archive the prior execution: %+v", task)
	}
	if task.RevisionHistory[0].Feedback != "Refine the feedback composer and keep Approve design primary." || task.RevisionHistory[0].Execution.Summary != "Mockup is ready." {
		t.Fatalf("revision history is missing the PM feedback or prior execution: %+v", task.RevisionHistory[0])
	}

	reportPath := filepath.Join(projectDirectory, ".productcrew", "tasks", "task-design", "delivery-report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status          string                `json:"status"`
		ThreadID        string                `json:"threadId"`
		Execution       *kanban.TaskExecution `json:"execution"`
		RevisionHistory []kanban.TaskRevision `json:"revisionHistory"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != string(kanban.TaskQueued) || report.ThreadID != "designer-thread" || report.Execution != nil || len(report.RevisionHistory) != 1 {
		t.Fatalf("persisted task report did not retain revision history: %+v", report)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := boards.GetProjectBoard(context.Background(), project.ID)
		if getErr == nil && len(current.Tasks) == 1 && current.Tasks[0].Status != kanban.TaskQueued {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background Designer requeue did not finish before test cleanup")
}

func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

func TestCreateBacklogAcceptsFrontendJSONRequest(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "json-backlog")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	payload := strings.NewReader(`{"type":"feature","source":"manual","title":"Board activity cleanup","description":"Revert and remove the board activity feature from the Workboard.","workType":"feature","deliveryTarget":"fullstack","requiresUI":false,"acceptanceCriteria":[]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/backlog", payload)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected backlog creation 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	board, err := boards.GetProjectBoard(context.Background(), project.ID)
	if err != nil || len(board.Backlog) != 1 {
		t.Fatalf("board backlog = %+v, %v", board.Backlog, err)
	}
	if board.Backlog[0].Type != kanban.BacklogFeature || board.Backlog[0].Title != "Board activity cleanup" {
		t.Fatalf("unexpected backlog item: %+v", board.Backlog[0])
	}
}

func TestCreateBacklogKeepsExplicitTypeWhenIntakeWorkTypeDiffers(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "explicit-type")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	payload := strings.NewReader(`{
		"type":"feature",
		"source":"manual",
		"title":"Add backlog remove action",
		"description":"Add a remove action for backlog cards even when the request mentions QA bug cards.",
		"workType":"feature",
		"deliveryTarget":"fullstack",
		"requiresUI":true,
		"acceptanceCriteria":[],
		"intake":{
			"questionnaire":{
				"schemaVersion":1,
				"heading":"Confirm details",
				"summary":"Answer decisions needed before planning.",
				"workType":"bug",
				"source":"codex",
				"questions":[
					{"id":"user-experience","label":"Does this change UI?","helpText":"Controls Designer work.","type":"toggle","binding":"requiresUI","required":true,"options":[{"value":"true","label":"Yes","description":"Include UI.","acceptanceCriterion":"UI behavior works"},{"value":"false","label":"No","description":"No UI.","acceptanceCriterion":"No UI regression"}],"defaultValues":["true"],"layout":{"span":12}},
					{"id":"delivery-surface","label":"Which surface?","helpText":"Choose the boundary.","type":"dropdown","binding":"deliveryTarget","required":true,"options":[{"value":"frontend","label":"Frontend","description":"Client.","acceptanceCriterion":"Frontend works"},{"value":"backend","label":"Backend","description":"Service.","acceptanceCriterion":"Backend works"},{"value":"fullstack","label":"Full-stack","description":"Both.","acceptanceCriterion":"Full-stack works"}],"defaultValues":["fullstack"],"layout":{"span":6}}
				]
			},
			"answers":{"user-experience":["true"],"delivery-surface":["fullstack"]}
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/backlog", payload)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected backlog creation 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	board, err := boards.GetProjectBoard(context.Background(), project.ID)
	if err != nil || len(board.Backlog) != 1 {
		t.Fatalf("board backlog = %+v, %v", board.Backlog, err)
	}
	if board.Backlog[0].Type != kanban.BacklogFeature {
		t.Fatalf("explicit request type was overwritten by intake work type: %+v", board.Backlog[0])
	}
}

func TestFallbackIntakeKeepsSelectedWorkTypeForBugWording(t *testing.T) {
	questionnaire := fallbackIntakeQuestionnaire(intakeGenerationRequest{
		Title:       "Add Remove button to remove Feature/Bug/Task from Backlog",
		Description: "Allow users to remove unnecessary backlog cards, including cards created from QA bug reports.",
		WorkType:    "feature",
	})

	if questionnaire.WorkType != "feature" {
		t.Fatalf("fallback intake reclassified the selected work type: %q", questionnaire.WorkType)
	}
}

func TestCreateBacklogPersistsMultipartRequestHistory(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "attachments")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	requestJSON := `{"type":"feature","source":"manual","title":"Screenshot references","description":"Persist screenshots with the planning request.","deliveryTarget":"frontend","requiresUI":true,"acceptanceCriteria":["Reference is available"]}`
	if err := writer.WriteField("request", requestJSON); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("attachments", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/backlog", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected backlog creation 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	board, err := boards.GetProjectBoard(context.Background(), project.ID)
	if err != nil || len(board.Backlog) != 1 {
		t.Fatalf("board backlog = %+v, %v", board.Backlog, err)
	}
	item := board.Backlog[0]
	if item.RequestID == "" || len(item.Attachments) != 1 || item.ArtifactPath == "" {
		t.Fatalf("request artifact references were not persisted: %+v", item)
	}
	if _, err := os.Stat(filepath.Join(projectDirectory, filepath.FromSlash(item.Attachments[0].RelativePath))); err != nil {
		t.Fatalf("request attachment not found: %v", err)
	}
	attachmentRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/requests/"+item.RequestID+"/attachments/"+item.Attachments[0].ID, nil)
	attachmentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(attachmentRecorder, attachmentRequest)
	if attachmentRecorder.Code != http.StatusOK || attachmentRecorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected attachment response: %d %s", attachmentRecorder.Code, attachmentRecorder.Header().Get("Content-Type"))
	}
}

func TestRemoveBacklogItemAPI(t *testing.T) {
	dataDirectory := t.TempDir()
	projectDirectory := filepath.Join(t.TempDir(), "api-backlog-remove")
	if err := ensureDirectory(projectDirectory); err != nil {
		t.Fatal(err)
	}
	projects, err := newProjectService(dataDirectory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.importLocal(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewJSONBoardRepository(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	boards := kanban.NewService(repository)
	assets := fstest.MapFS{"index.html": {Data: []byte("<app-root></app-root>")}}
	handler := newServerWithDependencies(assets, projects, func() (string, error) { return "", nil }, nil, boards, fakeTeamLeadPlanner{})

	now := time.Now().UTC()
	board := kanban.Board{
		ID: "board-1", ProjectID: project.ID, ProjectName: project.Name,
		Plans: []kanban.Plan{
			{ID: "plan-original", Status: kanban.PlanningApproved, Sequence: &kanban.PlanSequence{Status: kanban.PlanSequenceBlocked, BlockedReason: "QA found bugs", ScheduledAt: now.Add(-2 * time.Hour)}},
		},
		Backlog: []kanban.BacklogItem{{
			ID: "bug-item", Key: "BUG-001", Type: kanban.BacklogBug, Source: "qa", SourceReference: "other-qa:bug:1",
			Title: "[QA] Save fails", Description: "QA raised bug.", Status: kanban.BacklogPlanning, PlanID: "plan-bug",
			WaitingQATaskIDs: []string{"qa-task"},
		}},
		Tasks: []kanban.Task{
			{ID: "qa-task", PlanID: "plan-original", Key: "QA-001", Role: kanban.RoleQA, Column: kanban.ColumnQA, Status: kanban.TaskBlocked, SequenceOrder: 3, BlockedReason: "QA is waiting on a bug already in planning.", Execution: &kanban.TaskExecution{Summary: "Failed."}, ThreadID: "qa-thread"},
		},
	}
	if err := repository.Create(context.Background(), board); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/board/backlog/bug-item/remove", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var updatedBoard kanban.Board
	if err := json.Unmarshal(recorder.Body.Bytes(), &updatedBoard); err != nil {
		t.Fatal(err)
	}

	if len(updatedBoard.Backlog) != 0 {
		t.Fatalf("expected backlog to be empty, got: %d", len(updatedBoard.Backlog))
	}

	originalQA := updatedBoard.Tasks[0]
	if originalQA.Status != kanban.TaskQueued || originalQA.Column != kanban.ColumnQA || originalQA.BlockedReason != "" || originalQA.Execution != nil {
		t.Fatalf("waiting QA task was not requeued cleanly: %+v", originalQA)
	}
}
