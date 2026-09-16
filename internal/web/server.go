package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
	"github.com/theanh2906/AI-Product-Team/internal/codex"
	"github.com/theanh2906/AI-Product-Team/internal/deploy"
	"github.com/theanh2906/AI-Product-Team/internal/design"
	"github.com/theanh2906/AI-Product-Team/internal/designartifact"
	"github.com/theanh2906/AI-Product-Team/internal/development"
	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/notifications"
	"github.com/theanh2906/AI-Product-Team/internal/observability"
	"github.com/theanh2906/AI-Product-Team/internal/planning"
	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
	"github.com/theanh2906/AI-Product-Team/internal/quality"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
	"github.com/theanh2906/AI-Product-Team/internal/toolchain"
)

//go:embed dist
var content embed.FS

type server struct {
	assets              fs.FS
	fileServer          http.Handler
	mux                 *http.ServeMux
	projectService      *projectService
	folderPicker        func() (string, error)
	sourceScans         *sourceScanManager
	featureRadar        *featureRadarManager
	projectIntelligence *projectStudyManager
	insights            *insights.Service
	observability       *observability.Service
	notifications       *notifications.Service
	notificationEvents  *notificationEventHub
	codexRuntime        *codex.Client
	boards              *kanban.Service
	boardEvents         *boardEventHub
	taskSessions        *taskSessionLogManager
	teamLead            planning.Planner
	designer            design.Designer
	designArtifacts     *designartifact.Service
	projectArtifacts    projectartifact.Store
	developer           development.Developer
	qa                  quality.QA
	toolchain           *toolchain.Service
	gitInspector        *gitInspector
	gitAvailable        func(context.Context) bool
	gitDeliveries       gitdelivery.Repository
	gitDeliveryMu       sync.Mutex
	agentModelsMu       sync.Mutex
	agentModelsCache    agentModelOptionsResponse
	agentModelsCached   time.Time
	buildProfiles       buildverify.Repository
	buildDetector       buildverify.Detector
	buildRunner         buildverify.Runner
	buildRuns           *buildRunHub
	deployProfiles      deploy.Repository
	deployDetector      deploy.Detector
	deployRunner        deploy.Runner
	deployRuns          *deployRunHub
	projectWorkMu       sync.Mutex
	agentMu             sync.Mutex
	taskQueueMu         sync.Mutex
	taskQueueRunning    bool
	taskQueueRequested  bool
	autopilotMu         sync.Mutex
	autopilotPlans      map[string]struct{}
}

func NewServer() http.Handler {
	assets, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}
	return newServer(assets)
}

// NewServerWithCodex serves the UI with a connected local Codex app-server.
func NewServerWithCodex(runtime *codex.Client) http.Handler {
	assets, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}
	projects, err := newProjectService(os.Getenv("APP_DATA_DIR"), systemCredentialStore{})
	if err != nil {
		panic(err)
	}
	boardService := newBoardService(projects)
	if err := boardService.RecoverInterruptedState(context.Background()); err != nil {
		panic(err)
	}
	return newServerWithDependencies(assets, projects, pickFolder, runtime, boardService, nil)
}

func newServer(assets fs.FS) http.Handler {
	projects, err := newProjectService("", systemCredentialStore{})
	if err != nil {
		panic(err)
	}
	return newServerWithProjects(assets, projects, pickFolder)
}

func newServerWithProjects(assets fs.FS, projects *projectService, folderPicker func() (string, error)) http.Handler {
	return newServerWithProjectsAndCodex(assets, projects, folderPicker, nil)
}

func newServerWithProjectsAndCodex(assets fs.FS, projects *projectService, folderPicker func() (string, error), runtime *codex.Client) http.Handler {
	return newServerWithDependencies(assets, projects, folderPicker, runtime, newBoardService(projects), nil)
}

func newBoardService(projects *projectService) *kanban.Service {
	bundle := newRepositoryBundleOrPanic(projects)
	return kanban.NewService(bundle.Boards)
}

