package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const latestVersionCacheTTL = 30 * time.Minute

type Status string

const (
	StatusReady           Status = "ready"
	StatusUpdateAvailable Status = "update_available"
	StatusNotInstalled    Status = "not_installed"
	StatusCheckFailed     Status = "check_failed"
)

type Check struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	Outcome string `json:"outcome"`
}

type Tool struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Icon             string  `json:"icon"`
	Required         bool    `json:"required"`
	Status           Status  `json:"status"`
	Installed        bool    `json:"installed"`
	InstalledVersion string  `json:"installedVersion,omitempty"`
	LatestVersion    string  `json:"latestVersion,omitempty"`
	ExecutablePath   string  `json:"executablePath,omitempty"`
	Account          string  `json:"account,omitempty"`
	Action           string  `json:"action,omitempty"`
	ActionAvailable  bool    `json:"actionAvailable"`
	PackageManager   string  `json:"packageManager,omitempty"`
	Message          string  `json:"message,omitempty"`
	Checks           []Check `json:"checks"`
}

type Snapshot struct {
	Operational         bool      `json:"operational"`
	ActionsRecommended  int       `json:"actionsRecommended"`
	PackageManagerReady bool      `json:"packageManagerReady"`
	CheckedAt           time.Time `json:"checkedAt"`
	Tools               []Tool    `json:"tools"`
}

type definition struct {
	id          string
	name        string
	description string
	icon        string
	required    bool
	command     string
	versionArgs []string
	wingetID    string
	commonPaths []string
	parse       func(string) string
	installHint string
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)
type pathLookup func(string) (string, error)

type latestEntry struct {
	version string
	err     string
	at      time.Time
}

type Service struct {
	run       commandRunner
	lookPath  pathLookup
	now       func() time.Time
	mu        sync.Mutex
	latest    map[string]latestEntry
	operation sync.Mutex
	wingetMu  sync.Mutex
}

func NewService() *Service {
	return &Service{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		lookPath: exec.LookPath,
		now:      time.Now,
		latest:   make(map[string]latestEntry),
	}
}

func definitions() []definition {
	return []definition{
		{id: "codex", name: "Codex CLI", description: "Local AI runtime used by product-team agents.", icon: "smart_toy", required: true, command: "codex", versionArgs: []string{"--version"}, wingetID: "OpenAI.Codex", commonPaths: []string{`%LOCALAPPDATA%\Programs\OpenAI\Codex\bin\codex.exe`, `%LOCALAPPDATA%\Microsoft\WinGet\Links\codex.exe`}, parse: parseCodexVersion},
		{id: "claude-code", name: "Claude Code", description: "Optional fallback AI runtime that reuses the current local Claude Code login.", icon: "psychology_alt", command: "claude", versionArgs: []string{"--version"}, commonPaths: []string{`%APPDATA%\npm\claude.cmd`}, parse: parseClaudeVersion, installHint: "Install Claude Code with the official installer or run: npm install -g @anthropic-ai/claude-code"},
		{id: "github-copilot", name: "GitHub Copilot CLI", description: "Optional local agent runtime invoked through the authenticated GitHub CLI session.", icon: "hub", command: "gh", versionArgs: []string{"copilot", "--", "version"}, commonPaths: []string{`%ProgramFiles%\GitHub CLI\gh.exe`}, parse: parseCopilotVersion, installHint: "Install GitHub CLI and run gh auth login, then gh copilot -- login if your Copilot CLI requires it."},
		{id: "gh", name: "GitHub CLI", description: "Repository discovery, authentication, and GitHub operations.", icon: "hub", required: true, command: "gh", versionArgs: []string{"--version"}, wingetID: "GitHub.cli", commonPaths: []string{`%ProgramFiles%\GitHub CLI\gh.exe`}, parse: parseGHVersion},
		{id: "git", name: "Git", description: "Source control and local workspace operations.", icon: "account_tree", required: true, command: "git", versionArgs: []string{"--version"}, wingetID: "Git.Git", commonPaths: []string{`%ProgramFiles%\Git\cmd\git.exe`}, parse: parseGitVersion},
		{id: "node", name: "Node.js", description: "JavaScript and Angular project runtime.", icon: "javascript", command: "node", versionArgs: []string{"--version"}, wingetID: "OpenJS.NodeJS.LTS", commonPaths: []string{`%ProgramFiles%\nodejs\node.exe`}, parse: parseNodeVersion},
		{id: "go", name: "Go", description: "Backend build and test runtime.", icon: "code", command: "go", versionArgs: []string{"version"}, wingetID: "GoLang.Go", commonPaths: []string{`%ProgramFiles%\Go\bin\go.exe`}, parse: parseGoVersion},
	}
}

