package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/deploy"
)

// SQLiteDeployProfileRepository is the sqlite adapter for deploy.Repository,
// backed by its own table so deploy state never shares storage with build
// verification profiles.
type SQLiteDeployProfileRepository struct {
	db *sql.DB
}

func NewSQLiteDeployProfileRepository(db *sql.DB) (*SQLiteDeployProfileRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS deploy_profiles (
		project_id TEXT PRIMARY KEY,
		payload TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create deploy_profiles table: %w", err)
	}
	return &SQLiteDeployProfileRepository{db: db}, nil
}

func (r *SQLiteDeployProfileRepository) Get(ctx context.Context, projectID string) (deploy.Profile, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM deploy_profiles WHERE project_id = ?`, projectID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return deploy.Profile{}, deploy.ErrNotFound
	}
	if err != nil {
		return deploy.Profile{}, fmt.Errorf("read deploy profile: %w", err)
	}
	var profile deploy.Profile
	if err := json.Unmarshal([]byte(payload), &profile); err != nil {
		return deploy.Profile{}, fmt.Errorf("decode deploy profile: %w", err)
	}
	return profile, nil
}

// ListAll returns every deploy profile regardless of project. It backs
// datasource migration, which must copy the full dataset.
func (r *SQLiteDeployProfileRepository) ListAll(ctx context.Context) ([]deploy.Profile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM deploy_profiles`)
	if err != nil {
		return nil, fmt.Errorf("list deploy profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]deploy.Profile, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan deploy profile: %w", err)
		}
		var profile deploy.Profile
		if err := json.Unmarshal([]byte(payload), &profile); err != nil {
			return nil, fmt.Errorf("decode deploy profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list deploy profiles: %w", err)
	}
	return profiles, nil
}

func (r *SQLiteDeployProfileRepository) Upsert(ctx context.Context, profile deploy.Profile) error {
	payload, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("encode deploy profile: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO deploy_profiles (project_id, payload) VALUES (?, ?)
		ON CONFLICT(project_id) DO UPDATE SET payload = excluded.payload`, profile.ProjectID, payload)
	if err != nil {
		return fmt.Errorf("upsert deploy profile: %w", err)
	}
	return nil
}
