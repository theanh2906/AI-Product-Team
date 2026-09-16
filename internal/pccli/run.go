package pccli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiVersion = "v1"

type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Client *http.Client
}

type project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type commonOptions struct {
	server  string
	project string
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ", ") }
func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

type commandError struct {
	Code       string
	Message    string
	HTTPStatus int
}

type autopilotListEntry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Display string `json:"display"`
}

func (err *commandError) Error() string { return err.Message }

func (runner Runner) Run(args []string) int {
	runner = runner.withDefaults()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		runner.printHelp()
		return 0
	}
	var err error
	switch args[0] {
	case "projects":
		err = runner.runProjects(args[1:])
	case "new":
		err = runner.runNew(args[1:])
	case "autopilot":
		err = runner.runAutopilot(args[1:])
	case "explore":
		err = runner.runExplore(args[1:])
	case "status":
		err = runner.runStatus(args[1:])
	default:
		err = &commandError{Code: "unknown_command", Message: fmt.Sprintf("Unknown command %q. Run pc help for usage.", args[0])}
	}
	if err == nil {
		return 0
	}
	runner.writeError(err)
	var typed *commandError
	if errors.As(err, &typed) && typed.HTTPStatus == 0 {
		return 2
	}
	return 1
}

func (runner Runner) runProjects(args []string) error {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "list") {
		args = args[1:]
	}
	flags := runner.newFlagSet("projects")
	server := flags.String("server", runner.defaultServer(), "ProductCrew API base URL")
	if err := flags.Parse(args); err != nil {
		return validationError("invalid_arguments", err.Error())
	}
	if flags.NArg() != 0 {
		return validationError("invalid_arguments", "projects supports only the `list` subcommand")
	}
	projects, err := runner.listProjects(*server)
	if err != nil {
		return err
	}
	return runner.writeJSON(map[string]any{"apiVersion": apiVersion, "projects": projects})
}

func (runner Runner) runNew(args []string) error {
	flags := runner.newFlagSet("new")
	common := runner.bindCommon(flags)
	ticketType := flags.String("type", "", "todo, feature, or bug")
	title := flags.String("title", "", "Ticket title")
	description := flags.String("description", "", "Ticket description")
	delivery := flags.String("delivery", "fullstack", "frontend, backend, or fullstack")
	requiresUI := flags.Bool("requires-ui", false, "Ticket requires UI or UX work")
	feasibility := flags.Int("feasibility", 0, "Optional feasibility percentage")
	severity := flags.String("severity", "", "Optional bug severity")
	var criteria stringList
	flags.Var(&criteria, "acceptance", "Acceptance criterion; repeat for multiple values")
	if err := flags.Parse(args); err != nil {
		return validationError("invalid_arguments", err.Error())
	}
	if flags.NArg() != 0 {
		return validationError("invalid_arguments", "new does not accept positional arguments")
	}
	normalizedType := normalizeCLIBacklogType(*ticketType)
	if normalizedType == "" {
		return validationError("invalid_type", "--type must be todo, feature, or bug")
	}
	if strings.TrimSpace(*title) == "" {
		return validationError("missing_title", "--title is required")
	}
	if strings.TrimSpace(*description) == "" {
		return validationError("missing_description", "--description is required")
	}
	baseURL, selected, err := runner.resolveProject(common)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"type": normalizedType, "source": "remote-cli", "title": strings.TrimSpace(*title),
		"description": strings.TrimSpace(*description), "deliveryTarget": strings.ToLower(strings.TrimSpace(*delivery)),
		"requiresUI": *requiresUI, "acceptanceCriteria": []string(criteria), "feasibility": *feasibility, "severity": strings.TrimSpace(*severity),
	}
	var response struct {
		Ticket json.RawMessage `json:"ticket"`
		Item   json.RawMessage `json:"item"`
	}
	if err := runner.requestJSON(http.MethodPost, baseURL+"/api/projects/"+url.PathEscape(selected.ID)+"/automation/backlog", payload, &response); err != nil {
		return err
	}
	ticket := response.Ticket
	if len(ticket) == 0 {
		ticket = response.Item
	}
	return runner.writeJSON(struct {
		APIVersion string          `json:"apiVersion"`
		Project    project         `json:"project"`
		Ticket     json.RawMessage `json:"ticket"`
	}{APIVersion: apiVersion, Project: selected, Ticket: ticket})
}