// newRepositoryBundleOrPanic builds the durable repository bundle for the
// project's configured datasource, defaulting to local-json against the app
// data directory when settings.json predates the storageDatasource field.
// Startup fails fast rather than silently falling back to another store when
// the configured datasource is unavailable.
func newRepositoryBundleOrPanic(projects *projectService) storage.RepositoryBundle {
	config, err := projects.storageDatasourceConfig()
	if err != nil {
		panic(fmt.Errorf("read storage datasource configuration: %w", err))
	}
	bundle, err := storage.NewRepositoryBundle(config, projects.directory)
	if err != nil {
		panic(fmt.Errorf("open storage datasource: %w", err))
	}
	return bundle
}

func newServerWithDependencies(assets fs.FS, projects *projectService, folderPicker func() (string, error), runtime *codex.Client, boards *kanban.Service, planner planning.Planner) http.Handler {
	bundle := newRepositoryBundleOrPanic(projects)
	insightService := insights.NewService(bundle.Insights)
	logger, _, err := observability.NewLogger(projects.directory)
	if err != nil {
		panic(err)
	}
	observationService := observability.NewService(bundle.Events, logger)
	notificationService := notifications.NewService(bundle.Notifications)
	buildProfileRepository := bundle.BuildProfiles
	deployProfileRepository := bundle.DeployProfiles
	gitDeliveryRepository := bundle.GitDeliveries
	s := &server{
		assets:             assets,
		fileServer:         http.FileServer(http.FS(assets)),
		mux:                http.NewServeMux(),
		projectService:     projects,
		folderPicker:       folderPicker,
		codexRuntime:       runtime,
		boards:             boards,
		boardEvents:        newBoardEventHub(),
		taskSessions:       newTaskSessionLogManager(),
		designArtifacts:    designartifact.NewService(),
		projectArtifacts:   projectartifact.NewFilesystemStore(),
		teamLead:           planner,
		insights:           insightService,
		observability:      observationService,
		notifications:      notificationService,
		notificationEvents: newNotificationEventHub(),
		toolchain:          toolchain.NewService(),
		gitInspector:       newGitInspector(),
		gitDeliveries:      gitDeliveryRepository,
		buildProfiles:      buildProfileRepository,
		buildDetector:      buildverify.Detector{},
		buildRunner:        buildverify.Runner{DataDirectory: projects.directory, Timeout: 20 * time.Minute},
		buildRuns:          newBuildRunHub(),
		deployProfiles:     deployProfileRepository,
		deployDetector:     deploy.Detector{},
		deployRunner:       deploy.Runner{DataDirectory: projects.directory, Timeout: 20 * time.Minute},
		deployRuns:         newDeployRunHub(),
		autopilotPlans:     make(map[string]struct{}),
	}
	s.gitAvailable = func(ctx context.Context) bool {
		tool, err := s.toolchain.Inspect(ctx, "git", false)
		return err == nil && tool.Installed
	}
	if planner == nil {
		planner = newAIPlanner(projects, runtime)
		s.teamLead = planner
	}
	s.developer = newAIDeveloper(projects, runtime)
	s.designer = newAIDesigner(projects, runtime)
	s.qa = newAIQA(projects, runtime)
	s.sourceScans = newSourceScanManager(func(ctx context.Context, project project, request sourceScanRequest, scanID string) (sourceScanResult, error) {
		started := time.Now()
		ctx, usageCollector := agentusage.WithCollector(ctx)
		defer s.recordAIUsage(request.TraceID, project.ID, "source_scan", scanID, "source-scan", usageCollector)
		provider := s.selectedAIProvider()
		attributes := s.aiRuntimeAttributes(provider)
		attributes["cwd"], attributes["depth"], attributes["focusAreas"], attributes["title"] = project.Path, request.Depth, request.FocusAreas, "Bug scan"
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Bug Scanner started", CorrelationID: request.TraceID, ProjectID: project.ID, EntityType: "source_scan", EntityID: scanID, Agent: "source-scan", Stage: "analysis", Outcome: "running", Attributes: attributes})
		s.agentMu.Lock()
		defer s.agentMu.Unlock()
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Bug Scanner acquired the sequential agent slot", CorrelationID: request.TraceID, ProjectID: project.ID, EntityType: "source_scan", EntityID: scanID, Agent: "source-scan", Stage: "analysis", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})
		var result sourceScanResult
		var runErr error
		instructions, instructionErr := projects.effectiveRoleInstructionsForProjectPath("bug-scanner", project.Path)
		if instructionErr != nil {
			runErr = fmt.Errorf("load Bug Scanner profile: %w", instructionErr)
		} else if provider == "claude-code" {
			result, runErr = runClaudeSourceScan(ctx, project, request, scanID, instructions)
		} else if provider == "github-copilot" {
			result, runErr = runCopilotSourceScan(ctx, project, request, scanID, instructions, projects.agentRuntimeConfig(provider))
		} else {
			result, runErr = runCodexSourceScan(ctx, project, request, scanID, instructions)
		}
		s.recordAIJob(request.TraceID, project.ID, "source_scan", scanID, "source-scan", started, runErr)
		return result, runErr
	}, insightService, s.projectArtifacts)
	s.featureRadar = newFeatureRadarManager(func(ctx context.Context, project project, request featureRadarRequest, analysisID string) (featureRadarResult, error) {
		started := time.Now()
		ctx, usageCollector := agentusage.WithCollector(ctx)
		defer s.recordAIUsage(request.TraceID, project.ID, "feature_radar", analysisID, "feature-radar", usageCollector)
		provider := s.selectedAIProvider()
		runtimeConfig := projects.agentRuntimeConfig(provider)
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Feature Radar started", CorrelationID: request.TraceID, ProjectID: project.ID, EntityType: "feature_radar", EntityID: analysisID, Agent: "feature-radar", Stage: "analysis", Outcome: "running", Attributes: map[string]any{"cwd": project.Path, "depth": request.Depth, "reanalyze": request.Reanalyze, "model": runtimeConfig.Model, "reasoningEffort": runtimeConfig.Effort, "provider": provider, "title": "Feature analysis"}})
		s.agentMu.Lock()
		defer s.agentMu.Unlock()
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Feature Radar acquired the sequential agent slot", CorrelationID: request.TraceID, ProjectID: project.ID, EntityType: "feature_radar", EntityID: analysisID, Agent: "feature-radar", Stage: "analysis", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})
		instructions, instructionErr := projects.effectiveRoleInstructionsForProjectPath("feature-radar", project.Path)
		if instructionErr != nil {
			runErr := fmt.Errorf("load Feature Radar profile: %w", instructionErr)
			s.recordAIJob(request.TraceID, project.ID, "feature_radar", analysisID, "feature-radar", started, runErr)
			return featureRadarResult{}, runErr
		}
		runner := runCodexFeatureRadar(runtime, instructions)
		if provider == "claude-code" {
			runner = runClaudeFeatureRadar(instructions)
		} else if provider == "github-copilot" {
			runner = runCopilotFeatureRadar(instructions, runtimeConfig)
		}
		result, runErr := runner(ctx, project, request, analysisID)
		s.recordAIJob(request.TraceID, project.ID, "feature_radar", analysisID, "feature-radar", started, runErr)
		return result, runErr
	}, insightService, s.projectArtifacts)
	s.projectIntelligence = newProjectStudyManager(func(ctx context.Context, project project, inventory projectStudyInventory, studyID string, progress func(string, string, int)) (projectStudyAIOutput, error) {
		started := time.Now()
		ctx, usageCollector := agentusage.WithCollector(ctx)
		defer s.recordAIUsage(studyID, project.ID, "project_study", studyID, "project-study", usageCollector)
		provider := s.selectedAIProvider()
		runtimeConfig := projects.agentRuntimeConfig(provider)
		attributes := s.aiRuntimeAttributes(provider)
		attributes["cwd"], attributes["title"], attributes["filesScanned"] = project.executionPath(), "Project study", inventory.FilesScanned
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.started", Message: "Project study started", CorrelationID: studyID, ProjectID: project.ID, EntityType: "project_study", EntityID: studyID, Agent: "project-study", Stage: "analysis", Outcome: "running", Attributes: attributes})
		s.agentMu.Lock()
		defer s.agentMu.Unlock()
		observationService.Record(observability.Event{Category: "ai", Name: "ai.job.acquired", Message: "Project study acquired the sequential agent slot", CorrelationID: studyID, ProjectID: project.ID, EntityType: "project_study", EntityID: studyID, Agent: "project-study", Stage: "analysis", Outcome: "running", DurationMS: time.Since(started).Milliseconds()})
		instructions, instructionErr := projects.effectiveRoleInstructionsForProjectPath("team-lead", project.executionPath())
		if instructionErr != nil {
			runErr := fmt.Errorf("load project study profile: %w", instructionErr)
			s.recordAIJob(studyID, project.ID, "project_study", studyID, "project-study", started, runErr)
			return projectStudyAIOutput{}, runErr
		}
		runner := runCodexProjectStudy(runtime, instructions, runtimeConfig)
		if provider == "claude-code" {
			runner = runClaudeProjectStudy(instructions, runtimeConfig)
		} else if provider == "github-copilot" {
			runner = runCopilotProjectStudy(instructions, runtimeConfig)
		}
		result, runErr := runner(ctx, project, inventory, studyID, progress)
		s.recordAIJob(studyID, project.ID, "project_study", studyID, "project-study", started, runErr)
		return result, runErr
	})
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/notifications", s.getNotifications)
	s.mux.HandleFunc("GET /api/notifications/events", s.streamNotifications)
	s.mux.HandleFunc("PATCH /api/notifications/{notificationID}/read", s.markNotificationRead)
	s.mux.HandleFunc("POST /api/notifications/read-all", s.markAllNotificationsRead)
	s.mux.HandleFunc("DELETE /api/notifications/read", s.clearReadNotifications)
	s.mux.HandleFunc("GET /api/boards", s.getBoards)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board", s.getProjectBoard)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board/events", s.streamProjectBoard)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board/tasks/{taskID}/session/events", s.streamTaskSession)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board/plans/{planID}/session/events", s.streamPlanSession)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/queue/safe-stop", s.safeStopBoardQueue)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/queue/continue", s.continueBoardQueue)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board/tasks/{taskID}/artifacts/{artifactID}", s.getTaskDesignArtifact)
	s.mux.HandleFunc("GET /api/projects/{projectID}/board/tasks/{taskID}/design-source", s.getTaskDesignSource)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git/status", s.getProjectGitStatus)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git/commits", s.getProjectGitCommits)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git/delivery-preview", s.getGitDeliveryPreview)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git/deliveries", s.listGitDeliveries)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/deliveries", s.startGitDelivery)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git/deliveries/{deliveryID}", s.getGitDelivery)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/deliveries/{deliveryID}/stop", s.stopGitDelivery)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/deliveries/{deliveryID}/retry", s.retryGitDelivery)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/board/tasks/{taskID}/git", s.setTaskGitReference)
	s.mux.HandleFunc("DELETE /api/projects/{projectID}/board/tasks/{taskID}/git", s.clearTaskGitReference)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/tasks/{taskID}/git/branch", s.createTaskGitBranch)
	s.mux.HandleFunc("GET /api/projects/{projectID}/build-verification", s.getBuildProfile)
	s.mux.HandleFunc("POST /api/projects/{projectID}/build-verification/detect", s.detectBuildProfile)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/build-verification/action", s.selectBuildAction)
	s.mux.HandleFunc("POST /api/projects/{projectID}/build-verification/runs", s.startBuildRun)
	s.mux.HandleFunc("GET /api/projects/{projectID}/build-verification/runs/{runID}/events", s.streamBuildRun)
	s.mux.HandleFunc("GET /api/projects/{projectID}/deploy-action", s.getDeployProfile)
	s.mux.HandleFunc("POST /api/projects/{projectID}/deploy-action/detect", s.detectDeployProfile)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/deploy-action/action", s.selectDeployAction)
	s.mux.HandleFunc("POST /api/projects/{projectID}/deploy-action/runs", s.startDeployRun)
	s.mux.HandleFunc("GET /api/projects/{projectID}/deploy-action/runs/{runID}/events", s.streamDeployRun)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/plans", s.createBoardPlan)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/backlog", s.createBoardBacklogItem)
	s.mux.HandleFunc("POST /api/projects/{projectID}/automation/backlog", s.createBoardBacklogItem)
	s.mux.HandleFunc("GET /api/projects/{projectID}/automation/status", s.listRemoteTickets)
	s.mux.HandleFunc("GET /api/projects/{projectID}/automation/explore/{ticketRef}", s.getRemoteTicket)
	s.mux.HandleFunc("POST /api/projects/{projectID}/automation/autopilot", s.startRemoteAutopilot)
	s.mux.HandleFunc("GET /api/projects/{projectID}/tickets", s.listRemoteTickets)
	s.mux.HandleFunc("POST /api/projects/{projectID}/tickets", s.createBoardBacklogItem)
	s.mux.HandleFunc("GET /api/projects/{projectID}/tickets/{ticketID}", s.getRemoteTicket)
	s.mux.HandleFunc("POST /api/projects/{projectID}/autopilot", s.startRemoteAutopilot)
	s.mux.HandleFunc("GET /api/projects/{projectID}/requests/{requestID}", s.getProjectRequest)
	s.mux.HandleFunc("GET /api/projects/{projectID}/requests/{requestID}/attachments/{attachmentID}", s.getProjectRequestAttachment)
	s.mux.HandleFunc("GET /api/projects/{projectID}/agent-studio", s.getProjectAgentStudio)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/agent-studio/working-standard", s.putProjectWorkingStandard)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/agent-studio/roles/{role}", s.putProjectRoleDefinition)
	s.mux.HandleFunc("PUT /api/projects/{projectID}/agent-studio/agent-skills/{skillID}", s.putProjectAgentSkill)
	s.mux.HandleFunc("GET /api/projects/{projectID}/agent-studio/agents/{role}/effective", s.getProjectEffectiveAgentProfile)
	s.mux.HandleFunc("GET /api/projects/{projectID}/intelligence", s.getProjectIntelligence)
	s.mux.HandleFunc("POST /api/projects/{projectID}/intelligence/studies", s.startProjectStudy)
	s.mux.HandleFunc("GET /api/projects/{projectID}/intelligence/studies/{studyID}/events", s.streamProjectStudy)
	s.mux.HandleFunc("POST /api/projects/{projectID}/intelligence/studies/{studyID}/apply-guidance", s.applyProjectStudyGuidance)
	s.mux.HandleFunc("POST /api/projects/{projectID}/intake/questions", s.generateIntakeQuestions)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/backlog/{itemID}/plan", s.planBoardBacklogItem)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/backlog/{itemID}/remove", s.removeBoardBacklogItem)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/plans/{planID}/review", s.reviewBoardPlan)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/plans/{planID}/retry", s.retryBoardPlan)
	s.mux.HandleFunc("PATCH /api/projects/{projectID}/board/tasks/{taskID}/move", s.moveBoardTask)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/tasks/{taskID}/restart", s.restartBoardTask)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/tasks/{taskID}/approve-design", s.approveDesignBoardTask)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/tasks/{taskID}/design-feedback", s.submitDesignFeedbackBoardTask)
	s.mux.HandleFunc("POST /api/projects/{projectID}/board/tasks/{taskID}/ignore", s.ignoreBoardTask)
	s.mux.HandleFunc("PATCH /api/projects/{projectID}/board/tasks/{taskID}/status", s.updateBoardTaskStatus)
	s.mux.HandleFunc("POST /api/source-scans", s.startSourceScan)
	s.mux.HandleFunc("GET /api/source-scans/latest", s.getLatestSourceScan)
	s.mux.HandleFunc("GET /api/source-scans/{scanID}", s.getSourceScan)
	s.mux.HandleFunc("GET /api/source-scans/{scanID}/events", s.streamSourceScan)
	s.mux.HandleFunc("PATCH /api/source-scans/{projectID}/findings/{findingID}", s.updateSourceFinding)
	s.mux.HandleFunc("POST /api/feature-radar", s.startFeatureRadar)
	s.mux.HandleFunc("GET /api/feature-radar/latest", s.getLatestFeatureRadar)
	s.mux.HandleFunc("GET /api/feature-radar/{analysisID}/events", s.streamFeatureRadar)
	s.mux.HandleFunc("PATCH /api/feature-radar/{projectID}/suggestions/{suggestionID}", s.updateFeatureSuggestion)
	s.mux.HandleFunc("GET /api/observability/overview", s.getObservabilityOverview)
	s.mux.HandleFunc("GET /api/observability/ai-sessions", s.getAISessionOverview)
	s.mux.HandleFunc("GET /api/observability/events", s.getObservabilityEvents)
	s.mux.HandleFunc("GET /api/observability/events/stream", s.streamObservabilityEvents)
	s.mux.HandleFunc("GET /api/settings", s.getSettings)
	s.mux.HandleFunc("GET /api/settings/storage-datasource", s.getStorageDatasource)
	s.mux.HandleFunc("POST /api/settings/storage-datasource/check", s.checkStorageDatasource)
	s.mux.HandleFunc("POST /api/settings/storage-datasource/switch", s.switchStorageDatasource)
	s.mux.HandleFunc("GET /api/settings/backup/export", s.exportBackupSettings)
	s.mux.HandleFunc("POST /api/settings/backup/preview", s.previewBackupSettings)
	s.mux.HandleFunc("POST /api/settings/backup/restore", s.restoreBackupSettings)
	s.mux.HandleFunc("GET /api/settings/agent-model-options", s.getAgentModelOptions)
	s.mux.HandleFunc("PUT /api/settings", s.putSettings)
	s.mux.HandleFunc("PUT /api/settings/email", s.putEmailNotificationSettings)
	s.mux.HandleFunc("POST /api/settings/email/test", s.postEmailNotificationTest)
	s.mux.HandleFunc("PUT /api/settings/theme", s.putTheme)
	s.mux.HandleFunc("PUT /api/settings/roles/{role}", s.putRoleDefinition)
	s.mux.HandleFunc("PUT /api/settings/working-standard", s.putWorkingStandard)
	s.mux.HandleFunc("PUT /api/settings/agent-skills/{skillID}", s.putAgentSkill)
	s.mux.HandleFunc("GET /api/settings/agents/{role}/effective", s.getEffectiveAgentProfile)
	s.mux.HandleFunc("GET /api/system/tools", s.getSystemTools)
	s.mux.HandleFunc("POST /api/system/tools/{toolID}/actions/{action}", s.runSystemToolAction)
	s.mux.HandleFunc("GET /api/projects", s.getProjects)
	s.mux.HandleFunc("GET /api/product-creation/profiles", s.getProductCreationProfiles)
	s.mux.HandleFunc("POST /api/product-creation/blueprints", s.createProductBlueprint)
	s.mux.HandleFunc("GET /api/product-creation/blueprints/{blueprintID}", s.getProductBlueprint)
	s.mux.HandleFunc("POST /api/product-creation/blueprints/{blueprintID}/approve", s.approveProductBlueprint)
	s.mux.HandleFunc("GET /api/workspaces", s.getWorkspaces)
	s.mux.HandleFunc("POST /api/workspaces", s.postWorkspace)
	s.mux.HandleFunc("PUT /api/workspaces/{workspaceID}", s.putWorkspace)
	s.mux.HandleFunc("DELETE /api/projects/{projectID}", s.deleteProject)
	s.mux.HandleFunc("GET /api/git/repositories", s.getGitRepositories)
	s.mux.HandleFunc("GET /api/git/credentials", s.getGitCredentials)
	s.mux.HandleFunc("PUT /api/git/credentials/selected", s.putSelectedGitCredential)
	s.mux.HandleFunc("POST /api/projects/import/git", s.importGitProject)
	s.mux.HandleFunc("POST /api/projects/import/local", s.importLocalProject)
	s.mux.HandleFunc("POST /api/system/select-folder", s.selectFolder)
	s.mux.HandleFunc("/", s.serveSPA)
	s.migrateProjectArtifacts()
	s.recoverInterruptedGitDeliveries()
	s.scheduleQueuedAgentTasks("startup_recovery", "startup-recovery")
	return s.withObservability(s.withDesktopCORS(s.mux))
}

