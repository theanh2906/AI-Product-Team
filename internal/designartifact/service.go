package designartifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	RootDirectory = ".productcrew/design-artifacts"
	defaultWidth  = 1440
	defaultHeight = 900
	maxSourceSize = 5 << 20
	maxSources    = 12
)

type Artifact struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

type Manifest struct {
	TaskID      string     `json:"taskId"`
	GeneratedAt time.Time  `json:"generatedAt"`
	Artifacts   []Artifact `json:"artifacts"`
}

type Service struct {
	findRenderer func() string
	runRenderer  func(context.Context, string, string, string) error
}

func NewService() *Service {
	service := &Service{}
	service.findRenderer = findEdge
	service.runRenderer = service.renderWithEdge
	return service
}

func TaskDirectory(projectPath, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || filepath.Base(taskID) != taskID || taskID == "." || taskID == ".." {
		return "", fmt.Errorf("invalid design artifact task id")
	}
	return filepath.Join(projectPath, filepath.FromSlash(RootDirectory), taskID), nil
}

// Process validates Designer output and renders HTML/SVG sources to PNG.
// Missing mockups are returned as warnings so a textual handoff can still complete.
func (s *Service) Process(ctx context.Context, projectPath, taskID string) ([]Artifact, []string, error) {
	directory, err := TaskDirectory(projectPath, taskID)
	if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, []string{"Designer did not create a visual mockup."}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read design artifact directory: %w", err)
	}

	sources := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".html" && extension != ".svg" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, nil, fmt.Errorf("inspect design source %s: %w", entry.Name(), infoErr)
		}
		if info.Size() > maxSourceSize {
			return nil, nil, fmt.Errorf("design source %s exceeds the 5 MB limit", entry.Name())
		}
		source := filepath.Join(directory, entry.Name())
		sources = append(sources, source)
	}
	sort.Strings(sources)
	if len(sources) > maxSources {
		return nil, nil, fmt.Errorf("design task has %d mockup sources; maximum is %d", len(sources), maxSources)
	}

	warnings := make([]string, 0)
	renderer := s.findRenderer()
	for _, source := range sources {
		output := strings.TrimSuffix(source, filepath.Ext(source)) + ".png"
		if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("clear stale render %s: %w", filepath.Base(output), err)
		}
		if renderer == "" {
			warnings = append(warnings, "Microsoft Edge was not found; "+filepath.Base(source)+" could not be rendered to PNG.")
			continue
		}
		renderContext, cancel := context.WithTimeout(ctx, 60*time.Second)
		renderErr := s.runRenderer(renderContext, renderer, source, output)
		cancel()
		if renderErr != nil {
			warnings = append(warnings, fmt.Sprintf("Could not render %s: %v", filepath.Base(source), renderErr))
		}
	}

	artifacts, err := collectPNGs(projectPath, directory)
	if err != nil {
		return nil, nil, err
	}
	if len(artifacts) == 0 && len(warnings) == 0 {
		warnings = append(warnings, "Designer did not create a PNG, HTML, or SVG mockup.")
	}
	manifest := Manifest{TaskID: taskID, GeneratedAt: time.Now().UTC(), Artifacts: artifacts}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("encode design artifact manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), append(data, '\n'), 0o600); err != nil {
		return nil, nil, fmt.Errorf("write design artifact manifest: %w", err)
	}
	return artifacts, warnings, nil
}

func collectPNGs(projectPath, directory string) ([]Artifact, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read rendered design artifacts: %w", err)
	}
	artifacts := make([]Artifact, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect rendered artifact %s: %w", entry.Name(), err)
		}
		if info.Size() == 0 || info.Size() > maxSourceSize {
			return nil, fmt.Errorf("rendered artifact %s is empty or exceeds the 5 MB limit", entry.Name())
		}
		absolute := filepath.Join(directory, entry.Name())
		file, err := os.Open(absolute)
		if err != nil {
			return nil, fmt.Errorf("open rendered artifact %s: %w", entry.Name(), err)
		}
		imageConfig, decodeErr := png.DecodeConfig(file)
		_ = file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("rendered artifact %s is not a valid PNG: %w", entry.Name(), decodeErr)
		}
		relative, err := filepath.Rel(projectPath, absolute)
		if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
			return nil, fmt.Errorf("rendered artifact resolved outside the project")
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		hash := sha256.Sum256([]byte(filepath.ToSlash(relative)))
		artifacts = append(artifacts, Artifact{
			ID: "artifact-" + hex.EncodeToString(hash[:6]), Title: humanTitle(name), Kind: "mockup",
			RelativePath: filepath.ToSlash(relative), MediaType: mime.TypeByExtension(".png"),
			Width: imageConfig.Width, Height: imageConfig.Height,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].RelativePath < artifacts[j].RelativePath })
	return artifacts, nil
}

func (s *Service) renderWithEdge(ctx context.Context, renderer, source, output string) error {
	profile, err := os.MkdirTemp("", "productcrew-render-*")
	if err != nil {
		return fmt.Errorf("create renderer profile: %w", err)
	}
	defer os.RemoveAll(profile)
	urlPath := filepath.ToSlash(source)
	if runtime.GOOS == "windows" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	location := (&url.URL{Scheme: "file", Path: urlPath}).String()
	command := exec.CommandContext(ctx, renderer,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--disable-extensions", "--disable-background-networking",
		"--host-resolver-rules=MAP * 0.0.0.0",
		"--allow-file-access-from-files", "--user-data-dir="+profile,
		fmt.Sprintf("--window-size=%d,%d", defaultWidth, defaultHeight), "--screenshot="+output, location,
	)
	if outputBytes, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Edge renderer failed: %w: %s", err, truncate(strings.TrimSpace(string(outputBytes)), 800))
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		return fmt.Errorf("Edge renderer did not produce a PNG")
	}
	return nil
}

func findEdge() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if path, err := exec.LookPath("msedge.exe"); err == nil {
		return path
	}
	candidates := []string{
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func humanTitle(value string) string {
	value = strings.NewReplacer("-", " ", "_", " ").Replace(strings.TrimSpace(value))
	if value == "" {
		return "Design mockup"
	}
	words := strings.Fields(value)
	for index := range words {
		words[index] = strings.ToUpper(words[index][:1]) + words[index][1:]
	}
	return strings.Join(words, " ")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}
