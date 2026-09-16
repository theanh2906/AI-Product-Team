package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

func TestClientInitializesStartsThreadAndRunsTurn(t *testing.T) {
	clientConnection, serverConnection := net.Pipe()
	client := newClient(clientConnection)
	primaryRoot := t.TempDir()
	additionalRoot := t.TempDir()
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConnection.Close()
	})

	serverError := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverConnection)
		write := func(value any) error {
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			data = append(data, '\n')
			_, err = serverConnection.Write(data)
			return err
		}
		read := func() (map[string]any, error) {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return nil, err
			}
			var value map[string]any
			err = json.Unmarshal(line, &value)
			return value, err
		}

		initialize, err := read()
		if err != nil {
			serverError <- err
			return
		}
		if initialize["method"] != "initialize" {
			serverError <- &unexpectedMethodError{got: initialize["method"], want: "initialize"}
			return
		}
		if err := write(map[string]any{"id": initialize["id"], "result": map[string]any{"serverInfo": map[string]string{"name": "codex"}}}); err != nil {
			serverError <- err
			return
		}
		initialized, err := read()
		if err != nil {
			serverError <- err
			return
		}
		if initialized["method"] != "initialized" {
			serverError <- &unexpectedMethodError{got: initialized["method"], want: "initialized"}
			return
		}

		threadStart, err := read()
		if err != nil {
			serverError <- err
			return
		}
		if threadStart["method"] != "thread/start" {
			serverError <- &unexpectedMethodError{got: threadStart["method"], want: "thread/start"}
			return
		}
		params, _ := threadStart["params"].(map[string]any)
		roots, _ := params["runtimeWorkspaceRoots"].([]any)
		if len(roots) != 2 || roots[0] != primaryRoot || roots[1] != additionalRoot {
			serverError <- fmt.Errorf("unexpected runtime workspace roots: %#v", params["runtimeWorkspaceRoots"])
			return
		}
		sandbox, _ := params["sandbox"].(map[string]any)
		writableRoots, _ := sandbox["writableRoots"].([]any)
		if sandbox["type"] != "workspaceWrite" || len(writableRoots) != 2 {
			serverError <- fmt.Errorf("unexpected workspace sandbox: %#v", params["sandbox"])
			return
		}
		if err := write(map[string]any{"id": threadStart["id"], "result": map[string]any{"thread": map[string]any{"id": "thr_test"}}}); err != nil {
			serverError <- err
			return
		}

		turnStart, err := read()
		if err != nil {
			serverError <- err
			return
		}
		if turnStart["method"] != "turn/start" {
			serverError <- &unexpectedMethodError{got: turnStart["method"], want: "turn/start"}
			return
		}
		if err := write(map[string]any{"id": turnStart["id"], "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress", "items": []any{}}}}); err != nil {
			serverError <- err
			return
		}
		if err := write(map[string]any{
			"method": "item/completed",
			"params": map[string]any{
				"threadId": "thr_test",
				"turnId":   "turn_test",
				"item":     map[string]any{"id": "item_progress", "type": "agentMessage", "text": "Repository inspection complete."},
			},
		}); err != nil {
			serverError <- err
			return
		}
		if err := write(map[string]any{
			"method": "item/completed",
			"params": map[string]any{
				"threadId": "thr_test",
				"turnId":   "turn_test",
				"item":     map[string]any{"id": "item_final", "type": "agentMessage", "text": "Implementation plan ready."},
			},
		}); err != nil {
			serverError <- err
			return
		}
		if err := write(map[string]any{
			"method": "thread/tokenUsage/updated",
			"params": map[string]any{"threadId": "thr_test", "turnId": "turn_test", "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 80, "cachedInputTokens": 20, "outputTokens": 15, "reasoningOutputTokens": 5, "totalTokens": 95}}},
		}); err != nil {
			serverError <- err
			return
		}
		if err := write(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thr_test",
				"turn":     map[string]any{"id": "turn_test", "status": "completed", "items": []any{}},
			},
		}); err != nil {
			serverError <- err
			return
		}
		serverError <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.initialize(ctx, Config{ClientName: "test-client", ClientVersion: "1.0"}); err != nil {
		t.Fatal(err)
	}
	threadID, err := client.StartThread(ctx, ThreadConfig{
		Model:                 "gpt-test",
		CWD:                   primaryRoot,
		Sandbox:               "workspace-write",
		ApprovalPolicy:        "never",
		RuntimeWorkspaceRoots: []string{primaryRoot, additionalRoot},
		WritableRoots:         []string{primaryRoot, additionalRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "thr_test" {
		t.Fatalf("expected thread thr_test, got %q", threadID)
	}
	ctx, usageCollector := agentusage.WithCollector(ctx)
	result, err := client.RunTurn(ctx, threadID, "Plan this work item", TurnConfig{Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.FinalResponse != "Implementation plan ready." {
		t.Fatalf("unexpected turn result: %+v", result)
	}
	if usage := usageCollector.Snapshot(); usage.TotalTokens != 95 || usage.CachedInputTokens != 20 {
		t.Fatalf("unexpected token usage: %+v", usage)
	}
	if err := <-serverError; err != nil {
		t.Fatal(err)
	}
}

func TestNormalizedCodexEventShowsCommandActivity(t *testing.T) {
	event, ok := normalizedCodexEvent("item/started", map[string]any{
		"type": "commandExecution", "command": "go test ./...",
	}, "")
	if !ok || event.Kind != "command" || event.Detail != "go test ./..." {
		t.Fatalf("unexpected normalized event: %+v, ok=%v", event, ok)
	}
}

func TestDialInstalledCodexAppServer(t *testing.T) {
	if os.Getenv("CODEX_APP_SERVER_INTEGRATION") != "1" {
		t.Skip("set CODEX_APP_SERVER_INTEGRATION=1 to use the installed Codex CLI")
	}
	ctx, cancel := context.WithCancel(context.Background())
	client, err := Dial(ctx, Config{ClientName: "productcrew-test", ClientVersion: "test"})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		cancel()
		t.Fatalf("second close must be safe: %v", err)
	}
	cancel()
}

func TestResolveDefaultExecutableHonorsExplicitOverride(t *testing.T) {
	t.Setenv("CODEX_EXECUTABLE", `C:\Tools\codex.exe`)
	t.Setenv("CODEX_CLI_PATH", `C:\Other\codex.exe`)
	if got := resolveDefaultExecutable(); got != `C:\Tools\codex.exe` {
		t.Fatalf("expected explicit executable override, got %q", got)
	}
}

func TestLatestBundledWindowsExecutableRequiresSandboxHelper(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "OpenAI", "Codex", "bin", "older")
	newer := filepath.Join(root, "OpenAI", "Codex", "bin", "newer")
	for _, directory := range []string{older, newer} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "codex.exe"), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(older, "codex-windows-sandbox-setup.exe"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := latestBundledWindowsExecutable(root); got != filepath.Join(older, "codex.exe") {
		t.Fatalf("expected candidate with its sandbox helper, got %q", got)
	}
}

type unexpectedMethodError struct {
	got  any
	want string
}

func (e *unexpectedMethodError) Error() string {
	return fmt.Sprintf("expected method %q, got %v", e.want, e.got)
}
