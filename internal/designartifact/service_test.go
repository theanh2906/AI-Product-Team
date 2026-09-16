package designartifact

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProcessRendersTaskScopedMockupAndWritesManifest(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "overview.html"), []byte("<html><body>Mockup</body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "fake-edge" }
	service.runRenderer = func(_ context.Context, _, _, output string) error {
		file, err := os.Create(output)
		if err != nil {
			return err
		}
		defer file.Close()
		mockup := image.NewRGBA(image.Rect(0, 0, 1440, 900))
		mockup.Set(0, 0, color.RGBA{R: 12, G: 34, B: 56, A: 255})
		return png.Encode(file, mockup)
	}

	artifacts, warnings, err := service.Process(context.Background(), project, "task-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(artifacts) != 1 {
		t.Fatalf("unexpected result: artifacts=%+v warnings=%+v", artifacts, warnings)
	}
	if artifacts[0].Width != 1440 || artifacts[0].Height != 900 || artifacts[0].MediaType != "image/png" {
		t.Fatalf("unexpected artifact: %+v", artifacts[0])
	}
	if _, err := os.Stat(filepath.Join(directory, "manifest.json")); err != nil {
		t.Fatalf("expected manifest: %v", err)
	}
}

func TestProcessAllowsInlineScriptForInteractiveMockups(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-interactive")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `<html><body><button id="menu">Toggle</button><script>document.getElementById("menu").dataset.open = "true";</script></body></html>`
	if err := os.WriteFile(filepath.Join(directory, "overview.html"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }

	if _, _, err := service.Process(context.Background(), project, "task-interactive"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessAllowsExternalURLTextInMockupSource(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-form-url")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `<html><body><label>Server URL<input value="https://teamcity.example.com"></label></body></html>`
	if err := os.WriteFile(filepath.Join(directory, "empty-state.html"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }

	if _, _, err := service.Process(context.Background(), project, "task-form-url"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessAllowsScriptBearingMockupSource(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-network-call")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `<html><body><script>fetch("https://example.test/data")</script></body></html>`
	if err := os.WriteFile(filepath.Join(directory, "unsafe.html"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }

	if _, _, err := service.Process(context.Background(), project, "task-network-call"); err != nil {
		t.Fatal(err)
	}
}

func TestEdgeRendererSmoke(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Edge renderer is Windows-specific")
	}
	renderer := findEdge()
	if renderer == "" {
		t.Skip("Microsoft Edge is not installed")
	}
	directory := t.TempDir()
	source := filepath.Join(directory, "preview.html")
	output := filepath.Join(directory, "preview.png")
	if err := os.WriteFile(source, []byte(`<html><body style="margin:0;background:#eefaf7"><h1>ProductCrew mockup</h1></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	if err := service.renderWithEdge(context.Background(), renderer, source, output); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	config, err := png.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != defaultWidth || config.Height != defaultHeight {
		t.Fatalf("unexpected screenshot dimensions: %dx%d", config.Width, config.Height)
	}
}

func TestProcessKeepsTextHandoffWhenRendererIsUnavailable(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-456")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "overview.svg"), []byte("<svg></svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }

	artifacts, warnings, err := service.Process(context.Background(), project, "task-456")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 || len(warnings) != 1 {
		t.Fatalf("unexpected fallback: artifacts=%+v warnings=%+v", artifacts, warnings)
	}
}

func TestProcessRemovesStalePNGWhenRendererIsUnavailable(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "overview.html"), []byte("<html><body>Updated mockup</body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "overview.png")
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	mockup := image.NewRGBA(image.Rect(0, 0, 1440, 900))
	if err := png.Encode(file, mockup); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }

	artifacts, warnings, err := service.Process(context.Background(), project, "task-stale")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 || len(warnings) != 1 {
		t.Fatalf("unexpected stale render fallback: artifacts=%+v warnings=%+v", artifacts, warnings)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("stale PNG should be removed when it cannot be refreshed: %v", err)
	}
}

func TestTaskDirectoryRejectsTraversal(t *testing.T) {
	if _, err := TaskDirectory(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected traversal task id to be rejected")
	}
}

func TestProcessAllowsExternalScriptSourceWhenRendererIsUnavailable(t *testing.T) {
	project := t.TempDir()
	directory, err := TaskDirectory(project, "task-unsafe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unsafe.html"), []byte(`<script src="https://example.test/mockup.js"></script>`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService()
	service.findRenderer = func() string { return "" }
	if _, _, err := service.Process(context.Background(), project, "task-unsafe"); err != nil {
		t.Fatal(err)
	}
}
