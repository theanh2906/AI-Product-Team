package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

const (
	defaultExecutable      = "codex"
	defaultClientName      = "productcrew"
	defaultClientVersion   = "dev"
	defaultInitializeLimit = 15 * time.Second
)

// Config controls the local Codex app-server child process.
type Config struct {
	Executable    string
	ClientName    string
	ClientVersion string
}

// ThreadConfig contains the stable settings assigned to a Codex thread.
type ThreadConfig struct {
	Model                 string
	CWD                   string
	DeveloperInstructions string
	Sandbox               string
	ApprovalPolicy        string
	Ephemeral             bool
	RuntimeWorkspaceRoots []string
	WritableRoots         []string
}

// TurnConfig contains settings that may be overridden for one turn.
type TurnConfig struct {
	Model        string
	Effort       string
	CWD          string
	OutputSchema any
}

// TurnResult is the terminal result of one Codex turn.
type TurnResult struct {
	ThreadID      string
	TurnID        string
	Status        string
	FinalResponse string
}

// Event exposes raw app-server notifications for persistence and UI streaming.
type Event struct {
	Method string
	Params json.RawMessage
}

// RPCError is returned when app-server rejects a request.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("codex app-server error %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type turnState struct {
	done          chan struct{}
	threadID      string
	status        string
	finalResponse strings.Builder
	err           error
	completed     bool
	reporter      agentstream.Reporter
	usage         *agentusage.Collector
}

type processTransport struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (t *processTransport) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *processTransport) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *processTransport) Close() error {
	stdinErr := t.stdin.Close()
	stdoutErr := t.stdout.Close()
	return errors.Join(stdinErr, stdoutErr)
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// Client owns one local Codex app-server process and multiplexes JSON-RPC calls.
type Client struct {
	transport io.ReadWriteCloser
	command   *exec.Cmd
	stderr    *synchronizedBuffer

	writeMu         sync.Mutex
	mu              sync.Mutex
	pending         map[string]chan rpcResponse
	turns           map[string]*turnState
	threadReporters map[string]agentstream.Reporter
	threadUsage     map[string]*agentusage.Collector
	nextID          atomic.Int64

	events chan Event
	done   chan struct{}
	wait   chan error

	closeOnce sync.Once
	readErr   error

	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
}

// Dial starts codex app-server over stdio and completes the initialize handshake.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Executable == "" {
		config.Executable = resolveDefaultExecutable()
	}
	if config.ClientName == "" {
		config.ClientName = defaultClientName
	}
	if config.ClientVersion == "" {
		config.ClientVersion = defaultClientVersion
	}

	command := exec.CommandContext(ctx, config.Executable, "app-server", "--listen", "stdio://")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex app-server stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex app-server stdout: %w", err)
	}
	stderr := &synchronizedBuffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}

	client := newClient(&processTransport{stdin: stdin, stdout: stdout})
	client.command = command
	client.stderr = stderr
	go func() { client.wait <- command.Wait() }()

	initializeContext, cancel := context.WithTimeout(ctx, defaultInitializeLimit)
	defer cancel()
	if err := client.initialize(initializeContext, config); err != nil {
		_ = client.Close()
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("initialize Codex app-server: %w: %s", err, message)
		}
		return nil, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	return client, nil
}

func resolveDefaultExecutable() string {
	for _, variable := range []string{"CODEX_EXECUTABLE", "CODEX_CLI_PATH"} {
		if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
			return value
		}
	}
	if runtime.GOOS == "windows" {
		if bundled := latestBundledWindowsExecutable(os.Getenv("LOCALAPPDATA")); bundled != "" {
			return bundled
		}
	}
	if executable, err := exec.LookPath(defaultExecutable); err == nil {
		return executable
	}
	return defaultExecutable
}

func latestBundledWindowsExecutable(localAppData string) string {
	if strings.TrimSpace(localAppData) == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(localAppData, "OpenAI", "Codex", "bin", "*", "codex.exe"))
	type candidate struct {
		path    string
		updated time.Time
	}
	candidates := make([]candidate, 0, len(matches))
	for _, match := range matches {
		// Sandboxed threads need the helper shipped beside the Desktop runtime.
		// A stale PATH-only CLI without this sibling can connect successfully but
		// fail every command with orchestrator_helper_launch_failed on Windows.
		if _, err := os.Stat(filepath.Join(filepath.Dir(match), "codex-windows-sandbox-setup.exe")); err != nil {
			continue
		}
		info, err := os.Stat(match)
		if err == nil {
			candidates = append(candidates, candidate{path: match, updated: info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].updated.After(candidates[j].updated) })
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].path
}

