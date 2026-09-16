package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/claude"
	"github.com/theanh2906/AI-Product-Team/internal/codex"
	"github.com/theanh2906/AI-Product-Team/internal/copilot"
	"github.com/theanh2906/AI-Product-Team/internal/kanban"
	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
)

const productCreationSchemaVersion = 1

var productSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type productCreationProfile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

type productBlueprintRequest struct {
	ProductName       string `json:"productName"`
	Idea              string `json:"idea"`
	DestinationParent string `json:"destinationParent"`
	ProfileID         string `json:"profileId"`
	StartingPoint     string `json:"startingPoint"`
	InitializeGit     bool   `json:"initializeGit"`
}

type productBlueprintComponent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type productBlueprintMilestone struct {
	Title string   `json:"title"`
	Roles []string `json:"roles"`
	Tasks int      `json:"tasks"`
}

type productCreationPreflight struct {
	Ready    bool     `json:"ready"`
	Warnings []string `json:"warnings"`
	Tools    []string `json:"tools"`
}

type productBlueprint struct {
	SchemaVersion int                         `json:"schemaVersion"`
	ID            string                      `json:"id"`
	Status        string                      `json:"status"`
	ProductName   string                      `json:"productName"`
	Idea          string                      `json:"idea"`
	Destination   string                      `json:"destination"`
	ProfileID     string                      `json:"profileId"`
	ProfileName   string                      `json:"profileName"`
	StartingPoint string                      `json:"startingPoint"`
	InitializeGit bool                        `json:"initializeGit"`
	Summary       string                      `json:"summary"`
	Criteria      []string                    `json:"criteria"`
	Components    []productBlueprintComponent `json:"components"`
	Milestones    []productBlueprintMilestone `json:"milestones"`
	Decisions     []string                    `json:"decisions"`
	Preflight     productCreationPreflight    `json:"preflight"`
	CreatedAt     time.Time                   `json:"createdAt"`
	UpdatedAt     time.Time                   `json:"updatedAt"`
	Source        string                      `json:"source"`
	Warning       string                      `json:"warning,omitempty"`
}

type productCreationResult struct {
	Project       project `json:"project"`
	BacklogItemID string  `json:"backlogItemId"`
}

func creationProfiles() []productCreationProfile {
	return []productCreationProfile{
		{ID: "angular-go", Name: "Angular + Go", Description: "Web product with an Angular client and Go API.", Tools: []string{"node", "npm", "go"}},
		{ID: "angular-go-sqlite", Name: "Angular + Go + SQLite", Description: "Full-stack product with local persistence.", Tools: []string{"node", "npm", "go"}},
		{ID: "tauri-angular-go", Name: "Tauri + Angular + Go", Description: "Windows desktop product with a native shell.", Tools: []string{"node", "npm", "go", "cargo"}},
	}
}

func (s *server) getProductCreationProfiles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, creationProfiles())
}

func (s *server) createProductBlueprint(w http.ResponseWriter, r *http.Request) {
	var request productBlueprintRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	blueprint, err := s.buildProductBlueprint(request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	provider := s.selectedAIProvider()
	s.agentMu.Lock()
	enriched, generationErr := s.enrichProductBlueprint(r.Context(), blueprint, provider)
	s.agentMu.Unlock()
	if generationErr == nil {
		blueprint = enriched
		blueprint.Source = provider
	} else {
		blueprint.Source = "profile"
		blueprint.Warning = "Local AI enrichment was unavailable, so ProductCrew used the safe profile-driven blueprint."
	}
	if err := s.saveProductBlueprint(blueprint); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, blueprint)
}

func (s *server) getProductBlueprint(w http.ResponseWriter, r *http.Request) {
	blueprint, err := s.loadProductBlueprint(r.PathValue("blueprintID"))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Product blueprint was not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, blueprint)
}

