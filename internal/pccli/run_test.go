package pccli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectsListCommandReturnsProjectJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			writeTestJSON(w, http.StatusOK, []project{{ID: "project-1", Name: "AI-Product-Team", Path: `E:\Projects\AI-Product-Team`}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	exitCode := runner.Run([]string{"projects", "list", "--server", server.URL})
	if exitCode != 0 {
		t.Fatalf("expected success, got %d: %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"apiVersion": "v1"`) || !strings.Contains(stdout.String(), `"id": "project-1"`) {
		t.Fatalf("unexpected JSON output: %s", stdout.String())
	}
}

func TestNewCommandResolvesProjectAndCreatesTodo(t *testing.T) {
	var receivedType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			writeTestJSON(w, http.StatusOK, []project{{ID: "project-1", Name: "AI-Product-Team", Path: `E:\Projects\AI-Product-Team`}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/project-1/automation/backlog":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			receivedType, _ = payload["type"].(string)
			writeTestJSON(w, http.StatusCreated, map[string]any{"apiVersion": "v1", "ticket": map[string]any{"id": "backlog-1", "key": "TODO-001", "type": "todo", "title": payload["title"]}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	exitCode := runner.Run([]string{"new", "--server", server.URL, "--type", "todo", "--title", "Remote task", "--description", "Create this task through the ProductCrew API."})
	if exitCode != 0 {
		t.Fatalf("expected success, got %d: %s", exitCode, stderr.String())
	}
	if receivedType != "todo" {
		t.Fatalf("expected public todo type, got %q", receivedType)
	}
	if !strings.Contains(stdout.String(), `"apiVersion": "v1"`) || !strings.Contains(stdout.String(), `"key": "TODO-001"`) {
		t.Fatalf("unexpected JSON output: %s", stdout.String())
	}
}

func TestAutopilotCommandReturnsServerJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			writeTestJSON(w, http.StatusOK, []project{{ID: "project-1", Name: "ProductCrew"}})
		case "/api/projects/project-1/automation/autopilot":
			var payload struct {
				ID  string `json:"id"`
				All bool   `json:"all"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ID != "FEAT-013" || payload.All {
				t.Fatalf("unexpected Autopilot payload: %+v", payload)
			}
			writeTestJSON(w, http.StatusAccepted, map[string]any{"apiVersion": "v1", "results": []map[string]any{{"key": payload.ID, "action": "planning_started", "state": "planning"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	exitCode := runner.Run([]string{"autopilot", "--server", server.URL, "--project", "ProductCrew", "--id", "FEAT-013"})
	if exitCode != 0 {
		t.Fatalf("expected success, got %d: %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "planning_started"`) {
		t.Fatalf("unexpected JSON output: %s", stdout.String())
	}
}

func TestAutopilotListPrintsTriggerableTicketKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			writeTestJSON(w, http.StatusOK, []project{{ID: "project-1", Name: "ProductCrew"}})
		case "/api/projects/project-1/automation/status":
			if r.URL.Query().Get("status") != "backlog" {
				t.Fatalf("expected backlog filter, got %q", r.URL.RawQuery)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"tickets": []map[string]any{
				{"key": "BUG-004", "title": "Fix duplicate task numbering"},
				{"key": "FEAT-013", "title": "CLI and REST API for local automation"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	exitCode := runner.Run([]string{"autopilot", "--server", server.URL, "--project", "ProductCrew", "--list"})
	if exitCode != 0 {
		t.Fatalf("expected success, got %d: %s", exitCode, stderr.String())
	}
	expected := "BUG-004 : Fix duplicate task numbering\nFEAT-013 : CLI and REST API for local automation\n"
	if stdout.String() != expected {
		t.Fatalf("unexpected backlog list:\n%s", stdout.String())
	}
}

func TestExploreAndStatusUseApprovedAutomationRoutes(t *testing.T) {
	requestedPaths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			writeTestJSON(w, http.StatusOK, []project{{ID: "project-1", Name: "ProductCrew"}})
		case "/api/projects/project-1/automation/explore/FEAT-013":
			requestedPaths = append(requestedPaths, r.URL.RequestURI())
			writeTestJSON(w, http.StatusOK, map[string]any{"apiVersion": "v1", "ticket": map[string]any{"key": "FEAT-013"}})
		case "/api/projects/project-1/automation/status":
			requestedPaths = append(requestedPaths, r.URL.RequestURI())
			if r.URL.Query().Get("type") != "bug" || r.URL.Query().Get("status") != "blocked" {
				t.Fatalf("unexpected status query: %q", r.URL.RawQuery)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"apiVersion": "v1", "tickets": []map[string]any{{"key": "BUG-004"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	if exitCode := runner.Run([]string{"explore", "--server", server.URL, "--project", "ProductCrew", "--id", "FEAT-013"}); exitCode != 0 {
		t.Fatalf("expected explore success, got %d: %s", exitCode, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := runner.Run([]string{"status", "--server", server.URL, "--project", "ProductCrew", "--type", "bug", "--status", "blocked"}); exitCode != 0 {
		t.Fatalf("expected status success, got %d: %s", exitCode, stderr.String())
	}
	if len(requestedPaths) != 2 {
		t.Fatalf("expected approved automation routes, got %v", requestedPaths)
	}
	if requestedPaths[0] != "/api/projects/project-1/automation/explore/FEAT-013" {
		t.Fatalf("unexpected explore route: %s", requestedPaths[0])
	}
	if requestedPaths[1] != "/api/projects/project-1/automation/status?status=blocked&type=bug" &&
		requestedPaths[1] != "/api/projects/project-1/automation/status?type=bug&status=blocked" {
		t.Fatalf("unexpected status route: %s", requestedPaths[1])
	}
}

func TestCommandValidationErrorsAreJSON(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	exitCode := runner.Run([]string{"autopilot", "--id", "FEAT-013", "--all"})
	if exitCode != 2 {
		t.Fatalf("expected validation exit code 2, got %d", exitCode)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code": "invalid_autopilot_target"`) {
		t.Fatalf("expected JSON validation error, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
