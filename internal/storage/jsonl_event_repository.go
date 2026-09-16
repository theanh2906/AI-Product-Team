package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

type JSONLEventRepository struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
}

func NewJSONLEventRepository(directory string) (*JSONLEventRepository, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("event data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create event data directory: %w", err)
	}
	return &JSONLEventRepository{path: filepath.Join(directory, "events.jsonl"), maxBytes: 25 * 1024 * 1024, backups: 4}, nil
}

func (r *JSONLEventRepository) Path() string { return r.path }

func (r *JSONLEventRepository) Append(event observability.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, err := os.Stat(r.path); err == nil && info.Size() >= r.maxBytes {
		if err := rotateJSONL(r.path, r.backups); err != nil {
			return fmt.Errorf("rotate events.jsonl: %w", err)
		}
	}
	file, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open events.jsonl: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(event); err != nil {
		return fmt.Errorf("append events.jsonl: %w", err)
	}
	return file.Sync()
}

func (r *JSONLEventRepository) List(query observability.Query) ([]observability.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	limit := query.Limit
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	events := make([]observability.Event, 0, limit)
	for index := r.backups; index >= 0; index-- {
		path := r.path
		if index > 0 {
			path = fmt.Sprintf("%s.%d", r.path, index)
		}
		if err := scanEventFile(path, query, &events); err != nil {
			return nil, err
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.After(events[j].Timestamp) })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

// ListAll returns every recorded event, including rotated backups, with no
// limit. It backs datasource migration, which must copy the full history
// rather than the capped page List returns for UI consumption.
func (r *JSONLEventRepository) ListAll() ([]observability.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]observability.Event, 0)
	for index := r.backups; index >= 0; index-- {
		path := r.path
		if index > 0 {
			path = fmt.Sprintf("%s.%d", r.path, index)
		}
		if err := scanEventFile(path, observability.Query{}, &events); err != nil {
			return nil, err
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	return events, nil
}

func scanEventFile(path string, query observability.Query, events *[]observability.Event) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var event observability.Event
		if json.Unmarshal(scanner.Bytes(), &event) == nil && matchesEvent(event, query) {
			*events = append(*events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", filepath.Base(path), err)
	}
	return nil
}

func rotateJSONL(path string, backups int) error {
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for index := backups - 1; index >= 1; index-- {
		older, newer := fmt.Sprintf("%s.%d", path, index), fmt.Sprintf("%s.%d", path, index+1)
		if _, err := os.Stat(older); err == nil {
			if err := os.Rename(older, newer); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(path); err == nil {
		return os.Rename(path, path+".1")
	}
	return nil
}

func matchesEvent(event observability.Event, query observability.Query) bool {
	return (query.ProjectID == "" || event.ProjectID == query.ProjectID) &&
		(query.CorrelationID == "" || event.CorrelationID == query.CorrelationID) &&
		(query.Level == "" || event.Level == query.Level) &&
		(query.Category == "" || event.Category == query.Category) &&
		(query.Name == "" || event.Name == query.Name) &&
		(query.Since.IsZero() || !event.Timestamp.Before(query.Since))
}
