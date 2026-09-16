package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/buildverify"
)

// SQLiteBuildProfileRepository is the sqlite adapter for
// buildverify.Repository.
type SQLiteBuildProfileRepository struct {
	db *sql.DB
}

func NewSQLiteBuildProfileRepository(db *sql.DB) (*SQLiteBuildProfileRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS build_profiles (
		project_id TEXT PRIMARY KEY,
		payload TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create build_profiles table: %w", err)
	}
	return &SQLiteBuildProfileRepository{db: db}, nil
}

func (r *SQLiteBuildProfileRepository) Get(ctx context.Context, projectID string) (buildverify.Profile, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM build_profiles WHERE project_id = ?`, projectID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return buildverify.Profile{}, buildverify.ErrNotFound
	}
	if err != nil {
		return buildverify.Profile{}, fmt.Errorf("read build profile: %w", err)
	}
	var profile buildverify.Profile
	if err := json.Unmarshal([]byte(payload), &profile); err != nil {
		return buildverify.Profile{}, fmt.Errorf("decode build profile: %w", err)
	}
	return profile, nil
}

// ListAll returns every build profile regardless of project. It backs
// datasource migration, which must copy the full dataset.
func (r *SQLiteBuildProfileRepository) ListAll(ctx context.Context) ([]buildverify.Profile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM build_profiles`)
	if err != nil {
		return nil, fmt.Errorf("list build profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]buildverify.Profile, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan build profile: %w", err)
		}
		var profile buildverify.Profile
		if err := json.Unmarshal([]byte(payload), &profile); err != nil {
			return nil, fmt.Errorf("decode build profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list build profiles: %w", err)
	}
	return profiles, nil
}

func (r *SQLiteBuildProfileRepository) Upsert(ctx context.Context, profile buildverify.Profile) error {
	payload, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("encode build profile: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO build_profiles (project_id, payload) VALUES (?, ?)
		ON CONFLICT(project_id) DO UPDATE SET payload = excluded.payload`, profile.ProjectID, payload)
	if err != nil {
		return fmt.Errorf("upsert build profile: %w", err)
	}
	return nil
}
