package sourceguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	RecoveryDirectoryName = ".productcrew"
	maxSnapshotFileBytes  = 1 << 20
	maxSnapshotTotalBytes = 100 << 20
	maxSnapshotFiles      = 12000
	minProtectedBytes     = 1024
	maxPlaceholderBytes   = 128
)

var excludedDirectories = map[string]bool{
	".angular": true, ".cache": true, ".git": true, ".next": true, ".nuxt": true,
	".output": true, ".pnpm-store": true, ".productcrew": true, ".tmp": true,
	".turbo": true, ".venv": true, "bin": true, "build": true, "coverage": true,
	"dist": true, "node_modules": true, "out": true, "release": true, "target": true,
	"tmp": true, "vendor": true,
}

var sourceExtensions = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".cs": true, ".css": true, ".go": true,
	".h": true, ".hpp": true, ".html": true, ".java": true, ".js": true,
	".json": true, ".jsx": true, ".kt": true, ".less": true, ".mjs": true,
	".rs": true, ".scss": true, ".sh": true, ".sql": true, ".svelte": true,
	".swift": true, ".toml": true, ".ts": true, ".tsx": true, ".vue": true,
	".xml": true, ".yaml": true, ".yml": true,
}

var sourceFileNames = map[string]bool{
	".babelrc": true, ".dockerignore": true, ".env.example": true, ".eslintrc": true,
	".gitignore": true, "Cargo.toml": true, "Dockerfile": true, "go.mod": true,
	"go.sum": true, "Makefile": true, "package-lock.json": true, "package.json": true,
	"pnpm-lock.yaml": true, "tsconfig.json": true, "vite.config.ts": true,
	"yarn.lock": true,
}

type Guard struct {
	recoveryDir string
	roots       []rootSnapshot
}

type rootSnapshot struct {
	Index int    `json:"index"`
	Path  string `json:"path"`
	Files []File `json:"files"`
}

