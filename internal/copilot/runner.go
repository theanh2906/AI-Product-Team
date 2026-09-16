package copilot

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

type RunConfig struct {
	CWD             string
	AdditionalDirs  []string
	SystemPrompt    string
	Model           string
	Effort          string
	Schema          any
	SessionID       string
	Writable        bool
	RestrictPaths   bool
	Timeout         time.Duration
	PromptDirectory string
}

type Result struct {
	SessionID        string
	Result           string
	StructuredOutput json.RawMessage
}

const (
	DefaultModel          = "auto"
	DefaultEffort         = "high"
	maxInlinePromptLength = 20000
	stalePromptFileTTL    = 24 * time.Hour
)

func RunJSON(ctx context.Context, prompt string, config RunConfig) (Result, error) {
	if strings.TrimSpace(config.CWD) == "" {
		return Result{}, fmt.Errorf("GitHub Copilot working directory is required")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runJSONOnce(runContext, prompt, config)
	if err == nil {
		return retryIfMissingStructuredJSON(runContext, prompt, config, result)
	}
	if strings.TrimSpace(config.SessionID) != "" && isResumeFailure(err) {
		agentstream.Emit(ctx, agentstream.Event{Level: "warning", Kind: "thread", Message: "GitHub Copilot session could not resume; starting a new session"})
		config.SessionID = ""
		result, err = runJSONOnce(runContext, prompt, config)
		if err == nil {
			return retryIfMissingStructuredJSON(runContext, prompt, config, result)
		}
	}
	return Result{}, err
}

func retryIfMissingStructuredJSON(ctx context.Context, prompt string, config RunConfig, result Result) (Result, error) {
	ready, err := finalizeStructuredOutput(result, config.Schema)
	if err == nil {
		return ready, nil
	}
	agentstream.Emit(ctx, agentstream.Event{Level: "warning", Kind: "runtime", Message: "GitHub Copilot returned non-JSON output; requesting structured retry", Detail: truncate(result.Result, 1200)})
	retryPrompt, promptErr := repairJSONPrompt(prompt, result.Result, config.Schema)
	if promptErr != nil {
		return Result{SessionID: result.SessionID}, promptErr
	}
	retryConfig := config
	retryConfig.SessionID = firstNonEmpty(result.SessionID, config.SessionID)
	retry, retryErr := runJSONOnce(ctx, retryPrompt, retryConfig)
	if retryErr != nil {
		return Result{SessionID: retryConfig.SessionID}, fmt.Errorf("%w; structured retry failed: %v", err, retryErr)
	}
	return finalizeStructuredOutput(retry, config.Schema)
}

func runJSONOnce(ctx context.Context, prompt string, config RunConfig) (Result, error) {
	copilotPrompt, err := composePrompt(prompt, config)
	if err != nil {
		return Result{}, err
	}
	transport, err := preparePromptTransport(ctx, copilotPrompt, config)
	if err != nil {
		return Result{}, err
	}
	defer transport.cleanup()
	args, sessionID, err := buildRunArgs(transport.prompt, config)
	if err != nil {
		return Result{}, err
	}
	args = append(args, transport.args...)
	config.SessionID = sessionID

	command := exec.CommandContext(ctx, "gh", args...)
	command.Dir = config.CWD
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open GitHub Copilot stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open GitHub Copilot stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("start GitHub Copilot: %w", err)
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "turn", Message: "GitHub Copilot session started"})
	stderrDone := make(chan string, 1)
	go collectStderr(ctx, stderr, stderrDone)

	response, scanErr := readStream(ctx, stdout)
	waitErr := command.Wait()
	stderrText := <-stderrDone
	if scanErr != nil {
		return Result{}, scanErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderrText)
		if message == "" {
			message = strings.TrimSpace(response.Result)
		}
		if message == "" {
			message = waitErr.Error()
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("GitHub Copilot failed: %s", truncate(message, 2000))
	}
	if len(response.StructuredOutput) == 0 {
		response.StructuredOutput = extractJSONPayload(response.Result)
	}
	if strings.TrimSpace(response.SessionID) == "" {
		response.SessionID = strings.TrimSpace(config.SessionID)
	}
	if strings.TrimSpace(response.SessionID) == "" && strings.TrimSpace(response.Result) == "" && len(response.StructuredOutput) == 0 {
		return Result{}, fmt.Errorf("GitHub Copilot stream ended without a result event")
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "turn", Message: "GitHub Copilot turn completed"})
	return response, nil
}