func (s *server) approveProductBlueprint(w http.ResponseWriter, r *http.Request) {
	blueprint, err := s.loadProductBlueprint(r.PathValue("blueprintID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if blueprint.Status == "created" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "This blueprint has already been created."})
		return
	}
	result, err := s.scaffoldProduct(r.Context(), &blueprint)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	blueprint.Status = "created"
	blueprint.UpdatedAt = time.Now().UTC()
	_ = s.saveProductBlueprint(blueprint)
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) buildProductBlueprint(request productBlueprintRequest) (productBlueprint, error) {
	name := strings.TrimSpace(request.ProductName)
	idea := strings.TrimSpace(request.Idea)
	parent, err := filepath.Abs(strings.TrimSpace(request.DestinationParent))
	if err != nil || parent == "" {
		return productBlueprint{}, fmt.Errorf("choose an absolute destination folder")
	}
	if len(name) < 2 || len(name) > 60 || !productSlugPattern.MatchString(name) {
		return productBlueprint{}, fmt.Errorf("product name must use 2 to 60 lowercase letters, numbers, and single hyphens")
	}
	if len(idea) < 20 || len(idea) > 6000 {
		return productBlueprint{}, fmt.Errorf("describe the product outcome in 20 to 6000 characters")
	}
	profile, ok := findCreationProfile(request.ProfileID)
	if !ok {
		return productBlueprint{}, fmt.Errorf("select a supported product foundation")
	}
	info, statErr := os.Stat(parent)
	if statErr != nil || !info.IsDir() {
		return productBlueprint{}, fmt.Errorf("destination parent folder does not exist")
	}
	target := filepath.Join(parent, name)
	if !pathWithin(parent, target) {
		return productBlueprint{}, fmt.Errorf("destination must stay inside the selected parent folder")
	}
	if _, statErr := os.Stat(target); statErr == nil {
		return productBlueprint{}, fmt.Errorf("destination folder already exists")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return productBlueprint{}, fmt.Errorf("inspect destination: %w", statErr)
	}
	missing := []string{}
	available := []string{}
	tools := append([]string{}, profile.Tools...)
	if request.InitializeGit {
		tools = append(tools, "git")
	}
	sort.Strings(tools)
	for _, tool := range tools {
		if _, lookErr := exec.LookPath(tool); lookErr != nil {
			missing = append(missing, tool+" is not installed or not available on PATH")
		} else {
			available = append(available, tool)
		}
	}
	components := []productBlueprintComponent{
		{Name: "Angular application", Path: "frontend/", Description: "Product UI, navigation, state and API client."},
		{Name: "Go service", Path: "cmd/server/ + internal/", Description: "Local REST API, health checks and product logic."},
		{Name: "ProductCrew context", Path: ".productcrew/", Description: "Approved blueprint and project-local agent instructions."},
	}
	if profile.ID == "angular-go-sqlite" {
		components = append(components, productBlueprintComponent{Name: "SQLite store", Path: "data/", Description: "Local persistence with explicit migrations."})
	}
	if profile.ID == "tauri-angular-go" {
		components = append(components, productBlueprintComponent{Name: "Tauri shell", Path: "src-tauri/", Description: "Native desktop lifecycle and packaging."})
	}
	now := time.Now().UTC()
	return productBlueprint{
		SchemaVersion: productCreationSchemaVersion, ID: fmt.Sprintf("blueprint-%d", now.UnixNano()), Status: "draft",
		ProductName: name, Idea: idea, Destination: target, ProfileID: profile.ID, ProfileName: profile.Name,
		StartingPoint: normalizedStartingPoint(request.StartingPoint), InitializeGit: request.InitializeGit,
		Summary:    idea,
		Criteria:   []string{"The generated foundation starts with documented local commands", "The first delivery plan is visible in ProductCrew Work Board", "No secrets or external services are provisioned automatically"},
		Components: components,
		Milestones: []productBlueprintMilestone{
			{Title: "Foundation and first runnable slice", Roles: []string{"Team Lead", "Developer"}, Tasks: 3},
			{Title: "Core product experience", Roles: []string{"Designer", "Developer"}, Tasks: 4},
			{Title: "Build and acceptance", Roles: []string{"Developer", "QA"}, Tasks: 2},
		},
		Decisions: []string{"Local-first project", profile.Name, "Serial ProductCrew delivery queue", "Secrets configured after scaffold"},
		Preflight: productCreationPreflight{Ready: len(missing) == 0, Warnings: missing, Tools: available}, CreatedAt: now, UpdatedAt: now, Source: "profile",
	}, nil
}

type productBlueprintEnrichment struct {
	Summary    string                      `json:"summary"`
	Criteria   []string                    `json:"criteria"`
	Components []productBlueprintComponent `json:"components"`
	Milestones []productBlueprintMilestone `json:"milestones"`
	Decisions  []string                    `json:"decisions"`
}

var productBlueprintSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"summary", "criteria", "components", "milestones", "decisions"},
	"properties": map[string]any{
		"summary":    map[string]any{"type": "string"},
		"criteria":   map[string]any{"type": "array", "minItems": 3, "maxItems": 6, "items": map[string]any{"type": "string"}},
		"components": map[string]any{"type": "array", "minItems": 3, "maxItems": 8, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"name", "path", "description"}, "properties": map[string]any{"name": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}}},
		"milestones": map[string]any{"type": "array", "minItems": 2, "maxItems": 5, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"title", "roles", "tasks"}, "properties": map[string]any{"title": map[string]any{"type": "string"}, "roles": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": map[string]any{"type": "string"}}, "tasks": map[string]any{"type": "integer", "minimum": 1, "maximum": 8}}}},
		"decisions":  map[string]any{"type": "array", "minItems": 3, "maxItems": 6, "items": map[string]any{"type": "string"}},
	},
}

