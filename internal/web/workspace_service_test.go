package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateWorkspacePersistsDefinitionAndResolvesExecutionRoots(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	primaryPath := filepath.Join(directory, "frontend")
	additionalPath := filepath.Join(directory, "service")
	projects := []project{
		{ID: "project-frontend", Name: "Frontend", Path: primaryPath, Source: "local", ImportedAt: time.Now().UTC()},
		{ID: "project-service", Name: "Service", Path: additionalPath, Source: "local", ImportedAt: time.Now().UTC()},
	}
	if err := service.writeProjects(projects); err != nil {
		t.Fatal(err)
	}

	workspace, err := service.createWorkspace(createWorkspaceRequest{
		Name: "Product platform",
		Folders: []workspaceFolderInput{
			{ProjectID: projects[0].ID, Access: workspaceAccessReadWrite, Primary: true},
			{ProjectID: projects[1].ID, Access: workspaceAccessReadWrite},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Settings.QueueMode != "serial" || filepath.Ext(workspace.FilePath) != ".workspace" {
		t.Fatalf("unexpected workspace definition: %+v", workspace)
	}
	if _, err := os.Stat(workspace.FilePath); err != nil {
		t.Fatalf("workspace file was not persisted: %v", err)
	}

	resolved, err := service.findProject(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Kind != "workspace" || resolved.executionPath() != primaryPath {
		t.Fatalf("unexpected virtual workspace project: %+v", resolved)
	}
	additional := resolved.additionalExecutionPaths()
	if len(additional) != 1 || additional[0] != additionalPath {
		t.Fatalf("unexpected additional execution roots: %v", additional)
	}

	updated, err := service.updateWorkspace(workspace.ID, createWorkspaceRequest{
		Name: "Product platform updated",
		Folders: []workspaceFolderInput{
			{ProjectID: projects[0].ID, Access: workspaceAccessReadWrite, Primary: true},
			{ProjectID: projects[1].ID, Access: workspaceAccessReadOnly},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Product platform updated" || updated.Folders[1].Access != workspaceAccessReadOnly || updated.FilePath != workspace.FilePath {
		t.Fatalf("workspace update did not preserve identity and access: %+v", updated)
	}
}

func TestCreateWorkspaceRejectsUnsafeOrAmbiguousDefinitions(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	projects := []project{
		{ID: "project-a", Name: "A", Path: filepath.Join(directory, "a"), Source: "local", ImportedAt: time.Now().UTC()},
		{ID: "project-b", Name: "B", Path: filepath.Join(directory, "b"), Source: "local", ImportedAt: time.Now().UTC()},
	}
	if err := service.writeProjects(projects); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		folders []workspaceFolderInput
	}{
		{name: "one project", folders: []workspaceFolderInput{{ProjectID: "project-a", Access: workspaceAccessReadWrite, Primary: true}}},
		{name: "duplicate project", folders: []workspaceFolderInput{{ProjectID: "project-a", Access: workspaceAccessReadWrite, Primary: true}, {ProjectID: "project-a", Access: workspaceAccessReadWrite}}},
		{name: "read only primary", folders: []workspaceFolderInput{{ProjectID: "project-a", Access: workspaceAccessReadOnly, Primary: true}, {ProjectID: "project-b", Access: workspaceAccessReadWrite}}},
		{name: "multiple primary", folders: []workspaceFolderInput{{ProjectID: "project-a", Access: workspaceAccessReadWrite, Primary: true}, {ProjectID: "project-b", Access: workspaceAccessReadWrite, Primary: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.createWorkspace(createWorkspaceRequest{Name: "Unsafe workspace", Folders: test.folders}); err == nil {
				t.Fatal("expected invalid workspace definition to be rejected")
			}
		})
	}
}

func TestWorkspaceReadOnlyFoldersAreNotWritableExecutionRoots(t *testing.T) {
	workspace := workspaceDefinition{Folders: []workspaceFolder{
		{ProjectID: "primary", Path: `C:\code\primary`, Access: workspaceAccessReadWrite, Primary: true},
		{ProjectID: "docs", Path: `C:\code\docs`, Access: workspaceAccessReadOnly},
		{ProjectID: "api", Path: `C:\code\api`, Access: workspaceAccessReadWrite},
	}}
	resolved := workspaceProject(workspace)
	additional := resolved.additionalExecutionPaths()
	if len(additional) != 1 || additional[0] != `C:\code\api` {
		t.Fatalf("read-only roots must not be exposed as writable: %v", additional)
	}
}

func TestWorkspacePathsAreResolvedFromImportedProjects(t *testing.T) {
	projects := []project{
		{ID: "primary", Name: "Primary", Path: `C:\code\primary`},
		{ID: "api", Name: "API", Path: `C:\code\api`},
	}
	workspace := workspaceDefinition{Folders: []workspaceFolder{
		{ProjectID: "primary", Name: "Tampered", Path: `C:\outside`, Access: workspaceAccessReadWrite, Primary: true},
		{ProjectID: "api", Name: "Tampered API", Path: `D:\outside`, Access: workspaceAccessReadWrite},
	}}

	normalized, err := normalizeWorkspaceFolders(workspace, projects)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Folders[0].Path != projects[0].Path || normalized.Folders[1].Path != projects[1].Path {
		t.Fatalf("workspace file paths must not override imported project roots: %+v", normalized.Folders)
	}
}
