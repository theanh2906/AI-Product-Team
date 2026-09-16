package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadStreamExtractsSessionAndStructuredOutput(t *testing.T) {
	stream := strings.NewReader(strings.Join([]string{
		`{"type":"session","session_id":"session-1"}`,
		`{"type":"assistant","message":"working"}`,
		`{"type":"result","result":"{\"summary\":\"ok\"}"}`,
	}, "\n"))

	result, err := readStream(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "session-1" {
		t.Fatalf("expected session ID, got %q", result.SessionID)
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(extractJSONPayload(result.Result), &payload); err != nil {
		t.Fatalf("expected embedded JSON payload: %v", err)
	}
	if payload.Summary != "ok" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestReadStreamIgnoresAssistantTextAsFinalResult(t *testing.T) {
	stream := strings.NewReader(strings.Join([]string{
		`{"type":"session","session_id":"session-1"}`,
		`{"type":"assistant","message":"**Refining JSON Output** I am preparing the JSON."}`,
		`{"type":"result","result":"{\"summary\":\"verified\",\"bugs\":[]}"}`,
	}, "\n"))

	result, err := readStream(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Result, "Refining JSON Output") {
		t.Fatalf("assistant progress text was treated as final result: %q", result.Result)
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(extractJSONPayload(result.Result), &payload); err != nil {
		t.Fatalf("expected final result JSON payload: %v", err)
	}
	if payload.Summary != "verified" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestReadStreamUsesAssistantJSONWhenNoResultEvent(t *testing.T) {
	stream := strings.NewReader(strings.Join([]string{
		`{"type":"session","session_id":"session-1"}`,
		`{"type":"assistant","message":"{\"summary\":\"verified\",\"bugs\":[]}"}`,
	}, "\n"))

	result, err := readStream(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StructuredOutput) == 0 {
		t.Fatal("expected assistant JSON to be retained as structured output")
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(result.StructuredOutput, &payload); err != nil {
		t.Fatalf("expected structured JSON payload: %v", err)
	}
	if payload.Summary != "verified" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestReadStreamDoesNotUseToolJSONAsAgentResult(t *testing.T) {
	stream := strings.NewReader(strings.Join([]string{
		`{"type":"session","session_id":"session-1"}`,
		`{"type":"tool","message":"{\"summary\":\"tool output, not the answer\"}"}`,
	}, "\n"))

	result, err := readStream(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StructuredOutput) != 0 {
		t.Fatalf("tool JSON was treated as structured output: %s", result.StructuredOutput)
	}
	if strings.TrimSpace(result.Result) != "" {
		t.Fatalf("tool JSON was treated as result: %q", result.Result)
	}
}

func TestExtractJSONPayloadFromFencedResponse(t *testing.T) {
	raw := "Here is the result:\n```json\n{\"summary\":\"done\",\"changedFiles\":[]}\n```"
	payload := extractJSONPayload(raw)
	if !json.Valid(payload) {
		t.Fatalf("expected valid JSON payload, got %q", payload)
	}
	if string(payload) != `{"summary":"done","changedFiles":[]}` {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestFinalizeStructuredOutputRejectsMarkdownWhenSchemaIsRequired(t *testing.T) {
	result, err := finalizeStructuredOutput(Result{SessionID: "session-1", Result: "* QA found a bug"}, `{"type":"object"}`)
	if err == nil {
		t.Fatal("expected non-JSON output to fail")
	}
	if result.SessionID != "session-1" {
		t.Fatalf("expected session ID to be preserved, got %q", result.SessionID)
	}
	if !strings.Contains(err.Error(), "did not return a JSON object") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFinalizeStructuredOutputExplainsEmptyCopilotResponse(t *testing.T) {
	_, err := finalizeStructuredOutput(Result{SessionID: "session-1"}, `{"type":"object"}`)
	if err == nil {
		t.Fatal("expected empty output to fail")
	}
	if !strings.Contains(err.Error(), "No final or JSON-like response content was emitted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFinalizeStructuredOutputExtractsJSONWhenSchemaIsRequired(t *testing.T) {
	result, err := finalizeStructuredOutput(Result{Result: "```json\n{\"summary\":\"ok\"}\n```"}, `{"type":"object"}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.StructuredOutput) != `{"summary":"ok"}` {
		t.Fatalf("unexpected structured output: %s", result.StructuredOutput)
	}
}

func TestRepairJSONPromptIsStrictAndDoesNotUseScratchNotes(t *testing.T) {
	prompt, err := repairJSONPrompt("Verify the assigned QA task.", "* QA found a bug", `{"type":"object"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Return exactly one JSON object and nothing else.") {
		t.Fatalf("repair prompt is not strict enough: %s", prompt)
	}
	if !strings.Contains(prompt, "Start with { and end with }.") {
		t.Fatalf("repair prompt does not force JSON boundaries: %s", prompt)
	}
	if !strings.Contains(prompt, "Task:\nVerify the assigned QA task.") {
		t.Fatalf("repair prompt does not repeat the original task: %s", prompt)
	}
	if strings.Contains(strings.ToLower(prompt), "need inspect") {
		t.Fatalf("repair prompt includes scratch-note phrasing: %s", prompt)
	}
}

func TestComposePromptKeepsInstructionsSeparateFromTask(t *testing.T) {
	prompt, err := composePrompt("Inspect settings save.", RunConfig{
		SystemPrompt: "Use repo evidence only.",
		Schema:       `{"type":"object","properties":{"summary":{"type":"string"}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "System instructions:") || !strings.Contains(prompt, "Response contract:") || !strings.Contains(prompt, "Task:") {
		t.Fatalf("prompt did not include expected sections: %s", prompt)
	}
	if !strings.Contains(prompt, "The response must start with { and end with }.") {
		t.Fatalf("prompt does not force JSON boundaries: %s", prompt)
	}
	if strings.Contains(strings.ToLower(prompt), "need inspect") {
		t.Fatalf("prompt includes scratch-note phrasing: %s", prompt)
	}
}

func TestBuildRunArgsOmitsEffortForAutoModel(t *testing.T) {
	args, sessionID, err := buildRunArgs("Return JSON.", RunConfig{
		CWD:      t.TempDir(),
		Model:    "auto",
		Effort:   "high",
		Writable: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID == "" {
		t.Fatal("expected a new Copilot session ID")
	}
	for index, arg := range args {
		if arg == "--effort" || arg == "--reasoning-effort" {
			t.Fatalf("auto model must not receive a reasoning effort flag at index %d: %v", index, args)
		}
	}
	if !containsArgPair(args, "--model", "auto") {
		t.Fatalf("expected auto model arg: %v", args)
	}
}

func TestBuildRunArgsKeepsEffortForSpecificModel(t *testing.T) {
	args, _, err := buildRunArgs("Return JSON.", RunConfig{
		CWD:      t.TempDir(),
		Model:    "gpt-5.6-sol",
		Effort:   "max",
		Writable: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(args, "--model", "gpt-5.6-sol") {
		t.Fatalf("expected configured model arg: %v", args)
	}
	if !containsArgPair(args, "--effort", "max") {
		t.Fatalf("expected configured effort arg for specific model: %v", args)
	}
}

func TestBuildRunArgsScopesWritableWorkspaceDirectories(t *testing.T) {
	primary := t.TempDir()
	additional := t.TempDir()
	args, _, err := buildRunArgs("Return JSON.", RunConfig{
		CWD:            primary,
		AdditionalDirs: []string{additional},
		Model:          "auto",
		Writable:       true,
		RestrictPaths:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(args, "--add-dir", additional) {
		t.Fatalf("expected additional workspace root: %v", args)
	}
	for _, arg := range args {
		if arg == "--allow-all" || arg == "--allow-all-paths" {
			t.Fatalf("restricted workspace must not grant broad filesystem access: %v", args)
		}
	}
	if !containsArg(args, "--allow-all-tools") {
		t.Fatalf("expected headless tool approval for workspace execution: %v", args)
	}
}

func TestIsResumeFailureTreatsLegacyAttachmentPromptAsFreshSessionCandidate(t *testing.T) {
	err := fmt.Errorf("GitHub Copilot failed: --attachment file type not supported (must be an image or native document): C:\\Users\\anh.tang\\.productcrew\\copilot-prompts\\copilot-prompt-1872610941.md")

	if !isResumeFailure(err) {
		t.Fatal("expected legacy Copilot attachment transport failure to start a fresh session")
	}
}

func TestPreparePromptTransportUsesPromptFileForLongPrompt(t *testing.T) {
	directory := filepath.Join(t.TempDir(), ".productcrew", "copilot-prompts")
	longPrompt := strings.Repeat("ProductCrew prompt ", maxInlinePromptLength/len("ProductCrew prompt ")+20)

	transport, err := preparePromptTransport(context.Background(), longPrompt, RunConfig{PromptDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if transport.prompt == longPrompt {
		t.Fatal("expected long prompt to be replaced with a short file-read instruction")
	}
	if !strings.Contains(transport.prompt, directory) {
		t.Fatalf("expected short prompt to reference the ProductCrew prompt directory, got %q", transport.prompt)
	}
	if len(transport.args) != 2 || transport.args[0] != "--add-dir" || transport.args[1] != directory {
		t.Fatalf("expected prompt directory access args, got %v", transport.args)
	}
	matches, err := filepath.Glob(filepath.Join(directory, "copilot-prompt-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one prompt file, got %v", matches)
	}
	promptPath := matches[0]
	if !strings.Contains(transport.prompt, promptPath) {
		t.Fatalf("expected short prompt to reference prompt file, got %q", transport.prompt)
	}
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != longPrompt {
		t.Fatal("prompt file did not preserve the full prompt")
	}
	transport.cleanup()
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove prompt file, got %v", err)
	}
}

func TestPreparePromptTransportKeepsShortPromptInline(t *testing.T) {
	transport, err := preparePromptTransport(context.Background(), "Return JSON.", RunConfig{PromptDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if transport.prompt != "Return JSON." {
		t.Fatalf("short prompt should stay inline, got %q", transport.prompt)
	}
	if len(transport.args) != 0 {
		t.Fatalf("short prompt should not add args, got %v", transport.args)
	}
}

func TestPreparePromptTransportCleansStalePromptFiles(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, "copilot-prompt-stale.md")
	fresh := filepath.Join(directory, "copilot-prompt-fresh.md")
	other := filepath.Join(directory, "notes.md")
	for _, path := range []string{stale, fresh, other} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-stalePromptFileTTL - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	transport, err := preparePromptTransport(context.Background(), strings.Repeat("x", maxInlinePromptLength+1), RunConfig{PromptDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.cleanup()

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale prompt file to be removed, got %v", err)
	}
	for _, path := range []string{fresh, other} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}

func containsArgPair(args []string, key, value string) bool {
	for index := 0; index < len(args)-1; index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