func buildRunArgs(copilotPrompt string, config RunConfig) ([]string, string, error) {
	model := normalizeModel(config.Model)
	args := []string{"copilot", "--",
		"-p", copilotPrompt,
		"--output-format", "json",
		"--stream", "on",
		"--no-remote",
		"--no-remote-export",
		"-C", config.CWD,
		"--model", model,
	}
	for _, directory := range config.AdditionalDirs {
		if strings.TrimSpace(directory) != "" {
			args = append(args, "--add-dir", directory)
		}
	}
	if supportsReasoningEffort(model) {
		args = append(args, "--effort", normalizeEffort(config.Effort))
	}
	sessionID := strings.TrimSpace(config.SessionID)
	if sessionID != "" {
		args = append(args, "--resume="+sessionID)
	} else {
		newID, err := newSessionID()
		if err != nil {
			return nil, "", err
		}
		sessionID = newID
		args = append(args, "--session-id="+newID)
	}
	if config.Writable && !config.RestrictPaths {
		args = append(args, "--allow-all")
	} else if config.Writable {
		args = append(args,
			"--allow-all-tools",
			"--allow-all-urls",
			"--deny-tool=shell(git push)",
		)
	} else if config.RestrictPaths {
		args = append(args,
			"--allow-all-tools",
			"--deny-tool=shell(git push)",
			"--deny-tool=write",
		)
	} else {
		args = append(args,
			"--allow-all-tools",
			"--allow-all-paths",
			"--deny-tool=shell(git push)",
			"--deny-tool=write",
		)
	}
	return args, sessionID, nil
}

type promptTransport struct {
	prompt  string
	args    []string
	cleanup func()
}

func preparePromptTransport(ctx context.Context, copilotPrompt string, config RunConfig) (promptTransport, error) {
	if len(copilotPrompt) <= maxInlinePromptLength {
		return promptTransport{prompt: copilotPrompt, cleanup: func() {}}, nil
	}
	directory, err := promptDirectory(config.PromptDirectory)
	if err != nil {
		return promptTransport{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return promptTransport{}, fmt.Errorf("create GitHub Copilot prompt directory: %w", err)
	}
	cleanStalePromptFiles(directory, stalePromptFileTTL)
	file, err := os.CreateTemp(directory, "copilot-prompt-*.md")
	if err != nil {
		return promptTransport{}, fmt.Errorf("create GitHub Copilot prompt file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if _, err := file.WriteString(copilotPrompt); err != nil {
		_ = file.Close()
		cleanup()
		return promptTransport{}, fmt.Errorf("write GitHub Copilot prompt file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return promptTransport{}, fmt.Errorf("close GitHub Copilot prompt file: %w", err)
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "runtime", Message: "GitHub Copilot prompt file staged in ProductCrew storage"})
	return promptTransport{
		prompt:  "Read the ProductCrew prompt file at this absolute path and complete the task exactly as specified there:\n" + path + "\n\nReturn only the required JSON object.",
		args:    []string{"--add-dir", directory},
		cleanup: cleanup,
	}, nil
}

func promptDirectory(configured string) (string, error) {
	if directory := strings.TrimSpace(configured); directory != "" {
		return directory, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve ProductCrew prompt directory: %w", err)
	}
	return filepath.Join(home, ".productcrew", "copilot-prompts"), nil
}

func cleanStalePromptFiles(directory string, ttl time.Duration) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "copilot-prompt-") || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(directory, entry.Name()))
	}
}

func supportsReasoningEffort(model string) bool {
	return !strings.EqualFold(strings.TrimSpace(model), "auto")
}

func composePrompt(prompt string, config RunConfig) (string, error) {
	sections := make([]string, 0, 3)
	if instructions := strings.TrimSpace(config.SystemPrompt); instructions != "" {
		sections = append(sections, "System instructions:\n"+instructions)
	}
	if config.Schema != nil {
		schema, err := schemaJSON(config.Schema)
		if err != nil {
			return "", err
		}
		sections = append(sections, "Response contract:\nReturn one JSON object only. Do not include Markdown, headings, bullets, analysis, apologies, or explanations. The response must start with { and end with }. It must match this JSON Schema:\n"+string(schema))
	}
	sections = append(sections, "Task:\n"+strings.TrimSpace(prompt))
	return strings.Join(sections, "\n\n"), nil
}

