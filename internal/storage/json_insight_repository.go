package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
)

const insightFileVersion = 1

type insightFile struct {
	Version int               `json:"version"`
	Records []insights.Record `json:"records"`
}

type JSONInsightRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONInsightRepository(directory string) (*JSONInsightRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("insight data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create insight data directory: %w", err)
	}
	return &JSONInsightRepository{directory: directory, path: filepath.Join(directory, "insights.json")}, nil
}

func (r *JSONInsightRepository) Path() string { return r.path }

func (r *JSONInsightRepository) Upsert(ctx context.Context, record insights.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Records {
		if data.Records[index].ProjectID == record.ProjectID && data.Records[index].Kind == record.Kind {
			data.Records[index] = record
			return r.writeLocked(data)
		}
	}
	data.Records = append(data.Records, record)
	return r.writeLocked(data)
}

func (r *JSONInsightRepository) Get(ctx context.Context, projectID string, kind insights.Kind) (insights.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return insights.Record{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return insights.Record{}, err
	}
	for _, record := range data.Records {
		if record.ProjectID == projectID && record.Kind == kind {
			return record, nil
		}
	}
	return insights.Record{}, insights.ErrNotFound
}

func (r *JSONInsightRepository) ListProject(ctx context.Context, projectID string) ([]insights.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	records := make([]insights.Record, 0)
	for _, record := range data.Records {
		if record.ProjectID == projectID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].UpdatedAt.After(records[j].UpdatedAt) })
	return records, nil
}

// ListAll returns every insight record regardless of project. It backs
// datasource migration, which must copy the full dataset rather than one
// project at a time.
func (r *JSONInsightRepository) ListAll(ctx context.Context) ([]insights.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	records := make([]insights.Record, len(data.Records))
	copy(records, data.Records)
	return records, nil
}

func (r *JSONInsightRepository) Delete(ctx context.Context, projectID string, kind insights.Kind) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Records {
		if data.Records[index].ProjectID == projectID && data.Records[index].Kind == kind {
			data.Records = append(data.Records[:index], data.Records[index+1:]...)
			return r.writeLocked(data)
		}
	}
	return insights.ErrNotFound
}

func (r *JSONInsightRepository) DeleteProject(ctx context.Context, projectID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	data, err := r.readLocked()
	if err != nil {
		return 0, err
	}
	kept := data.Records[:0]
	removed := 0
	for _, record := range data.Records {
		if record.ProjectID == projectID {
			removed++
			continue
		}
		kept = append(kept, record)
	}
	if removed == 0 {
		return 0, nil
	}
	data.Records = kept
	return removed, r.writeLocked(data)
}

func (r *JSONInsightRepository) readLocked() (insightFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return insightFile{Version: insightFileVersion, Records: []insights.Record{}}, nil
	}
	if err != nil {
		return insightFile{}, fmt.Errorf("read insights.json: %w", err)
	}
	var value insightFile
	if err := json.Unmarshal(data, &value); err != nil {
		return insightFile{}, fmt.Errorf("decode insights.json: %w", err)
	}
	if value.Version != insightFileVersion {
		return insightFile{}, fmt.Errorf("unsupported insights.json version %d", value.Version)
	}
	if value.Records == nil {
		value.Records = []insights.Record{}
	}
	return value, nil
}

func (r *JSONInsightRepository) writeLocked(value insightFile) error {
	value.Version = insightFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode insights.json: %w", err)
	}
	temporary, err := os.CreateTemp(r.directory, "insights-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary insight file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, r.path); err != nil {
		return fmt.Errorf("replace insights.json: %w", err)
	}
	removeTemporary = false
	return nil
}