func (s *server) enrichProductBlueprint(ctx context.Context, blueprint productBlueprint, provider string) (productBlueprint, error) {
	draftDirectory := filepath.Join(s.projectService.directory, "creation-drafts", blueprint.ID)
	if err := os.MkdirAll(draftDirectory, 0o700); err != nil {
		return blueprint, err
	}
	prompt := fmt.Sprintf(`Shape a concise product blueprint from the approved brief. Keep the selected foundation exactly as given. Do not propose secrets, cloud provisioning, external side effects, or files outside the destination. Components must use repository-relative paths. Roles may only be Team Lead, Designer, Developer, and QA.

PRODUCT: %s
FOUNDATION: %s
STARTING POINT: %s
BRIEF:
%s`, blueprint.ProductName, blueprint.ProfileName, blueprint.StartingPoint, blueprint.Idea)
	instructions := "You are ProductCrew Blueprint Architect. Return only the requested structured JSON. Produce implementation-ready outcomes without writing files or executing commands."
	config := s.projectService.agentRuntimeConfig(provider)
	var payload []byte
	if provider == "claude-code" {
		result, err := claude.RunJSON(ctx, prompt, claude.RunConfig{CWD: draftDirectory, SystemPrompt: instructions, Model: config.Model, Effort: config.Effort, Schema: productBlueprintSchema, PermissionMode: "dontAsk", AllowedTools: claude.ReadOnlyTools(), Timeout: 90 * time.Second})
		if err != nil {
			return blueprint, err
		}
		payload = result.StructuredOutput
		if len(payload) == 0 {
			payload = []byte(result.Result)
		}
	} else if provider == "github-copilot" {
		result, err := copilot.RunJSON(ctx, prompt, copilot.RunConfig{CWD: draftDirectory, SystemPrompt: instructions, Model: config.Model, Effort: config.Effort, Schema: productBlueprintSchema, Writable: false, Timeout: 90 * time.Second})
		if err != nil {
			return blueprint, err
		}
		payload = copilot.Payload(result)
	} else {
		if s.codexRuntime == nil {
			return blueprint, fmt.Errorf("Codex app-server is unavailable")
		}
		threadID, err := s.codexRuntime.StartThread(ctx, codex.ThreadConfig{CWD: draftDirectory, DeveloperInstructions: instructions, Sandbox: "read-only", ApprovalPolicy: "never", Ephemeral: true})
		if err != nil {
			return blueprint, err
		}
		turn, err := s.codexRuntime.RunTurn(ctx, threadID, prompt, codex.TurnConfig{Effort: config.Effort, CWD: draftDirectory, OutputSchema: productBlueprintSchema})
		if err != nil {
			return blueprint, err
		}
		payload = []byte(turn.FinalResponse)
	}
	var value productBlueprintEnrichment
	if err := json.Unmarshal(payload, &value); err != nil {
		return blueprint, fmt.Errorf("decode product blueprint: %w", err)
	}
	if err := validateBlueprintEnrichment(value); err != nil {
		return blueprint, err
	}
	blueprint.Summary, blueprint.Criteria, blueprint.Components, blueprint.Milestones, blueprint.Decisions = strings.TrimSpace(value.Summary), value.Criteria, value.Components, value.Milestones, value.Decisions
	blueprint.UpdatedAt = time.Now().UTC()
	return blueprint, nil
}