func repairJSONPrompt(task string, previous string, schema any) (string, error) {
	schemaData, err := schemaJSON(schema)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`Your previous response was not valid JSON for ProductCrew.

Repeat the task below and return the answer in the required JSON shape.
Return exactly one JSON object and nothing else.
Do not include Markdown, headings, bullets, analysis, apologies, or explanations.
Start with { and end with }.
The JSON object must match this JSON Schema:
%s

Task:
%s

Previous response excerpt:
%s`, string(schemaData), strings.TrimSpace(task), truncate(previous, 2000)), nil
}

func finalizeStructuredOutput(result Result, schema any) (Result, error) {
	if schema == nil {
		return result, nil
	}
	if len(result.StructuredOutput) > 0 {
		if json.Valid(result.StructuredOutput) {
			return result, nil
		}
		return Result{SessionID: result.SessionID}, fmt.Errorf("GitHub Copilot returned invalid structured JSON")
	}
	if payload := extractJSONPayload(result.Result); len(payload) > 0 {
		result.StructuredOutput = payload
		return result, nil
	}
	if strings.TrimSpace(result.Result) == "" {
		return Result{SessionID: result.SessionID}, fmt.Errorf("GitHub Copilot did not return a JSON object required by ProductCrew. No final or JSON-like response content was emitted.")
	}
	return Result{SessionID: result.SessionID}, fmt.Errorf("GitHub Copilot did not return a JSON object required by ProductCrew. Response began with: %s", truncate(result.Result, 240))
}

func readStream(ctx context.Context, reader io.Reader) (Result, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var response Result
	var jsonCandidate json.RawMessage
	var jsonCandidateText string
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		recordCopilotUsage(ctx, event)
		emitEvent(ctx, event)
		eventType := strings.ToLower(stringValue(event, "type", "event", "kind"))
		if sessionID := sessionIDFromEvent(event, eventType); sessionID != "" {
			response.SessionID = sessionID
		}
		text := textValue(event)
		if text != "" && canUseTextAsJSONCandidate(eventType) {
			if payload := extractJSONPayload(text); len(payload) > 0 {
				jsonCandidate = payload
				jsonCandidateText = text
			}
		}
		if isFinalEvent(eventType) {
			if output := structuredValue(event); len(output) > 0 {
				response.StructuredOutput = output
			}
			if text != "" {
				response.Result = text
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("read GitHub Copilot stream: %w", err)
	}
	if len(response.StructuredOutput) == 0 && len(jsonCandidate) > 0 {
		response.StructuredOutput = jsonCandidate
		if strings.TrimSpace(response.Result) == "" || len(extractJSONPayload(response.Result)) == 0 {
			response.Result = jsonCandidateText
		}
	}
	return response, nil
}

func recordCopilotUsage(ctx context.Context, event map[string]any) {
	eventType := strings.ToLower(stringValue(event, "type", "event", "kind"))
	if eventType != "assistant.usage" && !strings.Contains(eventType, "assistant.usage") {
		return
	}
	data, _ := event["data"].(map[string]any)
	if data == nil {
		data = event
	}
	input := numericInt64(data["inputTokens"])
	output := numericInt64(data["outputTokens"])
	cached := numericInt64(data["cacheReadTokens"])
	cacheWrite := numericInt64(data["cacheWriteTokens"])
	reasoning := numericInt64(data["reasoningTokens"])
	total := input + output
	if total == 0 {
		total = numericInt64(data["totalTokens"])
	}
	agentusage.Record(ctx, agentusage.Usage{InputTokens: input, CachedInputTokens: cached, CacheWriteInputTokens: cacheWrite, OutputTokens: output, ReasoningTokens: reasoning, TotalTokens: total, CostUSD: numericFloat64(data["cost"])})
}

func numericInt64(value any) int64 { return int64(numericFloat64(value)) }
func numericFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		result, _ := typed.Float64()
		return result
	}
	return 0
}

func collectStderr(ctx context.Context, reader io.Reader, done chan<- string) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		agentstream.Emit(ctx, agentstream.Event{Level: "warning", Kind: "runtime", Message: "GitHub Copilot runtime message", Detail: truncate(line, 1200)})
	}
	done <- strings.Join(lines, "\n")
}

func emitEvent(ctx context.Context, event map[string]any) {
	eventType := strings.ToLower(stringValue(event, "type", "event", "kind"))
	message := strings.TrimSpace(textValue(event))
	switch {
	case strings.Contains(eventType, "tool"):
		agentstream.Emit(ctx, agentstream.Event{Kind: "tool", Message: firstNonEmpty(stringValue(event, "name", "tool"), "Using GitHub Copilot tool"), Detail: truncate(message, 1200)})
	case strings.Contains(eventType, "assistant") || strings.Contains(eventType, "message"):
		agentstream.Emit(ctx, agentstream.Event{Kind: "message", Message: "Agent response updated", Detail: truncate(message, 1200)})
	case strings.Contains(eventType, "error"):
		agentstream.Emit(ctx, agentstream.Event{Level: "error", Kind: "runtime", Message: "GitHub Copilot runtime error", Detail: truncate(message, 1200)})
	}
}

