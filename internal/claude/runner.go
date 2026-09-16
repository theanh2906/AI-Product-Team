package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

type RunConfig struct {
	CWD            string
	AdditionalDirs []string
	SystemPrompt   string
	Model          string
	Effort         string
	Schema         any
	SessionID      string
	PermissionMode string
	AllowedTools   []string
	Timeout        time.Duration
}

type Result struct {
	SessionID        string
	Result           string
	StructuredOutput json.RawMessage
}

type cliResponse struct {
	SessionID        string          `json:"session_id"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Usage            struct {
		InputTokens              int64 `json:"input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
	} `json:"usage"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

const (
	DefaultModel  = "sonnet"
	DefaultEffort = "high"
)

func RunJSON(ctx context.Context, prompt string, config RunConfig) (Result, error) {
	if strings.TrimSpace(config.CWD) == "" {
		return Result{}, fmt.Errorf("Claude Code working directory is required")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := runJSONOnce(runContext, prompt, config)
	if err == nil {
		return result, nil
	}
	if strings.TrimSpace(config.SessionID) != "" && isResumeFailure(err) {
		agentstream.Emit(ctx, agentstream.Event{Level: "warning", Kind: "thread", Message: "Claude session could not resume; starting a new session"})
		config.SessionID = ""
		return runJSONOnce(runContext, prompt, config)
	}
	return Result{}, err
}

func runJSONOnce(ctx context.Context, prompt string, config RunConfig) (Result, error) {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	for _, directory := range config.AdditionalDirs {
		if strings.TrimSpace(directory) != "" {
			args = append(args, "--add-dir", directory)
		}
	}
	sessionID := strings.TrimSpace(config.SessionID)
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if strings.TrimSpace(config.SystemPrompt) != "" {
		args = append(args, "--append-system-prompt", config.SystemPrompt)
	}
	args = append(args, "--model", normalizeModel(config.Model))
	args = append(args, "--effort", normalizeEffort(config.Effort))
	if strings.TrimSpace(config.PermissionMode) != "" {
		args = append(args, "--permission-mode", strings.TrimSpace(config.PermissionMode))
	}
	if len(config.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(config.AllowedTools, ","))
	}
	if config.Schema != nil {
		schema, err := schemaJSON(config.Schema)
		if err != nil {
			return Result{}, err
		}
		args = append(args, "--json-schema", string(schema))
	}

	command := exec.CommandContext(ctx, "claude", args...)
	command.Dir = config.CWD
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open Claude Code stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open Claude Code stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("start Claude Code: %w", err)
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "turn", Message: "Claude Code session started"})
	stderrDone := make(chan string, 1)
	go collectClaudeStderr(ctx, stderr, stderrDone)

	response, scanErr := readClaudeStream(ctx, stdout)
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
		return Result{}, fmt.Errorf("Claude Code failed: %s", truncate(message, 2000))
	}
	if strings.TrimSpace(response.SessionID) == "" {
		return Result{}, fmt.Errorf("Claude Code stream ended without a result event")
	}
	return Result{SessionID: response.SessionID, Result: response.Result, StructuredOutput: response.StructuredOutput}, nil
}

func readClaudeStream(ctx context.Context, reader io.Reader) (cliResponse, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var response cliResponse
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		emitClaudeEvent(ctx, event)
		if eventType, _ := event["type"].(string); eventType == "result" {
			if err := json.Unmarshal(line, &response); err != nil {
				return cliResponse{}, fmt.Errorf("decode Claude Code result event: %w", err)
			}
			total := response.Usage.InputTokens + response.Usage.CacheReadInputTokens + response.Usage.CacheCreationInputTokens + response.Usage.OutputTokens
			if total > 0 || response.TotalCostUSD > 0 {
				agentusage.Record(ctx, agentusage.Usage{InputTokens: response.Usage.InputTokens, CachedInputTokens: response.Usage.CacheReadInputTokens, CacheWriteInputTokens: response.Usage.CacheCreationInputTokens, OutputTokens: response.Usage.OutputTokens, TotalTokens: total, CostUSD: response.TotalCostUSD})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return cliResponse{}, fmt.Errorf("read Claude Code stream: %w", err)
	}
	return response, nil
}

func collectClaudeStderr(ctx context.Context, reader io.Reader, done chan<- string) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		agentstream.Emit(ctx, agentstream.Event{Level: "warning", Kind: "runtime", Message: "Claude Code runtime message", Detail: truncate(line, 1200)})
	}
	done <- strings.Join(lines, "\n")
}

func emitClaudeEvent(ctx context.Context, event map[string]any) {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "system":
		if subtype, _ := event["subtype"].(string); subtype == "init" {
			agentstream.Emit(ctx, agentstream.Event{Kind: "turn", Message: "Claude Code initialized"})
		}
	case "assistant":
		message, _ := event["message"].(map[string]any)
		content, _ := message["content"].([]any)
		for _, raw := range content {
			block, _ := raw.(map[string]any)
			blockType, _ := block["type"].(string)
			switch blockType {
			case "tool_use":
				name, _ := block["name"].(string)
				agentstream.Emit(ctx, agentstream.Event{Kind: "tool", Message: "Using " + strings.TrimSpace(name), Detail: compactClaudeValue(block["input"])})
			case "text":
				text, _ := block["text"].(string)
				agentstream.Emit(ctx, agentstream.Event{Kind: "message", Message: "Agent response updated", Detail: truncate(strings.TrimSpace(text), 1200)})
			}
		}
	case "user":
		agentstream.Emit(ctx, agentstream.Event{Kind: "tool", Message: "Tool result received"})
	case "result":
		subtype, _ := event["subtype"].(string)
		level := "info"
		message := "Claude Code turn completed"
		if subtype != "success" && subtype != "" {
			level = "error"
			message = "Claude Code turn ended: " + subtype
		}
		agentstream.Emit(ctx, agentstream.Event{Level: level, Kind: "turn", Message: message})
	}
}

func compactClaudeValue(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return truncate(string(data), 1200)
}

func normalizeModel(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return DefaultModel
}

func normalizeEffort(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "low", "medium", "high", "xhigh", "max":
		return value
	default:
		return DefaultEffort
	}
}

func isResumeFailure(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "resume") ||
		strings.Contains(message, "no conversation found") ||
		strings.Contains(message, "session") && (strings.Contains(message, "not found") || strings.Contains(message, "invalid") || strings.Contains(message, "unknown"))
}

func schemaJSON(schema any) ([]byte, error) {
	if raw, ok := schema.(string); ok {
		trimmed := strings.TrimSpace(raw)
		if !json.Valid([]byte(trimmed)) {
			return nil, fmt.Errorf("Claude JSON schema string is invalid JSON")
		}
		return []byte(trimmed), nil
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode Claude JSON schema: %w", err)
	}
	return encoded, nil
}

func ReadOnlyTools() []string {
	return []string{"Read", "Grep", "Glob", "LS"}
}

func WorkspaceWriteTools() []string {
	return []string{"Read", "Grep", "Glob", "LS", "Edit", "MultiEdit", "Write", "Bash"}
}

func Payload(result Result) []byte {
	if len(result.StructuredOutput) > 0 {
		return result.StructuredOutput
	}
	return []byte(result.Result)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
