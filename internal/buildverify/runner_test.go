package buildverify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunnerStreamsOutputAndPersistsLog(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	lines := make([]string, 0)
	run, err := (Runner{DataDirectory: data, Timeout: 30 * time.Second}).Run(context.Background(), root, "project-1", "task-1", Action{ID: "go-version", Label: "Go version", Executable: "go", Arguments: []string{"version"}}, func(_ string, line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" || run.ExitCode != 0 || run.LogPath == "" {
		t.Fatalf("run=%+v", run)
	}
	logData, err := os.ReadFile(run.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || !strings.Contains(string(logData), "go version") {
		t.Fatalf("lines=%v log=%s", lines, logData)
	}
}
