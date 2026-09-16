package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
)

const buildProfileFileVersion = 1

type buildProfileFile struct {
	Version  int                   `json:"version"`
	Profiles []buildverify.Profile `json:"profiles"`
}

type JSONBuildProfileRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONBuildProfileRepository(directory string) (*JSONBuildProfileRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("build profile data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	return &JSONBuildProfileRepository{path: filepath.Join(directory, "build-profiles.json"), directory: directory}, nil
}

func (r *JSONBuildProfileRepository) Get(ctx context.Context, projectID string) (buildverify.Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return buildverify.Profile{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return buildverify.Profile{}, err
	}
	for _, profile := range data.Profiles {
		if profile.ProjectID == projectID {
			return profile, nil
		}
	}
	return buildverify.Profile{}, buildverify.ErrNotFound
}

// ListAll returns every build profile regardless of project. It backs
// datasource migration, which must copy the full dataset.
func (r *JSONBuildProfileRepository) ListAll(ctx context.Context) ([]buildverify.Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	profiles := make([]buildverify.Profile, len(data.Profiles))
	copy(profiles, data.Profiles)
	return profiles, nil
}

func (r *JSONBuildProfileRepository) Upsert(ctx context.Context, profile buildverify.Profile) error {
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

func (r *JSONBuildProfileRepository) readLocked() (buildProfileFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return buildProfileFile{Version: buildProfileFileVersion, Profiles: []buildverify.Profile{}}, nil
	}
	if err != nil {
		return buildProfileFile{}, err
	}
	var value buildProfileFile
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode build-profiles.json: %w", err)
	}
	if value.Version != buildProfileFileVersion {
		return value, fmt.Errorf("unsupported build-profiles.json version %d", value.Version)
	}
	if value.Profiles == nil {
		value.Profiles = []buildverify.Profile{}
	}
	return value, nil
}

func (r *JSONBuildProfileRepository) writeLocked(value buildProfileFile) error {
	value.Version = buildProfileFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(r.directory, "build-profiles-*.tmp")
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