func validateBlueprintEnrichment(value productBlueprintEnrichment) error {
	if len(strings.TrimSpace(value.Summary)) < 20 || len(value.Criteria) < 3 || len(value.Components) < 3 || len(value.Milestones) < 2 || len(value.Decisions) < 3 {
		return fmt.Errorf("AI returned an incomplete product blueprint")
	}
	allowedRoles := map[string]bool{"Team Lead": true, "Designer": true, "Developer": true, "QA": true}
	for _, component := range value.Components {
		path := filepath.Clean(strings.TrimSpace(component.Path))
		if path == "." || filepath.IsAbs(path) || strings.HasPrefix(path, "..") {
			return fmt.Errorf("AI returned an unsafe component path")
		}
	}
	for _, milestone := range value.Milestones {
		for _, role := range milestone.Roles {
			if !allowedRoles[role] {
				return fmt.Errorf("AI returned an unsupported delivery role")
			}
		}
	}
	return nil
}

func (s *server) scaffoldProduct(ctx context.Context, blueprint *productBlueprint) (productCreationResult, error) {
	if !blueprint.Preflight.Ready {
		return productCreationResult{}, fmt.Errorf("install the required toolchain before creating this product")
	}
	parent := filepath.Dir(blueprint.Destination)
	staging := filepath.Join(parent, "."+blueprint.ProductName+".productcrew-staging")
	if !pathWithin(parent, staging) || !pathWithin(parent, blueprint.Destination) {
		return productCreationResult{}, fmt.Errorf("invalid scaffold destination")
	}
	if _, err := os.Stat(staging); err == nil {
		return productCreationResult{}, fmt.Errorf("a previous staging folder needs attention: %s", staging)
	} else if !errors.Is(err, os.ErrNotExist) {
		return productCreationResult{}, fmt.Errorf("inspect staging folder: %w", err)
	}
	if _, err := os.Stat(blueprint.Destination); err == nil {
		return productCreationResult{}, fmt.Errorf("destination folder already exists")
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return productCreationResult{}, fmt.Errorf("create staging folder: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := writeProductFoundation(staging, *blueprint); err != nil {
		return productCreationResult{}, err
	}
	if blueprint.InitializeGit {
		command := exec.CommandContext(ctx, "git", "init", "--initial-branch=main")
		command.Dir = staging
		if output, err := command.CombinedOutput(); err != nil {
			return productCreationResult{}, fmt.Errorf("initialize Git: %s", truncateMessage(string(output)))
		}
	}
	if err := os.Rename(staging, blueprint.Destination); err != nil {
		return productCreationResult{}, fmt.Errorf("publish scaffold: %w", err)
	}
	published = true
	created, err := s.projectService.importLocal(blueprint.Destination)
	if err != nil {
		return productCreationResult{}, fmt.Errorf("register created project: %w", err)
	}
	requestID := projectartifact.NewRequestID()
	manifest, err := s.projectArtifacts.SaveRequest(ctx, created.Path, projectartifact.RequestInput{
		RequestID: requestID, ProjectID: created.ID, Title: "Build " + blueprint.ProductName + " from approved blueprint",
		Description: blueprint.Idea, Source: "product-forge", SourceReference: blueprint.ID,
	})
	if err != nil {
		return productCreationResult{}, fmt.Errorf("persist creation request: %w", err)
	}
	board, item, err := s.boards.AddBacklogItem(ctx, created.ID, created.Name, kanban.BacklogDraft{
		RequestID: requestID, ArtifactPath: manifest.Directory, Type: kanban.BacklogFeature, Source: "product-forge", SourceReference: blueprint.ID,
		Title: "Build " + blueprint.ProductName + " from approved blueprint", Description: blueprint.Idea, DeliveryTarget: "full-stack", RequiresUI: true,
		AcceptanceCriteria: blueprint.Criteria, Feasibility: 100,
	})
	if err != nil {
		return productCreationResult{}, fmt.Errorf("create initial Work Board request: %w", err)
	}
	s.boardEvents.publish(board)
	return productCreationResult{Project: created, BacklogItemID: item.ID}, nil
}

func writeProductFoundation(root string, blueprint productBlueprint) error {
	files := map[string]string{
		"README.md":                  "# " + blueprint.ProductName + "\n\n" + blueprint.Idea + "\n\n## Start here\n\n1. Review `.productcrew/blueprint.json`.\n2. Open the project in ProductCrew Work Board.\n3. Route the generated backlog item to Team Lead.\n",
		"AGENTS.md":                  "# Project instructions\n\nFollow the approved ProductCrew blueprint in `.productcrew/blueprint.json`. Keep changes scoped, testable, and documented.\n",
		".gitignore":                 "node_modules/\ndist/\n.tmp/\n.env\n*.local\n",
		"go.mod":                     "module " + blueprint.ProductName + "\n\ngo 1.25\n",
		"cmd/server/main.go":         "package main\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nfunc main() {\n\thttp.HandleFunc(\"/api/health\", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{\"status\":\"ok\"}`)) })\n\tfmt.Println(\"API listening on http://127.0.0.1:8080\")\n\t_ = http.ListenAndServe(\"127.0.0.1:8080\", nil)\n}\n",
		"frontend/package.json":      "{\n  \"name\": \"" + blueprint.ProductName + "-frontend\",\n  \"private\": true,\n  \"scripts\": {\n    \"start\": \"ng serve\",\n    \"build\": \"ng build\",\n    \"test\": \"ng test --watch=false\"\n  },\n  \"dependencies\": {\n    \"@angular/common\": \"^21.0.0\",\n    \"@angular/core\": \"^21.0.0\",\n    \"@angular/platform-browser\": \"^21.0.0\",\n    \"@angular/router\": \"^21.0.0\",\n    \"rxjs\": \"^7.8.0\",\n    \"zone.js\": \"^0.15.0\"\n  },\n  \"devDependencies\": {\n    \"@angular/build\": \"^21.0.0\",\n    \"@angular/cli\": \"^21.0.0\",\n    \"typescript\": \"~5.9.0\"\n  }\n}\n",
		"frontend/angular.json":      "{\n  \"$schema\": \"./node_modules/@angular/cli/lib/config/schema.json\",\n  \"version\": 1,\n  \"projects\": {\n    \"app\": {\n      \"projectType\": \"application\",\n      \"root\": \"\",\n      \"sourceRoot\": \"src\",\n      \"architect\": {\n        \"build\": { \"builder\": \"@angular/build:application\", \"options\": { \"browser\": \"src/main.ts\", \"index\": \"src/index.html\", \"tsConfig\": \"tsconfig.app.json\", \"styles\": [\"src/styles.css\"] } },\n        \"serve\": { \"builder\": \"@angular/build:dev-server\", \"configurations\": { \"development\": { \"buildTarget\": \"app:build\" } }, \"defaultConfiguration\": \"development\" }\n      }\n    }\n  }\n}\n",
		"frontend/tsconfig.json":     "{\n  \"compilerOptions\": { \"target\": \"ES2022\", \"useDefineForClassFields\": false, \"strict\": true, \"module\": \"preserve\", \"moduleResolution\": \"bundler\", \"lib\": [\"ES2022\", \"dom\"], \"skipLibCheck\": true },\n  \"angularCompilerOptions\": { \"strictTemplates\": true }\n}\n",
		"frontend/tsconfig.app.json": "{ \"extends\": \"./tsconfig.json\", \"compilerOptions\": { \"outDir\": \"./out-tsc/app\" }, \"files\": [\"src/main.ts\"] }\n",
		"frontend/src/index.html":    "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>" + blueprint.ProductName + "</title></head><body><app-root></app-root></body></html>\n",
		"frontend/src/main.ts":       "import { Component } from '@angular/core';\nimport { bootstrapApplication } from '@angular/platform-browser';\n\n@Component({ selector: 'app-root', standalone: true, template: `<main><p>PRODUCT FOUNDATION</p><h1>" + blueprint.ProductName + "</h1><span>The approved ProductCrew blueprint is ready for delivery.</span></main>` })\nclass App {}\n\nbootstrapApplication(App).catch(console.error);\n",
		"frontend/src/styles.css":    ":root{font-family:Inter,ui-sans-serif,system-ui;color:#e8f4f1;background:#07110f}*{box-sizing:border-box}body{margin:0}main{min-height:100vh;display:grid;place-content:center;padding:48px;text-align:center}p{color:#44c4b6;font-size:12px;font-weight:800;letter-spacing:.15em}h1{margin:8px 0;font-size:clamp(44px,8vw,90px)}span{color:#9bb0aa}\n",
	}
	data, err := json.MarshalIndent(blueprint, "", "  ")
	if err != nil {
		return err
	}
	files[".productcrew/blueprint.json"] = string(data) + "\n"
	for relative, content := range files {
		target := filepath.Join(root, filepath.FromSlash(relative))
		if !pathWithin(root, target) {
			return fmt.Errorf("invalid scaffold path: %s", relative)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create scaffold directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	if blueprint.ProfileID == "angular-go-sqlite" {
		if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
			return err
		}
	}
	if blueprint.ProfileID == "tauri-angular-go" {
		cargo := "[package]\nname = \"" + blueprint.ProductName + "\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\ntauri = { version = \"2\" }\n"
		if err := os.MkdirAll(filepath.Join(root, "src-tauri", "src"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "src-tauri", "Cargo.toml"), []byte(cargo), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "src-tauri", "src", "main.rs"), []byte("fn main() { println!(\"ProductCrew desktop foundation\"); }\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) blueprintPath(id string) (string, error) {
	if !strings.HasPrefix(id, "blueprint-") || strings.ContainsAny(id, `/\\`) {
		return "", fmt.Errorf("invalid blueprint id")
	}
	return filepath.Join(s.projectService.directory, "creation-drafts", id+".json"), nil
}

func (s *server) saveProductBlueprint(blueprint productBlueprint) error {
	path, err := s.blueprintPath(blueprint.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create blueprint storage: %w", err)
	}
	return writeJSONFile(path, blueprint)
}

func (s *server) loadProductBlueprint(id string) (productBlueprint, error) {
	path, err := s.blueprintPath(id)
	if err != nil {
		return productBlueprint{}, err
	}
	var blueprint productBlueprint
	if err := readJSONFile(path, &blueprint); err != nil {
		return productBlueprint{}, err
	}
	return blueprint, nil
}

func findCreationProfile(id string) (productCreationProfile, bool) {
	for _, profile := range creationProfiles() {
		if profile.ID == strings.TrimSpace(id) {
			return profile, true
		}
	}
	return productCreationProfile{}, false
}

func normalizedStartingPoint(value string) string {
	switch strings.TrimSpace(value) {
	case "guided", "template":
		return strings.TrimSpace(value)
	default:
		return "blank"
	}
}
