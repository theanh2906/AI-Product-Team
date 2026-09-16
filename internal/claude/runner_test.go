package claude

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
)

type captureReporter struct {
	mu     sync.Mutex
	events []agentstream.Event
}

func (r *captureReporter) Report(event agentstream.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func TestNormalizeModelDefaultsToSonnet5(t *testing.T) {
	if got := normalizeModel(""); got != DefaultModel {
		t.Fatalf("expected default model %q, got %q", DefaultModel, got)
	}
	if got := normalizeModel(" custom-model "); got != "custom-model" {
		t.Fatalf("expected trimmed custom model, got %q", got)
	}
}

func TestNormalizeEffortDefaultsToHigh(t *testing.T) {
	if got := normalizeEffort(""); got != DefaultEffort {
		t.Fatalf("expected default effort %q, got %q", DefaultEffort, got)
	}
	if got := normalizeEffort(" HIGH "); got != "high" {
		t.Fatalf("expected normalized high effort, got %q", got)
	}
	if got := normalizeEffort("turbo"); got != DefaultEffort {
		t.Fatalf("expected invalid effort to fall back to %q, got %q", DefaultEffort, got)
	}
}

func TestIsResumeFailureRecognizesMissingConversation(t *testing.T) {
	err := fmt.Errorf("Claude Code failed: No conversation found with session ID: codex-thread")
	if !isResumeFailure(err) {
		t.Fatal("missing Claude conversation must start a fresh session")
	}
}

func TestReadClaudeStreamReturnsResultAndReportsToolActivity(t *testing.T) {
	reporter := &captureReporter{}
	ctx := agentstream.WithReporter(context.Background(), reporter)
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"session-1"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"main.go"}}]}}`,
		`{"type":"result","subtype":"success","session_id":"session-1","result":"done","structured_output":{"summary":"ok"}}`,
	}, "\n")
	result, err := readClaudeStream(ctx, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "session-1" || string(result.StructuredOutput) != `{"summary":"ok"}` {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(reporter.events) < 3 || reporter.events[1].Message != "Using Read" {
		t.Fatalf("expected streamed tool activity, got %+v", reporter.events)
	}
}
