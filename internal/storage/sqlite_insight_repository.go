package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
)

// SQLiteInsightRepository is the sqlite adapter for insights.Repository.
type SQLiteInsightRepository struct {
	db *sql.DB
}

func NewSQLiteInsightRepository(db *sql.DB) (*SQLiteInsightRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS insights (
		project_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL,
		PRIMARY KEY (project_id, kind)
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create insights table: %w", err)
	}
	return &SQLiteInsightRepository{db: db}, nil
}

func (r *SQLiteInsightRepository) Upsert(ctx context.Context, record insights.Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode insight record: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO insights (project_id, kind, payload) VALUES (?, ?, ?)
		ON CONFLICT(project_id, kind) DO UPDATE SET payload = excluded.payload`, record.ProjectID, string(record.Kind), payload)
	if err != nil {
		return fmt.Errorf("upsert insight record: %w", err)
	}
	return nil
}

func (r *SQLiteInsightRepository) Get(ctx context.Context, projectID string, kind insights.Kind) (insights.Record, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM insights WHERE project_id = ? AND kind = ?`, projectID, string(kind)).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return insights.Record{}, insights.ErrNotFound
	}
	if err != nil {
		return insights.Record{}, fmt.Errorf("read insight record: %w", err)
	}
	return decodeInsightRecord(payload)
}

func (r *SQLiteInsightRepository) ListProject(ctx context.Context, projectID string) ([]insights.Record, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM insights WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list insight records: %w", err)
	}
	defer rows.Close()
	records := make([]insights.Record, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan insight record: %w", err)
		}
		record, err := decodeInsightRecord(payload)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list insight records: %w", err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].UpdatedAt.After(records[j].UpdatedAt) })
	return records, nil
}

// ListAll returns every insight record regardless of project. It backs
// datasource migration, which must copy the full dataset rather than one
// project at a time.
func (r *SQLiteInsightRepository) ListAll(ctx context.Context) ([]insights.Record, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM insights`)
	if err != nil {
		return nil, fmt.Errorf("list insight records: %w", err)
	}
	defer rows.Close()
	records := make([]insights.Record, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan insight record: %w", err)
		}
		record, err := decodeInsightRecord(payload)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list insight records: %w", err)
	}
	return records, nil
}

func (r *SQLiteInsightRepository) Delete(ctx context.Context, projectID string, kind insights.Kind) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM insights WHERE project_id = ? AND kind = ?`, projectID, string(kind))
	if err != nil {
		return fmt.Errorf("delete insight record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete insight record: %w", err)
	}
	if affected == 0 {
		return insights.ErrNotFound
	}
	return nil
}

func (r *SQLiteInsightRepository) DeleteProject(ctx context.Context, projectID string) (int, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM insights WHERE project_id = ?`, projectID)
	if err != nil {
		return 0, fmt.Errorf("delete project insight records: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete project insight records: %w", err)
	}
	return int(affected), nil
}

func decodeInsightRecord(payload string) (insights.Record, error) {
	var record insights.Record
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return insights.Record{}, fmt.Errorf("decode insight record: %w", err)
	}
	return record, nil
}