func (runner Runner) runAutopilot(args []string) error {
	flags := runner.newFlagSet("autopilot")
	common := runner.bindCommon(flags)
	id := flags.String("id", "", "Backlog ticket key or id")
	all := flags.Bool("all", false, "Start all eligible backlog tickets")
	list := flags.Bool("list", false, "List open backlog tickets as ID : title")
	jsonOutput := flags.Bool("json", false, "Emit the backlog list as JSON")
	reviewer := flags.String("reviewer", "Remote CLI", "Reviewer recorded for automatic approval")
	if err := flags.Parse(args); err != nil {
		return validationError("invalid_arguments", err.Error())
	}
	if flags.NArg() != 0 {
		return validationError("invalid_arguments", "autopilot does not accept positional arguments")
	}
	targetCount := 0
	if *all {
		targetCount++
	}
	if strings.TrimSpace(*id) != "" {
		targetCount++
	}
	if *list {
		targetCount++
	}
	if targetCount != 1 {
		return validationError("invalid_autopilot_target", "Provide exactly one of --id, --all, or --list")
	}
	if *jsonOutput && !*list {
		return validationError("invalid_arguments", "--json can only be used with --list")
	}
	baseURL, selected, err := runner.resolveProject(common)
	if err != nil {
		return err
	}
	if *list {
		return runner.listAutopilotBacklog(baseURL, selected, *jsonOutput)
	}
	payload := map[string]any{"id": strings.TrimSpace(*id), "all": *all, "reviewer": strings.TrimSpace(*reviewer)}
	var response json.RawMessage
	if err := runner.requestJSON(http.MethodPost, baseURL+"/api/projects/"+url.PathEscape(selected.ID)+"/automation/autopilot", payload, &response); err != nil {
		return err
	}
	return runner.writeRawJSON(response)
}

