package deploy

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

const detectorVersion = "deploy-detector-v1"

var (
	deployNamePattern = regexp.MustCompile(`(?i)(deploy|publish|ship|release:deploy)`)
	makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*(?:\s+[A-Za-z0-9][A-Za-z0-9_.-]*)*):(?:\s|$)`)
)

// Detector scans a project for deploy-like commands in package.json and
// Makefile. It never fabricates commands and never auto-selects a default:
// SelectedActionID is always left empty, so the resulting profile always
// carries StatusNotConfigured until the user explicitly saves a selection.
type Detector struct{}

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
			actions = append(actions, packageDeployActions(data, relative, workingDir)...)
		case "makefile", "gnumakefile":
			actions = append(actions, makeDeployActions(data, relative, workingDir)...)
		}
	}
	actions = deduplicateActions(actions)
	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Confidence != actions[j].Confidence {
			return actions[i].Confidence > actions[j].Confidence
		}
		return actions[i].Label < actions[j].Label
	})
	profile := Profile{
		ProjectID:   projectID,
		ProjectName: projectName,
		ProjectPath: root,
		Status:      StatusNotConfigured,
		Fingerprint: hex.EncodeToString(hash.Sum(nil)),
		Detector:    detectorVersion,
		ConfigFiles: configFiles,
		Actions:     actions,
		DetectedAt:  time.Now().UTC(),
	}
	if len(actions) == 0 {
		profile.Summary = "No supported deploy command was found."
		return profile, nil
	}
	profile.Summary = fmt.Sprintf("ProductCrew detected %d candidate deploy command(s). Choose the command ProductCrew should expose as the Deploy action.", len(actions))
	return profile, nil
}

func discoverConfigFiles(ctx context.Context, root string) ([]string, error) {
	names := map[string]bool{"package.json": true, "makefile": true, "gnumakefile": true}
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

func packageDeployActions(data []byte, source, workingDir string) []Action {
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
		if !deployNamePattern.MatchString(name) {
			continue
		}
		actions = append(actions, Action{
			ID:          actionID(source, "npm-"+name),
			Label:       "npm run " + name,
			Description: truncate(script, 120),
			Executable:  "npm",
			Arguments:   []string{"run", name},
			WorkingDir:  workingDir,
			Source:      source,
			Confidence:  deployConfidence(name),
		})
	}
	return actions
}

func makeDeployActions(data []byte, source, workingDir string) []Action {
	seen := map[string]bool{}
	var actions []Action
	for _, line := range strings.Split(string(data), "\n") {
		match := makeTargetPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 2 {
			continue
		}
		for _, target := range strings.Fields(match[1]) {
			if seen[target] || !deployNamePattern.MatchString(target) {
				continue
			}
			seen[target] = true
			actions = append(actions, Action{
				ID:          actionID(source, "make-"+target),
				Label:       "make " + target,
				Description: "Run the " + target + " target from " + source + ".",
				Executable:  "make",
				Arguments:   []string{target},
				WorkingDir:  workingDir,
				Source:      source,
				Confidence:  deployConfidence(target),
			})
		}
	}
	return actions
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
	return result
}

func actionID(source, name string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + name))
	return "deploy-" + hex.EncodeToString(sum[:6])
}

func deployConfidence(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "deploy":
		return 95
	case strings.HasPrefix(name, "deploy:") || strings.HasSuffix(name, ":deploy"):
		return 90
	case name == "publish" || strings.Contains(name, "publish"):
		return 82
	case name == "release" || strings.Contains(name, "release"):
		return 78
	case strings.Contains(name, "ship"):
		return 74
	default:
		return 65
	}
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}
