package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
	"github.com/theanh2906/AI-Product-Team/internal/storage"
)

func newTestInsightService(t *testing.T) *insights.Service {
	t.Helper()
	repository, err := storage.NewJSONInsightRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return insights.NewService(repository)
}

func TestSortSourceScanFindingsBySeverity(t *testing.T) {
	findings := []sourceScanFinding{
		{ID: "low", Severity: "low"},
		{ID: "critical", Severity: "critical"},
		{ID: "medium", Severity: "medium"},
		{ID: "high", Severity: "high"},
	}

	sortSourceScanFindings(findings)

	for index, expected := range []string{"critical", "high", "medium", "low"} {
		if findings[index].Severity != expected {
			t.Fatalf("expected severity %q at index %d, got %q", expected, index, findings[index].Severity)
		}
	}
}

func TestSourceScanManagerRunsJobIndependently(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := newSourceScanManager(func(_ context.Context, project project, _ sourceScanRequest, scanID string) (sourceScanResult, error) {
		close(started)
		<-release
		return sourceScanResult{ScanID: scanID, ProjectID: project.ID}, nil
	}, newTestInsightService(t))

	job := manager.start(project{ID: "project-1", Name: "sample"}, sourceScanRequest{})
	<-started
	updates, unsubscribe, exists := manager.subscribe(job.ScanID)
	if !exists {
		t.Fatal("expected background job subscription")
	}
	defer unsubscribe()
	if initial := <-updates; initial.Status != "running" {
		t.Fatalf("expected initial running event, got %+v", initial)
	}
	running, exists := manager.get(job.ScanID)
	if !exists || running.Status != "running" {
		t.Fatalf("expected running background job, got %+v", running)
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case completed := <-updates:
			if completed.Status != "completed" {
				continue
			}
			if completed.Result == nil || completed.Result.ScanID != job.ScanID {
				t.Fatalf("unexpected completed result: %+v", completed.Result)
			}
			return
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background scan did not complete")
}

func TestSourceScanContinuesAfterSubscriberDisconnects(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := newSourceScanManager(func(_ context.Context, project project, _ sourceScanRequest, scanID string) (sourceScanResult, error) {
		close(started)
		<-release
		return sourceScanResult{ScanID: scanID, ProjectID: project.ID}, nil
	}, newTestInsightService(t))

	job := manager.start(project{ID: "project-1", Name: "sample"}, sourceScanRequest{})
	<-started
	_, unsubscribe, exists := manager.subscribe(job.ScanID)
	if !exists {
		t.Fatal("expected background job subscription")
	}
	unsubscribe()
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		latest, exists, err := manager.latest("project-1")
		if err != nil {
			t.Fatal(err)
		}
		if exists && latest.ScanID == job.ScanID && latest.Status == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background scan stopped after its subscriber disconnected")
}

func TestLatestSourceScanReturnsNewestJob(t *testing.T) {
	manager := newSourceScanManager(func(_ context.Context, project project, _ sourceScanRequest, scanID string) (sourceScanResult, error) {
		return sourceScanResult{ScanID: scanID, ProjectID: project.ID}, nil
	}, newTestInsightService(t))
	first := manager.start(project{ID: "project-1", Name: "first"}, sourceScanRequest{})
	time.Sleep(time.Millisecond)
	second := manager.start(project{ID: "project-2", Name: "second"}, sourceScanRequest{})

	latest, exists, err := manager.latest("")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected latest source scan")
	}
	if latest.ScanID != second.ScanID || latest.ScanID == first.ScanID {
		t.Fatalf("expected newest scan %q, got %+v", second.ScanID, latest)
	}
}

func TestReadSourceExcerptIncludesContextAndHighlightsTarget(t *testing.T) {
	projectPath := t.TempDir()
	filePath := filepath.Join(projectPath, "src", "example.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "line one\nline two\nline three\nline four\nline five\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	lines := readSourceExcerpt(projectPath, "src/example.go", 3, 1)
	if len(lines) != 3 {
		t.Fatalf("expected 3 source lines, got %+v", lines)
	}
	if lines[0].Number != 2 || lines[1].Number != 3 || lines[2].Number != 4 {
		t.Fatalf("unexpected source line numbers: %+v", lines)
	}
	if !lines[1].Highlighted || lines[0].Highlighted || lines[2].Highlighted {
		t.Fatalf("expected only target line to be highlighted: %+v", lines)
	}
}

func TestReadSourceExcerptRejectsPathOutsideProject(t *testing.T) {
	projectPath := t.TempDir()
	outsidePath := filepath.Join(filepath.Dir(projectPath), "outside-source.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outsidePath) })

	if lines := readSourceExcerpt(projectPath, "../outside-source.txt", 1, 1); len(lines) != 0 {
		t.Fatalf("expected path traversal to return no source lines, got %+v", lines)
	}
}
