package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	workspaceSchemaVersion   = 1
	workspaceAccessReadOnly  = "read-only"
	workspaceAccessReadWrite = "read-write"
)

var errWorkspaceNotFound = errors.New("workspace not found")

type workspaceFolder struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Access    string `json:"access"`
	Primary   bool   `json:"primary,omitempty"`
}

type workspaceSettings struct {
	QueueMode string `json:"queueMode"`
}

type workspaceDefinition struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Folders       []workspaceFolder `json:"folders"`
	Settings      workspaceSettings `json:"settings"`
	FilePath      string            `json:"filePath,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type workspaceFolderInput struct {
	ProjectID string `json:"projectId"`
	Access    string `json:"access"`
	Primary   bool   `json:"primary"`
}

type createWorkspaceRequest struct {
	Name    string                 `json:"name"`
	Folders []workspaceFolderInput `json:"folders"`
}

func (s *projectService) listWorkspaces() ([]workspaceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listWorkspacesLocked()
}

func (s *projectService) listWorkspacesLocked() ([]workspaceDefinition, error) {
	root := filepath.Join(s.directory, "workspaces")
	projects, err := s.readProjects()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []workspaceDefinition{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workspace directory: %w", err)
	}
	workspaces := make([]workspaceDefinition, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		matches, matchErr := filepath.Glob(filepath.Join(root, entry.Name(), "*.workspace"))
		if matchErr != nil || len(matches) == 0 {
			continue
		}
		workspace, readErr := readWorkspaceFile(matches[0])
		if readErr != nil {
			continue
		}
		workspace, readErr = normalizeWorkspaceFolders(workspace, projects)
		if readErr != nil {
			continue
		}
		workspaces = append(workspaces, workspace)
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].UpdatedAt.After(workspaces[j].UpdatedAt) })
	return workspaces, nil
}

func (s *projectService) createWorkspace(request createWorkspaceRequest) (workspaceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(request.Name)
	if len(name) < 3 || len(name) > 80 {
		return workspaceDefinition{}, fmt.Errorf("workspace name must be between 3 and 80 characters")
	}
	if len(request.Folders) < 2 {
		return workspaceDefinition{}, fmt.Errorf("a workspace requires at least two projects")
	}
	projects, err := s.readProjects()
	if err != nil {
		return workspaceDefinition{}, err
	}
	folders, err := resolveWorkspaceFolderInputs(request.Folders, projects)
	if err != nil {
		return workspaceDefinition{}, err
	}
	id, err := newWorkspaceID()
	if err != nil {
		return workspaceDefinition{}, err
	}
	now := time.Now().UTC()
	directory := filepath.Join(s.directory, "workspaces", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return workspaceDefinition{}, fmt.Errorf("create workspace directory: %w", err)
	}
	filePath := filepath.Join(directory, workspaceSlug(name)+".workspace")
	workspace := workspaceDefinition{
		SchemaVersion: workspaceSchemaVersion,
		ID:            id, Name: name, Folders: folders,
		Settings: workspaceSettings{QueueMode: "serial"},
		FilePath: filePath, CreatedAt: now, UpdatedAt: now,
	}
	if err := writeWorkspaceFile(workspace); err != nil {
		return workspaceDefinition{}, err
	}
	return workspace, nil
}

func (s *projectService) updateWorkspace(id string, request createWorkspaceRequest) (workspaceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, err := s.findWorkspaceLocked(id)
	if err != nil {
		return workspaceDefinition{}, err
	}
	name := strings.TrimSpace(request.Name)
	if len(name) < 3 || len(name) > 80 {
		return workspaceDefinition{}, fmt.Errorf("workspace name must be between 3 and 80 characters")
	}
	projects, err := s.readProjects()
	if err != nil {
		return workspaceDefinition{}, err
	}
	folders, err := resolveWorkspaceFolderInputs(request.Folders, projects)
	if err != nil {
		return workspaceDefinition{}, err
	}
	workspace.Name = name
	workspace.Folders = folders
	workspace.Settings.QueueMode = "serial"
	workspace.UpdatedAt = time.Now().UTC()
	if err := writeWorkspaceFile(workspace); err != nil {
		return workspaceDefinition{}, err
	}
	return workspace, nil
}

func resolveWorkspaceFolderInputs(inputs []workspaceFolderInput, projects []project) ([]workspaceFolder, error) {
	if len(inputs) < 2 {
		return nil, fmt.Errorf("a workspace requires at least two projects")
	}
	byID := make(map[string]project, len(projects))
	for _, candidate := range projects {
		byID[candidate.ID] = candidate
	}
	seen := make(map[string]struct{}, len(inputs))
	folders := make([]workspaceFolder, 0, len(inputs))
	primaryCount := 0
	for index, input := range inputs {
		candidate, ok := byID[strings.TrimSpace(input.ProjectID)]
		if !ok {
			return nil, fmt.Errorf("workspace project %q is not imported", input.ProjectID)
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			return nil, fmt.Errorf("workspace project %q is duplicated", candidate.Name)
		}
		seen[candidate.ID] = struct{}{}
		access := strings.ToLower(strings.TrimSpace(input.Access))
		if access == "" {
			access = workspaceAccessReadWrite
		}
		if access != workspaceAccessReadOnly && access != workspaceAccessReadWrite {
			return nil, fmt.Errorf("unsupported workspace access %q", input.Access)
		}
		primary := input.Primary
		if index == 0 && primaryCount == 0 {
			primary = true
		}
		if primary {
			primaryCount++
			if access != workspaceAccessReadWrite {
				return nil, fmt.Errorf("the primary workspace project must be writable")
			}
		}
		folders = append(folders, workspaceFolder{ProjectID: candidate.ID, Name: candidate.Name, Path: candidate.Path, Access: access, Primary: primary})
	}
	if primaryCount != 1 {
		return nil, fmt.Errorf("a workspace requires exactly one primary project")
	}
	return folders, nil
}

func (s *projectService) findWorkspaceLocked(id string) (workspaceDefinition, error) {
	workspaces, err := s.listWorkspacesLocked()
	if err != nil {
		return workspaceDefinition{}, err
	}
	for _, workspace := range workspaces {
		if workspace.ID == strings.TrimSpace(id) {
			return workspace, nil
		}
	}
	return workspaceDefinition{}, errWorkspaceNotFound
}

func workspaceProject(workspace workspaceDefinition) project {
	path := ""
	for _, folder := range workspace.Folders {
		if folder.Primary {
			path = folder.Path
			break
		}
	}
	return project{
		ID: workspace.ID, Name: workspace.Name, Path: path, Source: "workspace", Kind: "workspace",
		ImportedAt: workspace.CreatedAt, WorkspaceFile: workspace.FilePath, WorkspaceFolders: workspace.Folders,
	}
}

func readWorkspaceFile(path string) (workspaceDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workspaceDefinition{}, err
	}
	var workspace workspaceDefinition
	if err := json.Unmarshal(data, &workspace); err != nil {
		return workspaceDefinition{}, fmt.Errorf("decode workspace file: %w", err)
	}
	workspace.FilePath = path
	if workspace.SchemaVersion != workspaceSchemaVersion || strings.TrimSpace(workspace.ID) == "" || len(workspace.Folders) < 2 {
		return workspaceDefinition{}, fmt.Errorf("invalid ProductCrew workspace file")
	}
	return workspace, nil
}

func normalizeWorkspaceFolders(workspace workspaceDefinition, projects []project) (workspaceDefinition, error) {
	byID := make(map[string]project, len(projects))
	for _, candidate := range projects {
		byID[candidate.ID] = candidate
	}
	seen := make(map[string]struct{}, len(workspace.Folders))
	primaryCount := 0
	for index, folder := range workspace.Folders {
		candidate, ok := byID[strings.TrimSpace(folder.ProjectID)]
		if !ok {
			return workspaceDefinition{}, fmt.Errorf("workspace project %q is no longer imported", folder.ProjectID)
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			return workspaceDefinition{}, fmt.Errorf("workspace project %q is duplicated", candidate.Name)
		}
		seen[candidate.ID] = struct{}{}
		if folder.Access != workspaceAccessReadOnly && folder.Access != workspaceAccessReadWrite {
			return workspaceDefinition{}, fmt.Errorf("unsupported workspace access %q", folder.Access)
		}
		if folder.Primary {
			primaryCount++
			if folder.Access != workspaceAccessReadWrite {
				return workspaceDefinition{}, fmt.Errorf("the primary workspace project must be writable")
			}
		}
		workspace.Folders[index].Name = candidate.Name
		workspace.Folders[index].Path = candidate.Path
	}
	if primaryCount != 1 {
		return workspaceDefinition{}, fmt.Errorf("a workspace requires exactly one primary project")
	}
	return workspace, nil
}

func writeWorkspaceFile(workspace workspaceDefinition) error {
	filePath := workspace.FilePath
	workspace.FilePath = ""
	data, err := json.MarshalIndent(workspace, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace file: %w", err)
	}
	temporary := filePath + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write workspace file: %w", err)
	}
	if err := os.Rename(temporary, filePath); err != nil {
		return fmt.Errorf("replace workspace file: %w", err)
	}
	return nil
}

func newWorkspaceID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate workspace id: %w", err)
	}
	return "workspace-" + hex.EncodeToString(value), nil
}

func workspaceSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	slug := strings.Trim(result.String(), "-")
	if slug == "" {
		return "workspace"
	}
	return slug
}