type File struct {
	RelativePath string `json:"relativePath"`
	BackupPath   string `json:"backupPath"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	TaskID        string         `json:"taskId"`
	Role          string         `json:"role"`
	CreatedAt     time.Time      `json:"createdAt"`
	Roots         []rootSnapshot `json:"roots"`
	Skipped       []string       `json:"skipped,omitempty"`
}

type Report struct {
	RecoveryDir string         `json:"recoveryDir"`
	Restored    []RestoredFile `json:"restored"`
}

type RestoredFile struct {
	RootPath     string `json:"rootPath"`
	RelativePath string `json:"relativePath"`
	BeforeSize   int64  `json:"beforeSize"`
	AfterSize    int64  `json:"afterSize"`
	Reason       string `json:"reason"`
}

func Begin(ctx context.Context, projectPath string, workspaceRoots []string, taskID, role string) (*Guard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("project path is required")
	}
	primary, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	roots := uniqueAbsoluteRoots(append([]string{primary}, workspaceRoots...))
	if len(roots) == 0 {
		return nil, fmt.Errorf("no workspace roots available for source guard")
	}
	recoveryDir := filepath.Join(primary, RecoveryDirectoryName, "recovery", safeName(taskID)+"-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(recoveryDir, 0o700); err != nil {
		return nil, fmt.Errorf("create source recovery snapshot: %w", err)
	}
	manifest := Manifest{SchemaVersion: 1, TaskID: taskID, Role: role, CreatedAt: time.Now().UTC()}
	var totalBytes int64
	var totalFiles int
	for index, root := range roots {
		snapshot := rootSnapshot{Index: index, Path: root}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				manifest.Skipped = append(manifest.Skipped, fmt.Sprintf("%s: %v", path, walkErr))
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path != root && entry.IsDir() && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !isSourceLike(entry.Name()) {
				return nil
			}
			if totalFiles >= maxSnapshotFiles || totalBytes >= maxSnapshotTotalBytes {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				manifest.Skipped = append(manifest.Skipped, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			if info.Size() < 0 || info.Size() > maxSnapshotFileBytes {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil || !safeRelativePath(relative) {
				manifest.Skipped = append(manifest.Skipped, path+": unsafe relative path")
				return nil
			}
			backupPath := filepath.Join(recoveryDir, fmt.Sprintf("root-%02d", index), "files", relative)
			hash, err := copyFileWithHash(path, backupPath)
			if err != nil {
				manifest.Skipped = append(manifest.Skipped, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			snapshot.Files = append(snapshot.Files, File{RelativePath: filepath.ToSlash(relative), BackupPath: backupPath, Size: info.Size(), SHA256: hash})
			totalBytes += info.Size()
			totalFiles++
			return nil
		})
		if err != nil {
			_ = os.RemoveAll(recoveryDir)
			return nil, fmt.Errorf("create source recovery snapshot: %w", err)
		}
		sort.Slice(snapshot.Files, func(left, right int) bool {
			return snapshot.Files[left].RelativePath < snapshot.Files[right].RelativePath
		})
		manifest.Roots = append(manifest.Roots, snapshot)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.RemoveAll(recoveryDir)
		return nil, fmt.Errorf("encode source recovery manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(recoveryDir, "manifest.json"), data, 0o600); err != nil {
		_ = os.RemoveAll(recoveryDir)
		return nil, fmt.Errorf("write source recovery manifest: %w", err)
	}
	return &Guard{recoveryDir: recoveryDir, roots: manifest.Roots}, nil
}

func (g *Guard) VerifyAndRestore(ctx context.Context) (Report, error) {
	if g == nil {
		return Report{}, nil
	}
	report := Report{RecoveryDir: g.recoveryDir}
	for _, root := range g.roots {
		for _, before := range root.Files {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			currentPath := filepath.Join(root.Path, filepath.FromSlash(before.RelativePath))
			afterData, err := os.ReadFile(currentPath)
			if err != nil {
				continue
			}
			reason, suspicious := suspiciousRewrite(before.Size, afterData)
			if !suspicious {
				continue
			}
			if err := restoreFile(before.BackupPath, currentPath); err != nil {
				return report, fmt.Errorf("restore suspicious source rewrite %s: %w", before.RelativePath, err)
			}
			report.Restored = append(report.Restored, RestoredFile{
				RootPath: root.Path, RelativePath: before.RelativePath, BeforeSize: before.Size,
				AfterSize: int64(len(afterData)), Reason: reason,
			})
		}
	}
	if len(report.Restored) == 0 {
		_ = os.RemoveAll(g.recoveryDir)
		return report, nil
	}
	return report, reportError(report)
}

func reportError(report Report) error {
	files := make([]string, 0, len(report.Restored))
	for _, item := range report.Restored {
		files = append(files, fmt.Sprintf("%s (%dB -> %dB, %s)", item.RelativePath, item.BeforeSize, item.AfterSize, item.Reason))
	}
	sort.Strings(files)
	return fmt.Errorf("workspace safety guard restored suspicious source rewrite(s): %s. Recovery snapshot kept at %s", strings.Join(files, "; "), report.RecoveryDir)
}

func uniqueAbsoluteRoots(paths []string) []string {
	seen := map[string]bool{}
	roots := make([]string, 0, len(paths))
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		absolute, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, absolute)
	}
	return roots
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "task"
	}
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return "task"
	}
	return builder.String()
}

func shouldSkipDirectory(name string) bool {
	return excludedDirectories[strings.TrimSpace(name)]
}

func isSourceLike(name string) bool {
	if sourceFileNames[name] {
		return true
	}
	return sourceExtensions[strings.ToLower(filepath.Ext(name))]
}

func safeRelativePath(path string) bool {
	if filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(path)
	return cleaned != "." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) && cleaned != ".."
}

func copyFileWithHash(source, destination string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer output.Close()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(output, hash), input); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func restoreFile(backupPath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return err
	}
	input, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func suspiciousRewrite(beforeSize int64, after []byte) (string, bool) {
	if beforeSize < minProtectedBytes || int64(len(after)) > maxPlaceholderBytes {
		return "", false
	}
	trimmed := strings.TrimSpace(string(after))
	if trimmed == "" {
		return "empty source file", true
	}
	normalized := strings.ToLower(strings.Trim(trimmed, "`'\"; \t\r\n"))
	normalized = strings.TrimPrefix(normalized, "//")
	normalized = strings.TrimSpace(strings.Trim(normalized, "/* "))
	switch normalized {
	case "placeholder", "todo", "tbd", "stub":
		return "placeholder content", true
	}
	return "", false
}