func newClient(transport io.ReadWriteCloser) *Client {
	client := &Client{
		transport:       transport,
		pending:         make(map[string]chan rpcResponse),
		turns:           make(map[string]*turnState),
		threadReporters: make(map[string]agentstream.Reporter),
		threadUsage:     make(map[string]*agentusage.Collector),
		events:          make(chan Event, 256),
		done:            make(chan struct{}),
		wait:            make(chan error, 1),
		shutdownDone:    make(chan struct{}),
	}
	go client.readLoop()
	return client
}

func (c *Client) initialize(ctx context.Context, config Config) error {
	params := map[string]any{
		"clientInfo": map[string]string{
			"name":    config.ClientName,
			"title":   "ProductCrew",
			"version": config.ClientVersion,
		},
		"capabilities": map[string]any{
			"experimentalApi": false,
		},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// Events returns app-server notifications. Consumers must not assume every event
// is delivered; durable orchestration state should be based on method results.
func (c *Client) Events() <-chan Event { return c.events }

// StartThread creates a local, resumable Codex thread.
func (c *Client) StartThread(ctx context.Context, config ThreadConfig) (string, error) {
	agentstream.Emit(ctx, agentstream.Event{Kind: "thread", Message: "Preparing a new Codex thread"})
	sandbox := any(optionalString(config.Sandbox))
	if len(config.WritableRoots) > 0 && config.Sandbox == "workspace-write" {
		sandbox = map[string]any{"type": "workspaceWrite", "writableRoots": config.WritableRoots}
	}
	params := map[string]any{
		"model":                 optionalString(config.Model),
		"cwd":                   optionalString(config.CWD),
		"developerInstructions": optionalString(config.DeveloperInstructions),
		"sandbox":               sandbox,
		"approvalPolicy":        optionalString(config.ApprovalPolicy),
		"ephemeral":             config.Ephemeral,
	}
	if len(config.RuntimeWorkspaceRoots) > 0 {
		params["runtimeWorkspaceRoots"] = config.RuntimeWorkspaceRoots
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", params, &response); err != nil {
		return "", err
	}
	if response.Thread.ID == "" {
		return "", fmt.Errorf("Codex app-server returned an empty thread id")
	}
	agentstream.Emit(ctx, agentstream.Event{Kind: "thread", Message: "Codex thread ready"})
	return response.Thread.ID, nil
}

// ResumeThread restores a thread previously created by Codex.
func (c *Client) ResumeThread(ctx context.Context, threadID string, config ThreadConfig) error {
	agentstream.Emit(ctx, agentstream.Event{Kind: "thread", Message: "Resuming the Codex thread"})
	sandbox := any(optionalString(config.Sandbox))
	if len(config.WritableRoots) > 0 && config.Sandbox == "workspace-write" {
		sandbox = map[string]any{"type": "workspaceWrite", "writableRoots": config.WritableRoots}
	}
	params := map[string]any{
		"threadId":              threadID,
		"model":                 optionalString(config.Model),
		"cwd":                   optionalString(config.CWD),
		"developerInstructions": optionalString(config.DeveloperInstructions),
		"sandbox":               sandbox,
		"approvalPolicy":        optionalString(config.ApprovalPolicy),
	}
	if len(config.RuntimeWorkspaceRoots) > 0 {
		params["runtimeWorkspaceRoots"] = config.RuntimeWorkspaceRoots
	}
	err := c.call(ctx, "thread/resume", params, nil)
	if err == nil {
		agentstream.Emit(ctx, agentstream.Event{Kind: "thread", Message: "Codex thread resumed"})
	}
	return err
}

// RunTurn sends text to an existing thread and waits for its terminal notification.
func (c *Client) RunTurn(ctx context.Context, threadID, prompt string, config TurnConfig) (TurnResult, error) {
	agentstream.Emit(ctx, agentstream.Event{Kind: "turn", Message: "Codex turn requested"})
	reporter := agentstream.FromContext(ctx)
	usageCollector := agentusage.FromContext(ctx)
	if reporter != nil || usageCollector != nil {
		c.mu.Lock()
		if reporter != nil {
			c.threadReporters[threadID] = reporter
		}
		if usageCollector != nil {
			c.threadUsage[threadID] = usageCollector
		}
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			delete(c.threadReporters, threadID)
			delete(c.threadUsage, threadID)
			c.mu.Unlock()
		}()
	}
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": prompt,
		}},
	}
	if config.Model != "" {
		params["model"] = config.Model
	}
	if config.Effort != "" {
		params["effort"] = config.Effort
	}
	if config.CWD != "" {
		params["cwd"] = config.CWD
	}
	if config.OutputSchema != nil {
		params["outputSchema"] = config.OutputSchema
	}

	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", params, &response); err != nil {
		return TurnResult{}, err
	}
	if response.Turn.ID == "" {
		return TurnResult{}, fmt.Errorf("Codex app-server returned an empty turn id")
	}

	state := c.getTurn(response.Turn.ID)
	c.mu.Lock()
	state.reporter = agentstream.FromContext(ctx)
	state.usage = agentusage.FromContext(ctx)
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return TurnResult{}, ctx.Err()
	case <-c.done:
		return TurnResult{}, c.connectionError()
	case <-state.done:
	}

	c.mu.Lock()
	result := TurnResult{
		ThreadID:      state.threadID,
		TurnID:        response.Turn.ID,
		Status:        state.status,
		FinalResponse: state.finalResponse.String(),
	}
	err := state.err
	delete(c.turns, response.Turn.ID)
	c.mu.Unlock()
	if err != nil {
		return result, err
	}
	if result.Status != "completed" {
		return result, fmt.Errorf("Codex turn ended with status %q", result.Status)
	}
	return result, nil
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (c *Client) call(ctx context.Context, method string, params any, destination any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprintf("%d", id)
	responseChannel := make(chan rpcResponse, 1)

	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return c.connectionError()
	default:
	}
	c.pending[key] = responseChannel
	c.mu.Unlock()

	request := map[string]any{"id": id, "method": method, "params": params}
	if err := c.write(request); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return ctx.Err()
	case <-c.done:
		return c.connectionError()
	case response := <-responseChannel:
		if response.err != nil {
			return response.err
		}
		if destination == nil || len(response.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.result, destination); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) notify(method string, params any) error {
	return c.write(map[string]any{"method": method, "params": params})
}