func stringValue(event map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := event[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if session, ok := event["session"].(map[string]any); ok {
		for _, key := range keys {
			if value, ok := session[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func sessionIDFromEvent(event map[string]any, eventType string) string {
	if sessionID := stringValue(event, "session_id", "sessionId"); sessionID != "" {
		return sessionID
	}
	if session, ok := event["session"].(map[string]any); ok {
		if sessionID := stringValue(session, "id"); sessionID != "" {
			return sessionID
		}
	}
	if strings.Contains(eventType, "session") {
		return stringValue(event, "id")
	}
	return ""
}

func textValue(event map[string]any) string {
	for _, key := range []string{"result", "response", "text", "message", "content", "output"} {
		switch value := event[key].(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		case []any:
			if text := textFromParts(value); text != "" {
				return text
			}
		case map[string]any:
			if text := textValue(value); text != "" {
				return text
			}
		}
	}
	if data, ok := event["data"].(map[string]any); ok {
		return textValue(data)
	}
	return ""
}

func textFromParts(parts []any) string {
	values := make([]string, 0, len(parts))
	for _, raw := range parts {
		switch part := raw.(type) {
		case string:
			values = append(values, strings.TrimSpace(part))
		case map[string]any:
			values = append(values, textValue(part))
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func structuredValue(event map[string]any) json.RawMessage {
	for _, key := range []string{"structured_output", "structuredOutput", "json", "payload"} {
		if raw, ok := event[key]; ok {
			if data := rawJSON(raw); len(data) > 0 {
				return data
			}
		}
	}
	return nil
}

func isFinalEvent(eventType string) bool {
	return strings.Contains(eventType, "result") ||
		strings.Contains(eventType, "final") ||
		strings.Contains(eventType, "complete") ||
		strings.Contains(eventType, "done")
}

func canUseTextAsJSONCandidate(eventType string) bool {
	return isFinalEvent(eventType) ||
		strings.Contains(eventType, "assistant") ||
		strings.Contains(eventType, "message")
}

func rawJSON(value any) json.RawMessage {
	switch typed := value.(type) {
	case nil:
		return nil
	case json.RawMessage:
		if json.Valid(typed) {
			return typed
		}
	case string:
		return extractJSONPayload(typed)
	default:
		data, err := json.Marshal(typed)
		if err == nil && json.Valid(data) {
			return data
		}
	}
	return nil
}

func extractJSONPayload(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	if json.Valid([]byte(trimmed)) {
		return []byte(trimmed)
	}
	start := strings.Index(trimmed, "{")
	for start >= 0 && start < len(trimmed) {
		decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
		var payload map[string]any
		if err := decoder.Decode(&payload); err == nil {
			consumed := decoder.InputOffset()
			candidate := []byte(trimmed[start : start+int(consumed)])
			if json.Valid(candidate) {
				return candidate
			}
		}
		next := strings.Index(trimmed[start+1:], "{")
		if next < 0 {
			break
		}
		start += next + 1
	}
	return nil
}

func normalizeModel(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return DefaultModel
}

func newSessionID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate GitHub Copilot session ID: %w", err)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

func normalizeEffort(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return value
	default:
		return DefaultEffort
	}
}

func isResumeFailure(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "resume") ||
		strings.Contains(message, "no conversation found") ||
		strings.Contains(message, "--attachment file type not supported") ||
		strings.Contains(message, "session") && (strings.Contains(message, "not found") || strings.Contains(message, "invalid") || strings.Contains(message, "unknown"))
}

func schemaJSON(schema any) ([]byte, error) {
	if raw, ok := schema.(string); ok {
		trimmed := strings.TrimSpace(raw)
		if !json.Valid([]byte(trimmed)) {
			return nil, fmt.Errorf("GitHub Copilot JSON schema string is invalid JSON")
		}
		return []byte(trimmed), nil
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode GitHub Copilot JSON schema: %w", err)
	}
	return encoded, nil
}

func Payload(result Result) []byte {
	if len(result.StructuredOutput) > 0 {
		return result.StructuredOutput
	}
	return []byte(result.Result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