func (s *Service) Snapshot(ctx context.Context, refresh bool) Snapshot {
	defs := definitions()
	tools := make([]Tool, len(defs))
	var wg sync.WaitGroup
	for index, def := range defs {
		wg.Add(1)
		go func(index int, def definition) {
			defer wg.Done()
			tools[index] = s.inspect(ctx, def, refresh)
		}(index, def)
	}
	wg.Wait()

	operational := true
	actions := 0
	for _, tool := range tools {
		if tool.Required && !tool.Installed {
			operational = false
		}
		if tool.ActionAvailable {
			actions++
		}
	}
	_, wingetErr := s.lookPath("winget")
	return Snapshot{Operational: operational, ActionsRecommended: actions, PackageManagerReady: wingetErr == nil && runtime.GOOS == "windows", CheckedAt: s.now().UTC(), Tools: tools}
}

func (s *Service) Inspect(ctx context.Context, id string, refresh bool) (Tool, error) {
	for _, def := range definitions() {
		if def.id == id {
			return s.inspect(ctx, def, refresh), nil
		}
	}
	return Tool{}, fmt.Errorf("unsupported CLI tool %q", id)
}

func (s *Service) RunAction(ctx context.Context, id, action string) (Tool, error) {
	var selected *definition
	for _, candidate := range definitions() {
		if candidate.id == id {
			copy := candidate
			selected = &copy
			break
		}
	}
	if selected == nil {
		return Tool{}, fmt.Errorf("unsupported CLI tool %q", id)
	}
	if selected.wingetID == "" {
		if selected.installHint != "" {
			return Tool{}, fmt.Errorf("automatic setup is not available for %s. %s", selected.name, selected.installHint)
		}
		return Tool{}, fmt.Errorf("automatic setup is not available for %s", selected.name)
	}
	if runtime.GOOS != "windows" {
		return Tool{}, errors.New("automatic setup is currently available on Windows only")
	}
	if _, err := s.lookPath("winget"); err != nil {
		return Tool{}, errors.New("Windows Package Manager (winget) is required for automatic setup")
	}

	current := s.inspect(ctx, *selected, true)
	var verb string
	switch action {
	case "install":
		if current.Installed {
			return Tool{}, fmt.Errorf("%s is already installed", selected.name)
		}
		verb = "install"
	case "update":
		if !current.Installed {
			return Tool{}, fmt.Errorf("%s must be installed before it can be updated", selected.name)
		}
		if current.Status != StatusUpdateAvailable {
			return Tool{}, fmt.Errorf("%s is already up to date", selected.name)
		}
		if !current.ActionAvailable {
			return Tool{}, fmt.Errorf("%s", current.Message)
		}
		verb = "upgrade"
	default:
		return Tool{}, fmt.Errorf("unsupported tool action %q", action)
	}

	s.operation.Lock()
	defer s.operation.Unlock()
	var output []byte
	var err error
	if action == "update" && current.PackageManager == "nvm" {
		nvmPath, lookupErr := s.lookPath("nvm")
		if lookupErr != nil {
			return Tool{}, errors.New("Node.js is managed by NVM, but nvm.exe is not available in PATH")
		}
		output, err = s.run(ctx, nvmPath, "install", current.LatestVersion)
		if err == nil {
			output, err = s.run(ctx, nvmPath, "use", current.LatestVersion)
		}
	} else {
		args := []string{verb, "--id", selected.wingetID, "--exact", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}
		output, err = s.run(ctx, "winget", args...)
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 1000 {
			message = message[len(message)-1000:]
		}
		return Tool{}, fmt.Errorf("%s %s failed: %s", action, selected.name, message)
	}
	s.mu.Lock()
	delete(s.latest, selected.id)
	s.mu.Unlock()
	return s.inspect(ctx, *selected, true), nil
}

