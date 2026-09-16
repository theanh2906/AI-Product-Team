package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/projectartifact"
)

func TestBuildProductBlueprintValidatesDestinationAndPersistsDraft(t *testing.T) {
	data := t.TempDir()
	projects, err := newProjectService(data, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{projectService: projects}
	parent := t.TempDir()
	blueprint, err := s.buildProductBlueprint(productBlueprintRequest{
		ProductName: "fresh-product", Idea: "Build a local product that helps teams turn briefs into verified delivery work.",
		DestinationParent: parent, ProfileID: "angular-go", StartingPoint: "blank",
	})
	if err != nil {
		t.Fatalf("buildProductBlueprint() error = %v", err)
	}
	if blueprint.Destination != filepath.Join(parent, "fresh-product") {
		t.Fatalf("Destination = %q", blueprint.Destination)
	}
	if err := s.saveProductBlueprint(blueprint); err != nil {
		t.Fatalf("saveProductBlueprint() error = %v", err)
	}
	loaded, err := s.loadProductBlueprint(blueprint.ID)
	if err != nil {
		t.Fatalf("loadProductBlueprint() error = %v", err)
	}
	if loaded.ProductName != blueprint.ProductName {
		t.Fatalf("ProductName = %q", loaded.ProductName)
	}
}

func TestWriteProductFoundationCreatesBuildableShape(t *testing.T) {
	root := t.TempDir()
	blueprint := productBlueprint{ProductName: "fresh-product", Idea: "A focused local product.", ProfileID: "angular-go", Criteria: []string{"Works"}}
	if err := writeProductFoundation(root, blueprint); err != nil {
		t.Fatalf("writeProductFoundation() error = %v", err)
	}
	for _, relative := range []string{"README.md", "go.mod", "cmd/server/main.go", "frontend/angular.json", "frontend/src/main.ts", ".productcrew/blueprint.json"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("missing %s: %v", relative, err)
		}
	}
}

func TestBuildProductBlueprintRejectsExistingDestination(t *testing.T) {
	data := t.TempDir()
	projects, err := newProjectService(data, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{projectService: projects}
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "existing-product"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = s.buildProductBlueprint(productBlueprintRequest{ProductName: "existing-product", Idea: "Build a local product with a safe outcome and verified workflow.", DestinationParent: parent, ProfileID: "angular-go"})
	if err == nil {
		t.Fatal("expected destination collision error")
	}
}

func TestScaffoldProductPublishesRegistersAndCreatesBacklog(t *testing.T) {
	data := t.TempDir()
	projects, err := newProjectService(data, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	s := &server{
		projectService:   projects,
		boards:           newBoardService(projects),
		boardEvents:      newBoardEventHub(),
		projectArtifacts: projectartifact.NewFilesystemStore(),
	}
	blueprint := productBlueprint{
		SchemaVersion: productCreationSchemaVersion,
		ID:            "blueprint-test",
		ProductName:   "fresh-product",
		Idea:          "Build a local product that turns a brief into a safe, reviewable delivery plan.",
		Destination:   filepath.Join(parent, "fresh-product"),
		ProfileID:     "angular-go",
		ProfileName:   "Angular + Go",
		Criteria:      []string{"The first workflow works end to end"},
		Preflight:     productCreationPreflight{Ready: true},
	}
	result, err := s.scaffoldProduct(context.Background(), &blueprint)
	if err != nil {
		t.Fatalf("scaffoldProduct() error = %v", err)
	}
	if result.Project.Path != blueprint.Destination {
		t.Fatalf("Project.Path = %q", result.Project.Path)
	}
	if result.BacklogItemID == "" {
		t.Fatal("BacklogItemID is empty")
	}
	if _, err := os.Stat(filepath.Join(blueprint.Destination, "frontend", "angular.json")); err != nil {
		t.Fatalf("published scaffold missing: %v", err)
	}
	if _, err := s.boards.GetProjectBoard(context.Background(), result.Project.ID); err != nil {
		t.Fatalf("GetProjectBoard() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, ".fresh-product.productcrew-staging")); !os.IsNotExist(err) {
		t.Fatalf("staging folder remains after publish: %v", err)
	}
}