func (runner Runner) listAutopilotBacklog(baseURL string, selected project, jsonOutput bool) error {
	var response struct {
		Tickets []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"tickets"`
	}
	endpoint := baseURL + "/api/projects/" + url.PathEscape(selected.ID) + "/automation/status?status=backlog"
	if err := runner.requestJSON(http.MethodGet, endpoint, nil, &response); err != nil {
		return err
	}
	entries := make([]autopilotListEntry, 0, len(response.Tickets))
	for _, ticket := range response.Tickets {
		title := strings.Join(strings.Fields(ticket.Title), " ")
		entries = append(entries, autopilotListEntry{ID: ticket.Key, Title: title, Display: ticket.Key + " : " + title})
	}
	if jsonOutput {
		return runner.writeJSON(map[string]any{"apiVersion": apiVersion, "project": selected, "tickets": entries})
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(runner.Stdout, "No open backlog tickets.")
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintln(runner.Stdout, entry.Display); err != nil {
			return err
		}
	}
	return nil
}

func (runner Runner) runExplore(args []string) error {
	flags := runner.newFlagSet("explore")
	common := runner.bindCommon(flags)
	id := flags.String("id", "", "Ticket key or id")
	if err := flags.Parse(args); err != nil {
		return validationError("invalid_arguments", err.Error())
	}
	if strings.TrimSpace(*id) == "" {
		return validationError("missing_ticket_id", "--id is required")
	}
	if flags.NArg() != 0 {
		return validationError("invalid_arguments", "explore does not accept positional arguments")
	}
	return runner.fetchTickets(common, strings.TrimSpace(*id), "", "")
}

func (runner Runner) runStatus(args []string) error {
	flags := runner.newFlagSet("status")
	common := runner.bindCommon(flags)
	id := flags.String("id", "", "Optional ticket key or id")
	status := flags.String("status", "", "Optional execution or backlog status filter")
	ticketType := flags.String("type", "", "Optional todo, feature, or bug filter")
	if err := flags.Parse(args); err != nil {
		return validationError("invalid_arguments", err.Error())
	}
	if flags.NArg() != 0 {
		return validationError("invalid_arguments", "status does not accept positional arguments")
	}
	return runner.fetchTickets(common, strings.TrimSpace(*id), strings.TrimSpace(*status), strings.TrimSpace(*ticketType))
}

func (runner Runner) fetchTickets(common *commonOptions, id, status, ticketType string) error {
	baseURL, selected, err := runner.resolveProject(common)
	if err != nil {
		return err
	}
	path := baseURL + "/api/projects/" + url.PathEscape(selected.ID) + "/automation/status"
	if id != "" {
		path = baseURL + "/api/projects/" + url.PathEscape(selected.ID) + "/automation/explore/" + url.PathEscape(id)
	} else {
		query := url.Values{}
		if status != "" {
			query.Set("status", status)
		}
		if ticketType != "" {
			query.Set("type", ticketType)
		}
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
	}
	var response json.RawMessage
	if err := runner.requestJSON(http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	return runner.writeRawJSON(response)
}

func (runner Runner) bindCommon(flags *flag.FlagSet) *commonOptions {
	options := &commonOptions{}
	flags.StringVar(&options.server, "server", runner.defaultServer(), "ProductCrew API base URL")
	flags.StringVar(&options.project, "project", strings.TrimSpace(runner.Getenv("PRODUCTCREW_PROJECT")), "Project id, name, or path")
	return options
}

func (runner Runner) resolveProject(options *commonOptions) (string, project, error) {
	baseURL, err := normalizeServerURL(options.server)
	if err != nil {
		return "", project{}, err
	}
	projects, err := runner.listProjects(baseURL)
	if err != nil {
		return "", project{}, err
	}
	selector := strings.TrimSpace(options.project)
	if selector == "" {
		if len(projects) == 1 {
			return baseURL, projects[0], nil
		}
		return "", project{}, validationError("project_required", "--project is required when ProductCrew has zero or multiple projects")
	}
	for _, candidate := range projects {
		if candidate.ID == selector {
			return baseURL, candidate, nil
		}
	}
	matches := make([]project, 0, 1)
	for _, candidate := range projects {
		nameMatch := strings.EqualFold(candidate.Name, selector)
		pathMatch := candidate.Path != "" && strings.EqualFold(filepath.Clean(candidate.Path), filepath.Clean(selector))
		if nameMatch || pathMatch {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return baseURL, matches[0], nil
	}
	if len(matches) > 1 {
		return "", project{}, validationError("project_ambiguous", "Project name is ambiguous; use the exact project id")
	}
	return "", project{}, validationError("project_not_found", fmt.Sprintf("Project %q was not found", selector))
}

func (runner Runner) listProjects(server string) ([]project, error) {
	baseURL, err := normalizeServerURL(server)
	if err != nil {
		return nil, err
	}
	var projects []project
	if err := runner.requestJSON(http.MethodGet, baseURL+"/api/projects", nil, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (runner Runner) requestJSON(method, endpoint string, payload any, destination any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return &commandError{Code: "encode_request_failed", Message: err.Error()}
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return &commandError{Code: "invalid_request", Message: err.Error()}
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := runner.Client.Do(request)
	if err != nil {
		return &commandError{Code: "productcrew_unreachable", Message: "ProductCrew is not reachable at " + endpoint + ": " + err.Error(), HTTPStatus: http.StatusServiceUnavailable}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return &commandError{Code: "read_response_failed", Message: err.Error(), HTTPStatus: response.StatusCode}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &apiError)
		if apiError.Code == "" {
			apiError.Code = "api_error"
		}
		if apiError.Error == "" {
			apiError.Error = strings.TrimSpace(string(data))
		}
		return &commandError{Code: apiError.Code, Message: apiError.Error, HTTPStatus: response.StatusCode}
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return &commandError{Code: "invalid_api_response", Message: "ProductCrew returned invalid JSON: " + err.Error(), HTTPStatus: response.StatusCode}
	}
	return nil
}

func (runner Runner) writeJSON(value any) error {
	encoder := json.NewEncoder(runner.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func (runner Runner) writeRawJSON(value json.RawMessage) error {
	var normalized any
	if err := json.Unmarshal(value, &normalized); err != nil {
		return &commandError{Code: "invalid_api_response", Message: err.Error()}
	}
	return runner.writeJSON(normalized)
}

func (runner Runner) writeError(err error) {
	value := struct {
		APIVersion string `json:"apiVersion"`
		Error      struct {
			Code       string `json:"code"`
			Message    string `json:"message"`
			HTTPStatus int    `json:"status,omitempty"`
		} `json:"error"`
	}{APIVersion: apiVersion}
	value.Error.Code = "command_failed"
	value.Error.Message = err.Error()
	var typed *commandError
	if errors.As(err, &typed) {
		value.Error.Code = typed.Code
		value.Error.HTTPStatus = typed.HTTPStatus
	}
	encoder := json.NewEncoder(runner.Stderr)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func (runner Runner) printHelp() {
	fmt.Fprintln(runner.Stdout, `ProductCrew automation CLI

Usage:
  pc projects list [--server URL]
  pc new --project ID_OR_NAME --type todo|feature|bug --title TITLE --description DESCRIPTION [options]
  pc autopilot --project ID_OR_NAME (--id TICKET | --all | --list [--json])
  pc explore --project ID_OR_NAME --id TICKET
  pc status --project ID_OR_NAME [--id TICKET] [--status STATUS] [--type TYPE]

Environment:
  PRODUCTCREW_URL       API URL (default http://127.0.0.1:8081)
  PRODUCTCREW_PROJECT   Default project id, name, or path

Commands emit JSON on stdout except autopilot --list; add --json for structured list output.
Errors emit JSON on stderr.`)
}

func (runner Runner) withDefaults() Runner {
	if runner.Stdout == nil {
		runner.Stdout = os.Stdout
	}
	if runner.Stderr == nil {
		runner.Stderr = os.Stderr
	}
	if runner.Getenv == nil {
		runner.Getenv = os.Getenv
	}
	if runner.Client == nil {
		runner.Client = &http.Client{Timeout: 30 * time.Second}
	}
	return runner
}

func (runner Runner) newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func (runner Runner) defaultServer() string {
	if value := strings.TrimSpace(runner.Getenv("PRODUCTCREW_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:8081"
}

func normalizeServerURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", validationError("invalid_server_url", "--server must be an absolute http or https URL")
	}
	return value, nil
}

func validationError(code, message string) error {
	return &commandError{Code: code, Message: message}
}

func normalizeCLIBacklogType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "task", "todo":
		return "todo"
	case "feature":
		return "feature"
	case "bug":
		return "bug"
	default:
		return ""
	}
}
