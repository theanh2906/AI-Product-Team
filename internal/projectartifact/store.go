package projectartifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	RootDirectory          = ".productcrew"
	RequestDirectory       = RootDirectory + "/requests"
	InsightDirectory       = RootDirectory + "/insights"
	TaskDirectory          = RootDirectory + "/tasks"
	MaxAttachments         = 12
	MaxAttachmentBytes     = 10 << 20
	MaxRequestUploadBytes  = 50 << 20
	requestManifestVersion = 1
	insightManifestVersion = 1
)

var allowedExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
	".pdf": true, ".txt": true, ".md": true, ".json": true,
}

type Upload struct {
	Name      string
	MediaType string
	Size      int64
	Open      func() (io.ReadCloser, error)
}

type RequestInput struct {
	RequestID       string
	ProjectID       string
	Title           string
	Description     string
	Source          string
	SourceReference string
	CreatedAt       time.Time
	Attachments     []Upload
}

type Attachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"originalName"`
	StoredName   string `json:"storedName"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type RequestManifest struct {
	SchemaVersion   int          `json:"schemaVersion"`
	RequestID       string       `json:"requestId"`
	ProjectID       string       `json:"projectId"`
	Title           string       `json:"title"`
	Source          string       `json:"source"`
	SourceReference string       `json:"sourceReference,omitempty"`
	DescriptionPath string       `json:"descriptionPath"`
	Directory       string       `json:"directory"`
	Attachments     []Attachment `json:"attachments"`
	CreatedAt       time.Time    `json:"createdAt"`
}

type InsightManifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	RunID         string    `json:"runId"`
	ResultPath    string    `json:"resultPath"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type TaskReportInput struct {
	TaskID      string
	Key         string
	Role        string
	Title       string
	Status      string
	Summary     string
	CompletedAt time.Time
	Payload     any
}

type Store interface {
	SaveRequest(context.Context, string, RequestInput) (RequestManifest, error)
	LoadRequest(context.Context, string, string) (RequestManifest, error)
	DeleteRequest(context.Context, string, string) error
	OpenAttachment(context.Context, string, string, string) (Attachment, *os.File, error)
	SaveInsight(context.Context, string, string, string, any) (InsightManifest, error)
	SaveTaskReport(context.Context, string, TaskReportInput) (string, error)
}

type FilesystemStore struct{}

func NewFilesystemStore() *FilesystemStore { return &FilesystemStore{} }

func NewRequestID() string {
	var value [10]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "request-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("request-%x", time.Now().UnixNano())
}

func (s *FilesystemStore) SaveRequest(ctx context.Context, projectPath string, input RequestInput) (RequestManifest, error) {
	if err := ctx.Err(); err != nil {
		return RequestManifest{}, err
	}
	projectRoot, err := validateProjectRoot(projectPath)
	if err != nil {
		return RequestManifest{}, err
	}
	requestID, err := safeSegment(input.RequestID, "request id")
	if err != nil {
		return RequestManifest{}, err
	}
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	if title == "" || description == "" {
		return RequestManifest{}, fmt.Errorf("request title and description are required")
	}
	if len(input.Attachments) > MaxAttachments {
		return RequestManifest{}, fmt.Errorf("a request can contain at most %d attachments", MaxAttachments)
	}

	requestRoot := filepath.Join(projectRoot, filepath.FromSlash(RequestDirectory))
	if err := os.MkdirAll(requestRoot, 0o700); err != nil {
		return RequestManifest{}, fmt.Errorf("create project request storage: %w", err)
	}
	_ = ensureLocalGitExclude(projectRoot)
	temporary, err := os.MkdirTemp(requestRoot, ".request-*")
	if err != nil {
		return RequestManifest{}, fmt.Errorf("create temporary request storage: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()

	createdAt := input.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	directoryRelative := filepath.ToSlash(filepath.Join(RequestDirectory, requestID))
	descriptionRelative := filepath.ToSlash(filepath.Join(directoryRelative, "request.md"))
	manifest := RequestManifest{
		SchemaVersion: requestManifestVersion, RequestID: requestID, ProjectID: strings.TrimSpace(input.ProjectID),
		Title: title, Source: strings.TrimSpace(input.Source), SourceReference: strings.TrimSpace(input.SourceReference),
		DescriptionPath: descriptionRelative, Directory: directoryRelative, Attachments: []Attachment{}, CreatedAt: createdAt,
	}
	markdown := requestMarkdown(manifest, description)
	if err := os.WriteFile(filepath.Join(temporary, "request.md"), []byte(markdown), 0o600); err != nil {
		return RequestManifest{}, fmt.Errorf("write request description: %w", err)
	}

	if len(input.Attachments) > 0 {
		attachmentDirectory := filepath.Join(temporary, "attachments")
		if err := os.MkdirAll(attachmentDirectory, 0o700); err != nil {
			return RequestManifest{}, fmt.Errorf("create request attachment storage: %w", err)
		}
		var total int64
		for index, upload := range input.Attachments {
			if err := ctx.Err(); err != nil {
				return RequestManifest{}, err
			}
			attachment, written, err := saveAttachment(projectRoot, requestID, attachmentDirectory, index, upload)
			if err != nil {
				return RequestManifest{}, err
			}
			total += written
			if total > MaxRequestUploadBytes {
				return RequestManifest{}, fmt.Errorf("request attachments exceed the %d MB total limit", MaxRequestUploadBytes>>20)
			}
			manifest.Attachments = append(manifest.Attachments, attachment)
		}
	}
	if err := writeJSONFile(filepath.Join(temporary, "manifest.json"), manifest); err != nil {
		return RequestManifest{}, fmt.Errorf("write request manifest: %w", err)
	}
	target := filepath.Join(requestRoot, requestID)
	if _, err := os.Stat(target); err == nil {
		return RequestManifest{}, fmt.Errorf("request artifact %s already exists", requestID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return RequestManifest{}, fmt.Errorf("inspect request artifact: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		return RequestManifest{}, fmt.Errorf("publish request artifact: %w", err)
	}
	removeTemporary = false
	return manifest, nil
}

func (s *FilesystemStore) LoadRequest(ctx context.Context, projectPath, requestID string) (RequestManifest, error) {
	if err := ctx.Err(); err != nil {
		return RequestManifest{}, err
	}
	projectRoot, err := validateProjectRoot(projectPath)
	if err != nil {
		return RequestManifest{}, err
	}
	requestID, err = safeSegment(requestID, "request id")
	if err != nil {
		return RequestManifest{}, err
	}
	var manifest RequestManifest
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(RequestDirectory), requestID, "manifest.json"))
	if err != nil {
		return RequestManifest{}, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return RequestManifest{}, fmt.Errorf("decode request manifest: %w", err)
	}
	if manifest.SchemaVersion != requestManifestVersion || manifest.RequestID != requestID {
		return RequestManifest{}, fmt.Errorf("unsupported or mismatched request manifest")
	}
	if manifest.Attachments == nil {
		manifest.Attachments = []Attachment{}
	}
	return manifest, nil
}

func (s *FilesystemStore) DeleteRequest(ctx context.Context, projectPath, requestID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	projectRoot, err := validateProjectRoot(projectPath)
	if err != nil {
		return err
	}
	requestID, err = safeSegment(requestID, "request id")
	if err != nil {
		return err
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(RequestDirectory), requestID)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove request artifact: %w", err)
	}
	return nil
}

func (s *FilesystemStore) OpenAttachment(ctx context.Context, projectPath, requestID, attachmentID string) (Attachment, *os.File, error) {
	manifest, err := s.LoadRequest(ctx, projectPath, requestID)
	if err != nil {
		return Attachment{}, nil, err
	}
	attachmentID, err = safeSegment(attachmentID, "attachment id")
	if err != nil {
		return Attachment{}, nil, err
	}
	for _, attachment := range manifest.Attachments {
		if attachment.ID != attachmentID {
			continue
		}
		absolute, err := resolveWithinProject(projectPath, attachment.RelativePath)
		if err != nil {
			return Attachment{}, nil, err
		}
		file, err := os.Open(absolute)
		return attachment, file, err
	}
	return Attachment{}, nil, os.ErrNotExist
}

func (s *FilesystemStore) SaveInsight(ctx context.Context, projectPath, kind, runID string, value any) (InsightManifest, error) {
	if err := ctx.Err(); err != nil {
		return InsightManifest{}, err
	}
	projectRoot, err := validateProjectRoot(projectPath)
	if err != nil {
		return InsightManifest{}, err
	}
	kind, err = safeSegment(kind, "insight kind")
	if err != nil {
		return InsightManifest{}, err
	}
	runID, err = safeSegment(runID, "insight run id")
	if err != nil {
		return InsightManifest{}, err
	}
	directory := filepath.Join(projectRoot, filepath.FromSlash(InsightDirectory), kind, runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return InsightManifest{}, fmt.Errorf("create insight history directory: %w", err)
	}
	_ = ensureLocalGitExclude(projectRoot)
	resultPath := filepath.Join(directory, "result.json")
	if err := writeJSONAtomic(resultPath, value); err != nil {
		return InsightManifest{}, fmt.Errorf("write insight result: %w", err)
	}
	updatedAt := time.Now().UTC()
	manifest := InsightManifest{
		SchemaVersion: insightManifestVersion, Kind: kind, RunID: runID,
		ResultPath: filepath.ToSlash(filepath.Join(InsightDirectory, kind, runID, "result.json")), UpdatedAt: updatedAt,
	}
	if err := writeJSONAtomic(filepath.Join(directory, "manifest.json"), manifest); err != nil {
		return InsightManifest{}, fmt.Errorf("write insight manifest: %w", err)
	}
	latest := filepath.Join(projectRoot, filepath.FromSlash(InsightDirectory), kind, "latest.json")
	if err := writeJSONAtomic(latest, manifest); err != nil {
		return InsightManifest{}, fmt.Errorf("write latest insight pointer: %w", err)
	}
	return manifest, nil
}

func (s *FilesystemStore) SaveTaskReport(ctx context.Context, projectPath string, input TaskReportInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	projectRoot, err := validateProjectRoot(projectPath)
	if err != nil {
		return "", err
	}
	taskID, err := safeSegment(input.TaskID, "task id")
	if err != nil {
		return "", err
	}
	directory := filepath.Join(projectRoot, filepath.FromSlash(TaskDirectory), taskID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create task report directory: %w", err)
	}
	_ = ensureLocalGitExclude(projectRoot)
	if err := writeJSONAtomic(filepath.Join(directory, "delivery-report.json"), input.Payload); err != nil {
		return "", fmt.Errorf("write task delivery report: %w", err)
	}
	completedAt := input.CompletedAt.UTC()
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	markdown := strings.Join([]string{
		"# " + strings.TrimSpace(input.Key) + " " + strings.TrimSpace(input.Title), "",
		"- Task ID: `" + taskID + "`", "- Role: `" + strings.TrimSpace(input.Role) + "`",
		"- Status: `" + strings.TrimSpace(input.Status) + "`", "- Completed: `" + completedAt.Format(time.RFC3339) + "`",
		"", "## Summary", "", strings.TrimSpace(input.Summary), "",
	}, "\n")
	if err := os.WriteFile(filepath.Join(directory, "delivery-report.md"), []byte(markdown), 0o600); err != nil {
		return "", fmt.Errorf("write task delivery summary: %w", err)
	}
	return filepath.ToSlash(filepath.Join(TaskDirectory, taskID)), nil
}

func saveAttachment(projectRoot, requestID, directory string, index int, upload Upload) (Attachment, int64, error) {
	original := filepath.Base(strings.TrimSpace(upload.Name))
	if original == "" || original == "." {
		original = fmt.Sprintf("attachment-%02d", index+1)
	}
	extension := strings.ToLower(filepath.Ext(original))
	if !allowedExtensions[extension] {
		return Attachment{}, 0, fmt.Errorf("attachment %s uses an unsupported file type", original)
	}
	if upload.Size > MaxAttachmentBytes {
		return Attachment{}, 0, fmt.Errorf("attachment %s exceeds the %d MB limit", original, MaxAttachmentBytes>>20)
	}
	if upload.Open == nil {
		return Attachment{}, 0, fmt.Errorf("attachment %s cannot be opened", original)
	}
	source, err := upload.Open()
	if err != nil {
		return Attachment{}, 0, fmt.Errorf("open attachment %s: %w", original, err)
	}
	defer source.Close()
	storedName := fmt.Sprintf("attachment-%02d%s", index+1, extension)
	target := filepath.Join(directory, storedName)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Attachment{}, 0, fmt.Errorf("create attachment %s: %w", original, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, MaxAttachmentBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return Attachment{}, 0, fmt.Errorf("store attachment %s: %w", original, copyErr)
	}
	if closeErr != nil {
		return Attachment{}, 0, fmt.Errorf("close attachment %s: %w", original, closeErr)
	}
	if written > MaxAttachmentBytes {
		return Attachment{}, 0, fmt.Errorf("attachment %s exceeds the %d MB limit", original, MaxAttachmentBytes>>20)
	}
	mediaType, err := validateStoredAttachment(target, extension, strings.TrimSpace(upload.MediaType))
	if err != nil {
		return Attachment{}, 0, fmt.Errorf("validate attachment %s: %w", original, err)
	}
	relative := filepath.ToSlash(filepath.Join(RequestDirectory, requestID, "attachments", storedName))
	return Attachment{
		ID: fmt.Sprintf("attachment-%02d", index+1), OriginalName: original, StoredName: storedName,
		RelativePath: relative, MediaType: mediaType, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, written, nil
}

func validateStoredAttachment(path, extension, declared string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	detected := http.DetectContentType(buffer[:read])
	allowed := false
	switch extension {
	case ".png":
		allowed = detected == "image/png"
	case ".jpg", ".jpeg":
		allowed = detected == "image/jpeg"
	case ".webp":
		allowed = detected == "image/webp"
	case ".gif":
		allowed = detected == "image/gif"
	case ".pdf":
		allowed = detected == "application/pdf"
	case ".txt", ".md", ".json":
		allowed = strings.HasPrefix(detected, "text/plain") || detected == "application/json" || detected == "application/octet-stream"
	}
	if !allowed {
		return "", fmt.Errorf("content does not match the %s extension", extension)
	}
	if declared != "" && strings.HasPrefix(declared, "image/") && !strings.HasPrefix(detected, "image/") {
		return "", fmt.Errorf("declared media type does not match file content")
	}
	if extension == ".md" {
		return "text/markdown", nil
	}
	if extension == ".json" {
		return "application/json", nil
	}
	if detected == "application/octet-stream" {
		if inferred := mime.TypeByExtension(extension); inferred != "" {
			return inferred, nil
		}
	}
	return detected, nil
}

func validateProjectRoot(projectPath string) (string, error) {
	root, err := filepath.Abs(strings.TrimSpace(projectPath))
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project path is unavailable")
	}
	return root, nil
}

func safeSegment(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return "", fmt.Errorf("invalid %s", label)
	}
	return value, nil
}

func resolveWithinProject(projectPath, relativePath string) (string, error) {
	root, err := validateProjectRoot(projectPath)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("artifact path resolves outside the project")
	}
	return absolute, nil
}

func requestMarkdown(manifest RequestManifest, description string) string {
	lines := []string{
		"# " + manifest.Title, "", "## Request metadata", "",
		"- Request ID: `" + manifest.RequestID + "`",
		"- Project ID: `" + manifest.ProjectID + "`",
		"- Source: `" + manifest.Source + "`",
		"- Created: `" + manifest.CreatedAt.Format(time.RFC3339) + "`",
	}
	if manifest.SourceReference != "" {
		lines = append(lines, "- Source reference: `"+manifest.SourceReference+"`")
	}
	lines = append(lines, "", "## Description", "", description, "")
	return strings.Join(lines, "\n")
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifact-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		_, err = temporary.Write(append(data, '\n'))
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func ensureLocalGitExclude(projectRoot string) error {
	gitDirectory := filepath.Join(projectRoot, ".git")
	info, err := os.Stat(gitDirectory)
	if err != nil || !info.IsDir() {
		return nil
	}
	excludePath := filepath.Join(gitDirectory, "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "/.productcrew/" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o700); err != nil {
		return err
	}
	content := strings.TrimRight(string(data), "\r\n")
	if content != "" {
		content += "\n"
	}
	content += "/.productcrew/\n"
	return os.WriteFile(excludePath, []byte(content), 0o600)
}

func SortAttachments(values []Attachment) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}