func (s *Service) inspect(ctx context.Context, def definition, refresh bool) Tool {
	tool := Tool{ID: def.id, Name: def.name, Description: def.description, Icon: def.icon, Required: def.required, Checks: make([]Check, 0, 4)}
	path, pathErr := s.findExecutable(def)
	tool.Installed = pathErr == nil
	tool.ExecutablePath = path
	if !tool.Installed {
		tool.Status = StatusNotInstalled
		tool.Message = "Command is not available in the service PATH."
		if def.installHint != "" {
			tool.Message = def.installHint
		}
		tool.Action = "install"
		tool.Checks = append(tool.Checks,
			Check{ID: "command", Label: "Command available", Detail: def.command + " is not available in PATH.", Outcome: "failed"},
			Check{ID: "version", Label: "Version check", Detail: "Install the tool before checking its version.", Outcome: "blocked"},
			Check{ID: "integration", Label: "App integration", Detail: "Waiting for the command to become available.", Outcome: "blocked"},
		)
	} else {
		versionContext, cancel := context.WithTimeout(ctx, 8*time.Second)
		output, err := s.run(versionContext, path, def.versionArgs...)
		cancel()
		tool.InstalledVersion = def.parse(string(output))
		tool.Checks = append(tool.Checks, Check{ID: "command", Label: "Command available", Detail: path + " is accessible in the service PATH.", Outcome: "passed"})
		if err != nil || tool.InstalledVersion == "" {
			tool.Status = StatusCheckFailed
			tool.Message = "The command was found, but its version could not be read."
			tool.Checks = append(tool.Checks, Check{ID: "version", Label: "Version check", Detail: tool.Message, Outcome: "failed"})
		} else {
			tool.Status = StatusReady
		}
	}

	if def.wingetID == "" {
		if tool.Installed && tool.Status != StatusCheckFailed {
			tool.Checks = append(tool.Checks, Check{ID: "version", Label: "Version check", Detail: "Installed version " + tool.InstalledVersion + ". Latest-version checks are managed by the tool installer.", Outcome: "passed"})
		}
	} else {
		latest, latestErr := s.latestVersion(ctx, def, refresh)
		tool.LatestVersion = latest
		if tool.Installed && tool.Status != StatusCheckFailed {
			if latestErr != nil {
				tool.Checks = append(tool.Checks, Check{ID: "version", Label: "Version check", Detail: "Installed " + tool.InstalledVersion + ". Latest version is temporarily unavailable.", Outcome: "warning"})
			} else if isNewer(latest, tool.InstalledVersion) {
				tool.Status = StatusUpdateAvailable
				tool.Action = "update"
				tool.Message = "A newer version is available."
				tool.Checks = append(tool.Checks, Check{ID: "version", Label: "Version check", Detail: "Version " + latest + " is available.", Outcome: "warning"})
			} else {
				tool.Checks = append(tool.Checks, Check{ID: "version", Label: "Version check", Detail: "Installed version " + tool.InstalledVersion + " is current.", Outcome: "passed"})
			}
		}
	}

	if def.id == "claude-code" && tool.Installed {
		tool.Checks = append(tool.Checks, Check{ID: "authentication", Label: "Local session", Detail: "ProductCrew will reuse the Claude Code login owned by this Windows user when fallback execution is enabled.", Outcome: "warning"})
	}
	if def.id == "github-copilot" && tool.Installed {
		tool.Checks = append(tool.Checks, Check{ID: "authentication", Label: "Local session", Detail: "ProductCrew will invoke GitHub Copilot through the current GitHub CLI login for this Windows user.", Outcome: "warning"})
	}
	if def.id == "gh" && tool.Installed {
		account, err := s.githubAccount(ctx, tool.ExecutablePath)
		if err != nil {
			tool.Checks = append(tool.Checks, Check{ID: "authentication", Label: "Authentication", Detail: "GitHub CLI is not authenticated.", Outcome: "warning"})
		} else {
			tool.Account = account
			tool.Checks = append(tool.Checks, Check{ID: "authentication", Label: "Authentication", Detail: "Connected as " + account + ".", Outcome: "passed"})
		}
	}
	if tool.Installed {
		tool.Checks = append(tool.Checks, Check{ID: "integration", Label: "App integration", Detail: "ProductCrew can invoke " + def.name + ".", Outcome: "passed"})
	}
	if tool.Action != "" {
		_, wingetErr := s.lookPath("winget")
		if def.wingetID == "" {
			tool.PackageManager = "external"
			tool.ActionAvailable = false
			if def.installHint != "" {
				tool.Message = def.installHint
			}
		} else if !tool.Installed {
			tool.PackageManager = "winget"
			tool.ActionAvailable = wingetErr == nil && runtime.GOOS == "windows"
		} else if def.id == "node" {
			if _, nvmErr := s.lookPath("nvm"); nvmErr == nil && strings.Contains(strings.ToLower(tool.ExecutablePath), "nvm") {
				tool.PackageManager = "nvm"
				tool.ActionAvailable = true
			} else {
				tool.PackageManager = "winget"
				tool.ActionAvailable = wingetErr == nil && runtime.GOOS == "windows" && s.wingetManaged(ctx, def)
			}
		} else {
			tool.PackageManager = "winget"
			tool.ActionAvailable = wingetErr == nil && runtime.GOOS == "windows" && s.wingetManaged(ctx, def)
		}
		if !tool.ActionAvailable && wingetErr != nil {
			tool.Message = "Install Windows Package Manager to enable automatic setup."
		} else if !tool.ActionAvailable && tool.Installed {
			tool.PackageManager = "external"
			tool.Message = def.name + " is managed by another application. Update it from that application's installer."
		}
	}
	return tool
}