func (s *server) recordAIJob(traceID, projectID, entityType, entityID, agent string, started time.Time, err error) {
	event := observability.Event{Category: "ai", Name: "ai.job.completed", Message: agent + " completed", CorrelationID: traceID, ProjectID: projectID, EntityType: entityType, EntityID: entityID, Agent: agent, Stage: "analysis", Outcome: "success", DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		event.Level = observability.LevelError
		event.Name = "ai.job.failed"
		event.Message = agent + " failed"
		event.Outcome = "failed"
		event.Attributes = map[string]any{"error": err.Error()}
	}
	s.observability.Record(event)
	route := "/observability"
	title := agent + " completed"
	level := notifications.LevelSuccess
	message := "Background AI job completed successfully."
	if entityType == "source_scan" {
		route = "/source-scan"
		title = "Bug Scanner completed"
	}
	if entityType == "feature_radar" {
		route = "/feature-radar"
		title = "Feature Radar completed"
	}
	if entityType == "project_study" {
		route = "/project-atlas"
		title = "Project study completed"
		message = "Project knowledge and visualizations are ready."
	}
	if err != nil {
		level = notifications.LevelError
		title = agent + " needs attention"
		message = err.Error()
	}
	s.notify(notifications.Draft{Level: level, Kind: entityType, Title: title, Message: message, ProjectID: projectID, EntityID: entityID, Route: route})
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	codexStatus := "unavailable"
	if s.codexRuntime != nil {
		codexStatus = "connected"
	}
	provider := s.selectedAIProvider()
	runtimeStatus := codexStatus
	if provider == "claude-code" {
		if _, err := exec.LookPath("claude"); err == nil {
			runtimeStatus = "connected"
		}
	}
	if provider == "github-copilot" {
		if _, err := exec.LookPath("gh"); err == nil {
			runtimeStatus = "connected"
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":         "ready",
		"codexAppServer": codexStatus,
		"aiRuntime":      runtimeStatus,
		"aiProvider":     provider,
	})
}

func (s *server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestedPath := strings.TrimPrefix(r.URL.Path, "/")
	if requestedPath != "" {
		if info, err := fs.Stat(s.assets, requestedPath); err == nil && !info.IsDir() {
			if requestedPath == "index.html" {
				setSPAIndexHeaders(w)
			}
			s.fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(requestedPath, ".") {
			http.NotFound(w, r)
			return
		}
	}

	index, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		http.Error(w, "Angular frontend has not been built; run npm run build:go in frontend", http.StatusServiceUnavailable)
		return
	}
	setSPAIndexHeaders(w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}

func setSPAIndexHeaders(w http.ResponseWriter) {
	// A cached index can reference fingerprinted bundles removed by the next
	// build, leaving Angular rendered without its stylesheet or lazy chunks.
	// Always revalidate the shell while allowing the browser to cache the
	// fingerprinted assets referenced by the current document normally.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
