package projectartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesystemStorePersistsRequestAndAttachment(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewFilesystemStore()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	manifest, err := store.SaveRequest(context.Background(), project, RequestInput{
		RequestID: "request-123", ProjectID: "project-1", Title: "Saved filters",
		Description: "Persist the selected filters between sessions.", Source: "manual",
		CreatedAt:   time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC),
		Attachments: []Upload{{Name: "reference.png", MediaType: "image/png", Size: int64(len(png)), Open: readerUpload(png)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Directory != ".productcrew/requests/request-123" || len(manifest.Attachments) != 1 {
		t.Fatalf("unexpected request manifest: %+v", manifest)
	}
	loaded, err := store.LoadRequest(context.Background(), project, "request-123")
	if err != nil || loaded.Attachments[0].OriginalName != "reference.png" {
		t.Fatalf("load request = %+v, %v", loaded, err)
	}
	markdown, err := os.ReadFile(filepath.Join(project, ".productcrew", "requests", "request-123", "request.md"))
	if err != nil || !strings.Contains(string(markdown), "Persist the selected filters") {
		t.Fatalf("request markdown = %q, %v", markdown, err)
	}
	attachment, file, err := store.OpenAttachment(context.Background(), project, "request-123", "attachment-01")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if attachment.SHA256 == "" || attachment.RelativePath != ".productcrew/requests/request-123/attachments/attachment-01.png" {
		t.Fatalf("unexpected attachment metadata: %+v", attachment)
	}
	exclude, err := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), "/.productcrew/") {
		t.Fatalf("local git exclude = %q, %v", exclude, err)
	}
}

func TestFilesystemStoreRejectsUnsafeAttachmentAndID(t *testing.T) {
	store := NewFilesystemStore()
	project := t.TempDir()
	if _, err := store.SaveRequest(context.Background(), project, RequestInput{
		RequestID: "../escape", Title: "Unsafe request", Description: "This request must remain inside the project.",
	}); err == nil {
		t.Fatal("expected unsafe request id to be rejected")
	}
	if _, err := store.SaveRequest(context.Background(), project, RequestInput{
		RequestID: "request-safe", Title: "Unsafe attachment", Description: "Executable attachments must not be accepted.",
		Attachments: []Upload{{Name: "payload.exe", Size: 2, Open: readerUpload([]byte("MZ"))}},
	}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported attachment rejection, got %v", err)
	}
}

func TestFilesystemStorePersistsInsightHistoryAndTaskReport(t *testing.T) {
	store := NewFilesystemStore()
	project := t.TempDir()
	manifest, err := store.SaveInsight(context.Background(), project, "bug-scan", "SCAN-123", map[string]any{"scanId": "SCAN-123", "findings": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ResultPath != ".productcrew/insights/bug-scan/SCAN-123/result.json" {
		t.Fatalf("unexpected insight manifest: %+v", manifest)
	}
	latest, err := os.ReadFile(filepath.Join(project, ".productcrew", "insights", "bug-scan", "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded InsightManifest
	if json.Unmarshal(latest, &decoded) != nil || decoded.RunID != "SCAN-123" {
		t.Fatalf("latest insight pointer = %s", latest)
	}
	path, err := store.SaveTaskReport(context.Background(), project, TaskReportInput{
		TaskID: "task-123", Key: "DEV-001", Role: "developer", Title: "Implement history",
		Status: "completed", Summary: "Implemented project-local history.", Payload: map[string]any{"summary": "done"},
	})
	if err != nil || path != ".productcrew/tasks/task-123" {
		t.Fatalf("task report path = %q, %v", path, err)
	}
}

func readerUpload(data []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
}
