package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/deploy"
)

const deployProfileFileVersion = 1

type deployProfileFile struct {
	Version  int              `json:"version"`
	Profiles []deploy.Profile `json:"profiles"`
}

// JSONDeployProfileRepository is the local-json adapter for
// deploy.Repository, kept entirely separate from build profile storage.
type JSONDeployProfileRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONDeployProfileRepository(directory string) (*JSONDeployProfileRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("deploy profile data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	return &JSONDeployProfileRepository{path: filepath.Join(directory, "deploy-profiles.json"), directory: directory}, nil
}

func (r *JSONDeployProfileRepository) Get(ctx context.Context, projectID string) (deploy.Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return deploy.Profile{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return deploy.Profile{}, err
	}
	for _, profile := range data.Profiles {
		if profile.ProjectID == projectID {
			return profile, nil
		}
	}
	return deploy.Profile{}, deploy.ErrNotFound
}

// ListAll returns every deploy profile regardless of project. It backs
// datasource migration, which must copy the full dataset.
func (r *JSONDeployProfileRepository) ListAll(ctx context.Context) ([]deploy.Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	profiles := make([]deploy.Profile, len(data.Profiles))
	copy(profiles, data.Profiles)
	return profiles, nil
}

func (r *JSONDeployProfileRepository) Upsert(ctx context.Context, profile deploy.Profile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Profiles {
		if data.Profiles[index].ProjectID == profile.ProjectID {
			data.Profiles[index] = profile
			return r.writeLocked(data)
		}
	}
	data.Profiles = append(data.Profiles, profile)
	return r.writeLocked(data)
}

func (r *JSONDeployProfileRepository) readLocked() (deployProfileFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return deployProfileFile{Version: deployProfileFileVersion, Profiles: []deploy.Profile{}}, nil
	}
	if err != nil {
		return deployProfileFile{}, err
	}
	var value deployProfileFile
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode deploy-profiles.json: %w", err)
	}
	if value.Version != deployProfileFileVersion {
		return value, fmt.Errorf("unsupported deploy-profiles.json version %d", value.Version)
	}
	if value.Profiles == nil {
		value.Profiles = []deploy.Profile{}
	}
	return value, nil
}

func (r *JSONDeployProfileRepository) writeLocked(value deployProfileFile) error {
	value.Version = deployProfileFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(r.directory, "deploy-profiles-*.tmp")
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