func (c *Client) write(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Codex app-server message: %w", err)
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.transport.Write(data); err != nil {
		return fmt.Errorf("write Codex app-server message: %w", err)
	}
	return nil
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.transport)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.Method != "" && len(message.ID) > 0 {
			c.rejectServerRequest(message.ID, message.Method)
			continue
		}
		if len(message.ID) > 0 {
			c.resolveResponse(string(message.ID), message.Result, message.Error)
			continue
		}
		if message.Method != "" {
			c.handleNotification(message.Method, message.Params)
		}
	}
	c.failConnection(scanner.Err())
}

func (c *Client) resolveResponse(id string, result json.RawMessage, rpcErr *RPCError) {
	c.mu.Lock()
	channel := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if channel == nil {
		return
	}
	if rpcErr != nil {
		channel <- rpcResponse{err: rpcErr}
		return
	}
	channel <- rpcResponse{result: result}
}

func (c *Client) rejectServerRequest(id json.RawMessage, method string) {
	_ = c.write(map[string]any{
		"id": json.RawMessage(id),
		"error": map[string]any{
			"code":    -32601,
			"message": "The background orchestrator cannot service " + method,
		},
	})
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "thread/tokenUsage/updated":
		var notification struct {
			ThreadID   string `json:"threadId"`
			TurnID     string `json:"turnId"`
			TokenUsage struct {
				Last struct {
					InputTokens           int64 `json:"inputTokens"`
					CachedInputTokens     int64 `json:"cachedInputTokens"`
					CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
					OutputTokens          int64 `json:"outputTokens"`
					ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
					TotalTokens           int64 `json:"totalTokens"`
				} `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(params, &notification) == nil && notification.TurnID != "" {
			state := c.getTurn(notification.TurnID)
			c.mu.Lock()
			collector := state.usage
			if collector == nil {
				collector = c.threadUsage[notification.ThreadID]
			}
			c.mu.Unlock()
			if collector != nil {
				last := notification.TokenUsage.Last
				collector.Add(agentusage.Usage{InputTokens: last.InputTokens, CachedInputTokens: last.CachedInputTokens, CacheWriteInputTokens: last.CacheWriteInputTokens, OutputTokens: last.OutputTokens, ReasoningTokens: last.ReasoningOutputTokens, TotalTokens: last.TotalTokens})
			}
		}
	case "item/completed":
		var notification struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &notification) == nil && notification.Item.Type == "agentMessage" {
			state := c.getTurn(notification.TurnID)
			c.mu.Lock()
			state.threadID = notification.ThreadID
			// A turn may emit several completed agent messages around tool calls.
			// The last one is the terminal assistant response (and, for structured
			// turns, the only payload that should be decoded as the output schema).
			state.finalResponse.Reset()
			state.finalResponse.WriteString(notification.Item.Text)
			c.mu.Unlock()
		}
	case "turn/completed":
		var notification struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &notification) == nil && notification.Turn.ID != "" {
			state := c.getTurn(notification.Turn.ID)
			c.mu.Lock()
			state.threadID = notification.ThreadID
			state.status = notification.Turn.Status
			if notification.Turn.Error != nil {
				state.err = errors.New(notification.Turn.Error.Message)
			}
			if !state.completed {
				state.completed = true
				close(state.done)
			}
			c.mu.Unlock()
		}
	}
	c.reportTurnEvent(method, params)

	select {
	case c.events <- Event{Method: method, Params: append(json.RawMessage(nil), params...)}:
	default:
	}
}

func (c *Client) reportTurnEvent(method string, params json.RawMessage) {
	var envelope struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
		Item  map[string]any `json:"item"`
		Delta string         `json:"delta"`
	}
	if json.Unmarshal(params, &envelope) != nil {
		return
	}
	turnID := envelope.TurnID
	if turnID == "" {
		turnID = envelope.Turn.ID
	}
	if turnID == "" {
		return
	}
	c.mu.Lock()
	state := c.turns[turnID]
	var reporter agentstream.Reporter
	if state != nil {
		reporter = state.reporter
	}
	if reporter == nil {
		reporter = c.threadReporters[envelope.ThreadID]
	}
	c.mu.Unlock()
	if reporter == nil {
		return
	}
	if event, ok := normalizedCodexEvent(method, envelope.Item, envelope.Delta); ok {
		reporter.Report(event)
	}
}

func normalizedCodexEvent(method string, item map[string]any, delta string) (agentstream.Event, bool) {
	if method == "turn/completed" {
		return agentstream.Event{Kind: "turn", Message: "Codex turn completed"}, true
	}
	if method == "turn/started" {
		return agentstream.Event{Kind: "turn", Message: "Codex is processing the task"}, true
	}
	if !strings.HasPrefix(method, "item/") || len(item) == 0 {
		return agentstream.Event{}, false
	}
	itemType, _ := item["type"].(string)
	phase := "started"
	if strings.HasSuffix(method, "/completed") {
		phase = "completed"
	}
	switch itemType {
	case "commandExecution":
		command := firstString(item, "command", "displayCommand")
		return agentstream.Event{Kind: "command", Message: "Command " + phase, Detail: command}, true
	case "fileChange":
		return agentstream.Event{Kind: "file", Message: "File changes " + phase, Detail: compactJSON(item["changes"])}, true
	case "mcpToolCall":
		name := firstString(item, "tool", "name", "server")
		return agentstream.Event{Kind: "tool", Message: "Tool call " + phase, Detail: name}, true
	case "webSearch":
		return agentstream.Event{Kind: "search", Message: "Web search " + phase, Detail: firstString(item, "query")}, true
	case "reasoning":
		return agentstream.Event{Kind: "reasoning", Message: "Codex reasoning updated", Detail: firstString(item, "summary", "text")}, true
	case "agentMessage":
		if phase != "completed" {
			return agentstream.Event{}, false
		}
		return agentstream.Event{Kind: "message", Message: "Agent response updated", Detail: firstString(item, "text")}, true
	default:
		if itemType == "" {
			return agentstream.Event{}, false
		}
		return agentstream.Event{Kind: "activity", Message: humanizeCodexType(itemType) + " " + phase}, true
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return truncateEventDetail(strings.TrimSpace(text), 1200)
		}
	}
	return ""
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return truncateEventDetail(string(data), 1200)
}

func humanizeCodexType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Activity"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func truncateEventDetail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func (c *Client) getTurn(turnID string) *turnState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.turns[turnID]
	if state == nil {
		state = &turnState{done: make(chan struct{})}
		c.turns[turnID] = state
	}
	return state
}

func (c *Client) failConnection(err error) {
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	c.readErr = err
	for id, channel := range c.pending {
		channel <- rpcResponse{err: fmt.Errorf("Codex app-server connection closed: %w", err)}
		delete(c.pending, id)
	}
	for _, state := range c.turns {
		if !state.completed {
			state.err = fmt.Errorf("Codex app-server connection closed: %w", err)
			state.completed = true
			close(state.done)
		}
	}
	c.mu.Unlock()
	c.closeOnce.Do(func() {
		close(c.done)
		close(c.events)
	})
}

func (c *Client) connectionError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readErr == nil {
		return fmt.Errorf("Codex app-server connection is closed")
	}
	return fmt.Errorf("Codex app-server connection is closed: %w", c.readErr)
}

// Close stops the local app-server child process.
func (c *Client) Close() error {
	c.shutdownOnce.Do(func() {
		defer close(c.shutdownDone)
		_ = c.transport.Close()
		if c.command == nil {
			return
		}
		select {
		case err := <-c.wait:
			c.shutdownErr = normalizeWaitError(err)
		case <-time.After(3 * time.Second):
			if c.command.Process != nil {
				_ = c.command.Process.Kill()
			}
			c.shutdownErr = normalizeWaitError(<-c.wait)
		}
	})
	<-c.shutdownDone
	return c.shutdownErr
}

func normalizeWaitError(err error) error {
	var exitError *exec.ExitError
	if err == nil || errors.As(err, &exitError) {
		return nil
	}
	return err
}
