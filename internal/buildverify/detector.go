package buildverify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const detectorVersion = "build-detector-v2"

var makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*(?:\s+[A-Za-z0-9][A-Za-z0-9_.-]*)*):(?:\s|$)`)

type Detector struct{ Advisor Advisor }

func (d Detector) Detect(ctx context.Context, projectID, projectName, projectPath string) (Profile, error) {
	root, err := filepath.Abs(projectPath)
	if err != nil {
		return Profile{}, fmt.Errorf("resolve project path: %w", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return Profile{}, fmt.Errorf("project folder is unavailable")
	}
	files, err := discoverConfigFiles(ctx, root)
	if err != nil {
		return Profile{}, err
	}
	actions := make([]Action, 0)
	configFiles := make([]string, 0, len(files))
	hash := sha256.New()
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return Profile{}, err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		configFiles = append(configFiles, relative)
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write(data)
		workingDir := filepath.ToSlash(filepath.Dir(relative))
		if workingDir == "." {
			workingDir = ""
		}
		switch strings.ToLower(filepath.Base(path)) {
		case "package.json":
			actions = append(actions, packageActions(data, relative, workingDir)...)
		case "makefile", "gnumakefile":
			actions = append(actions, makeActions(data, relative, workingDir)...)
		case "go.mod":
			actions = append(actions,
				Action{ID: actionID(relative, "go-test"), Label: "Go test suite", Description: "Run all Go package tests.", Executable: "go", Arguments: []string{"test", "./..."}, WorkingDir: workingDir, Source: relative, Confidence: 86},
				Action{ID: actionID(relative, "go-build"), Label: "Go build", Description: "Compile all Go packages.", Executable: "go", Arguments: []string{"build", "./..."}, WorkingDir: workingDir, Source: relative, Confidence: 82},
			)
		case "cargo.toml":
			actions = append(actions, Action{ID: actionID(relative, "cargo-check"), Label: "Cargo check", Description: "Type-check the Rust workspace.", Executable: "cargo", Arguments: []string{"check"}, WorkingDir: workingDir, Source: relative, Confidence: 84})
		}
	}
	actions = deduplicateActions(actions)
	profile := Profile{ProjectID: projectID, ProjectName: projectName, ProjectPath: root, Status: "ready", Fingerprint: hex.EncodeToString(hash.Sum(nil)), Detector: detectorVersion, ConfigFiles: configFiles, Actions: actions, DetectedAt: time.Now().UTC()}
	if len(actions) == 0 {
		profile.Status = "not_configured"
		profile.Summary = "No supported build command was found."
		return profile, nil
	}
	recommendedID, summary := deterministicRecommendation(actions)
	for index := range actions {
		actions[index].Recommended = actions[index].ID == recommendedID
	}
	profile.Actions = actions
	profile.Summary = summary
	return profile, nil
}

func discoverConfigFiles(ctx context.Context, root string) ([]string, error) {
	names := map[string]bool{"package.json": true, "makefile": true, "gnumakefile": true, "go.mod": true, "cargo.toml": true}
	excluded := map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, "target": true, ".angular": true, ".productcrew": true}
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filepath.SkipDir
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		depth := 0
		if relative != "." {
			depth = len(strings.Split(filepath.ToSlash(relative), "/"))
		}
		if entry.IsDir() {
			if path != root && (excluded[strings.ToLower(entry.Name())] || depth > 3) {
				return filepath.SkipDir
			}
			return nil
		}
		if names[strings.ToLower(entry.Name())] {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func packageActions(data []byte, source, workingDir string) []Action {
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	actions := make([]Action, 0)
	for name, script := range manifest.Scripts {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n") {
			continue
		}
		actions = append(actions, Action{ID: actionID(source, "npm-"+name), Label: "npm run " + name, Description: truncate(script, 120), Executable: "npm", Arguments: []string{"run", name}, WorkingDir: workingDir, Source: source, Confidence: scriptConfidence(name)})
	}
	return actions
}

func makeActions(data []byte, source, workingDir string) []Action {
	seen := map[string]bool{}
	var actions []Action
	for _, line := range strings.Split(string(data), "\n") {
		match := makeTargetPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 2 {
			continue
		}
		for _, target := range strings.Fields(match[1]) {
			if seen[target] {
				continue
			}
			seen[target] = true
			actions = append(actions, Action{ID: actionID(source, "make-"+target), Label: "make " + target, Description: "Run the " + target + " target from " + source + ".", Executable: "make", Arguments: []string{target}, WorkingDir: workingDir, Source: source, Confidence: scriptConfidence(target)})
		}
	}
	return actions
}

func deterministicRecommendation(actions []Action) (string, string) {
	best := actions[0]
	for _, action := range actions[1:] {
		if action.Confidence > best.Confidence {
			best = action
		}
	}
	return best.ID, "ProductCrew detected " + fmt.Sprintf("%d", len(actions)) + " local build action(s). Choose the command ProductCrew should expose on the Work Board."
}

func deduplicateActions(actions []Action) []Action {
	seen := map[string]bool{}
	result := make([]Action, 0, len(actions))
	for _, action := range actions {
		key := action.WorkingDir + "\x00" + action.Executable + "\x00" + strings.Join(action.Arguments, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, action)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Confidence != result[j].Confidence {
			return result[i].Confidence > result[j].Confidence
		}
		return result[i].Label < result[j].Label
	})
	return result
}

func actionID(source, name string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + name))
	return "build-" + hex.EncodeToString(sum[:6])
}
func containsAction(actions []Action, id string) bool {
	for _, action := range actions {
		if action.ID == id {
			return true
		}
	}
	return false
}

func scriptConfidence(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "installer-update" || strings.Contains(name, "build:exe:update") || strings.Contains(name, "build:installer:update"):
		return 100
	case name == "update" || strings.HasSuffix(name, ":update"):
		return 97
	case name == "release":
		return 95
	case name == "check" || name == "verify" || name == "ci":
		return 92
	case name == "test" || strings.Contains(name, "test"):
		return 86
	case name == "build" || strings.Contains(name, "build"):
		return 84
	case name == "typecheck" || strings.Contains(name, "typecheck"):
		return 78
	case name == "lint" || strings.Contains(name, "lint"):
		return 72
	default:
		return 60
	}
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}