func (s *Service) wingetManaged(ctx context.Context, def definition) bool {
	s.wingetMu.Lock()
	defer s.wingetMu.Unlock()
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := s.run(checkContext, "winget", "list", "--id", def.wingetID, "--exact", "--accept-source-agreements", "--disable-interactivity")
	return err == nil && strings.Contains(strings.ToLower(string(output)), strings.ToLower(def.wingetID))
}

func (s *Service) findExecutable(def definition) (string, error) {
	if path, err := s.lookPath(def.command); err == nil {
		return path, nil
	}
	for _, candidate := range def.commonPaths {
		expanded := os.ExpandEnv(candidate)
		info, err := os.Stat(expanded)
		if err == nil && !info.IsDir() {
			absolute, absoluteErr := filepath.Abs(expanded)
			if absoluteErr == nil {
				return absolute, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func (s *Service) latestVersion(ctx context.Context, def definition, refresh bool) (string, error) {
	s.mu.Lock()
	cached, found := s.latest[def.id]
	cacheTTL := latestVersionCacheTTL
	if cached.err != "" {
		cacheTTL = 30 * time.Second
	}
	if found && !refresh && s.now().Sub(cached.at) < cacheTTL {
		s.mu.Unlock()
		if cached.err != "" {
			return cached.version, errors.New(cached.err)
		}
		return cached.version, nil
	}
	s.mu.Unlock()
	if runtime.GOOS != "windows" {
		return "", errors.New("winget version checks are available on Windows only")
	}
	if _, err := s.lookPath("winget"); err != nil {
		return "", err
	}
	s.wingetMu.Lock()
	defer s.wingetMu.Unlock()
	checkContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := s.run(checkContext, "winget", "show", "--id", def.wingetID, "--exact", "--accept-source-agreements", "--disable-interactivity")
	version := parseWingetVersion(string(output))
	if err == nil && version == "" {
		err = errors.New("winget response did not include a version")
	}
	if err != nil && found && cached.version != "" {
		return cached.version, nil
	}
	entry := latestEntry{version: version, at: s.now()}
	if err != nil {
		entry.err = err.Error()
	}
	s.mu.Lock()
	s.latest[def.id] = entry
	s.mu.Unlock()
	return version, err
}

func (s *Service) githubAccount(ctx context.Context, executablePath string) (string, error) {
	checkContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := s.run(checkContext, executablePath, "auth", "status", "--json", "hosts")
	if err != nil {
		return "", err
	}
	var payload struct {
		Hosts map[string][]struct {
			Login  string `json:"login"`
			Active bool   `json:"active"`
			State  string `json:"state"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return "", err
	}
	for _, accounts := range payload.Hosts {
		for _, account := range accounts {
			if account.Active && account.State == "success" && account.Login != "" {
				return account.Login, nil
			}
		}
	}
	return "", errors.New("no active GitHub account")
}

var numericVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

func parseCodexVersion(value string) string  { return firstVersion(value) }
func parseClaudeVersion(value string) string { return firstVersion(value) }
func parseCopilotVersion(value string) string {
	return firstVersion(value)
}
func parseGHVersion(value string) string { return firstVersion(value) }
func parseGitVersion(value string) string {
	return firstVersion(strings.ReplaceAll(value, ".windows.", "."))
}
func parseNodeVersion(value string) string { return firstVersion(value) }
func parseGoVersion(value string) string   { return firstVersion(value) }

func parseWingetVersion(value string) string {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "version") {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func firstVersion(value string) string {
	return numericVersionPattern.FindString(value)
}

func isNewer(latest, installed string) bool {
	left := numericParts(latest)
	right := numericParts(strings.ReplaceAll(installed, ".windows.", "."))
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for len(left) < length {
		left = append(left, 0)
	}
	for len(right) < length {
		right = append(right, 0)
	}
	for index := 0; index < length; index++ {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}

func numericParts(value string) []int {
	matches := regexp.MustCompile(`\d+`).FindAllString(value, -1)
	parts := make([]int, 0, len(matches))
	for _, match := range matches {
		number, _ := strconv.Atoi(match)
		parts = append(parts, number)
	}
	return parts
}
