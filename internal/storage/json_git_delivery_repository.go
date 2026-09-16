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

	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
)

const gitDeliveryFileVersion = 1

type gitDeliveryFile struct {
	Version    int                    `json:"version"`
	Deliveries []gitdelivery.Delivery `json:"deliveries"`
}

type JSONGitDeliveryRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONGitDeliveryRepository(directory string) (*JSONGitDeliveryRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("git delivery data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	return &JSONGitDeliveryRepository{path: filepath.Join(directory, "git-deliveries.json"), directory: directory}, nil
}

func (r *JSONGitDeliveryRepository) Get(ctx context.Context, id string) (gitdelivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return gitdelivery.Delivery{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return gitdelivery.Delivery{}, err
	}
	for _, delivery := range data.Deliveries {
		if delivery.ID == id {
			return delivery, nil
		}
	}
	return gitdelivery.Delivery{}, gitdelivery.ErrNotFound
}

func (r *JSONGitDeliveryRepository) List(ctx context.Context, projectID string, limit int) ([]gitdelivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	result := make([]gitdelivery.Delivery, 0)
	for _, delivery := range data.Deliveries {
		if projectID == "" || delivery.ProjectID == projectID {
			result = append(result, delivery)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.After(result[j].StartedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *JSONGitDeliveryRepository) ListAll(ctx context.Context) ([]gitdelivery.Delivery, error) {
	return r.List(ctx, "", 0)
}

func (r *JSONGitDeliveryRepository) Upsert(ctx context.Context, delivery gitdelivery.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Deliveries {
		if data.Deliveries[index].ID == delivery.ID {
			data.Deliveries[index] = delivery
			return r.writeLocked(data)
		}
	}
	data.Deliveries = append(data.Deliveries, delivery)
	return r.writeLocked(data)
}

func (r *JSONGitDeliveryRepository) readLocked() (gitDeliveryFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return gitDeliveryFile{Version: gitDeliveryFileVersion, Deliveries: []gitdelivery.Delivery{}}, nil
	}
	if err != nil {
		return gitDeliveryFile{}, err
	}
	var value gitDeliveryFile
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode git-deliveries.json: %w", err)
	}
	if value.Version != gitDeliveryFileVersion {
		return value, fmt.Errorf("unsupported git-deliveries.json version %d", value.Version)
	}
	if value.Deliveries == nil {
		value.Deliveries = []gitdelivery.Delivery{}
	}
	return value, nil
}

func (r *JSONGitDeliveryRepository) writeLocked(value gitDeliveryFile) error {
	value.Version = gitDeliveryFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(r.directory, "git-deliveries-*.tmp")
	if err != nil {
		return err
	}
	path := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
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
	if err := replaceFile(path, r.path); err != nil {
		return err
	}
	keep = true
	return nil
}
