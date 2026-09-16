package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/gitdelivery"
)

type SQLiteGitDeliveryRepository struct {
	db *sql.DB
}

func NewSQLiteGitDeliveryRepository(db *sql.DB) (*SQLiteGitDeliveryRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS git_deliveries (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		started_at TEXT NOT NULL,
		payload TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_git_deliveries_project_started ON git_deliveries(project_id, started_at DESC);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create git_deliveries table: %w", err)
	}
	return &SQLiteGitDeliveryRepository{db: db}, nil
}

func (r *SQLiteGitDeliveryRepository) Get(ctx context.Context, id string) (gitdelivery.Delivery, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM git_deliveries WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return gitdelivery.Delivery{}, gitdelivery.ErrNotFound
	}
	if err != nil {
		return gitdelivery.Delivery{}, fmt.Errorf("read git delivery: %w", err)
	}
	return decodeGitDelivery(payload)
}

func (r *SQLiteGitDeliveryRepository) List(ctx context.Context, projectID string, limit int) ([]gitdelivery.Delivery, error) {
	query := `SELECT payload FROM git_deliveries`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY started_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list git deliveries: %w", err)
	}
	defer rows.Close()
	result := make([]gitdelivery.Delivery, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan git delivery: %w", err)
		}
		delivery, err := decodeGitDelivery(payload)
		if err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func (r *SQLiteGitDeliveryRepository) ListAll(ctx context.Context) ([]gitdelivery.Delivery, error) {
	return r.List(ctx, "", 0)
}

func (r *SQLiteGitDeliveryRepository) Upsert(ctx context.Context, delivery gitdelivery.Delivery) error {
	payload, err := json.Marshal(delivery)
	if err != nil {
		return fmt.Errorf("encode git delivery: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO git_deliveries (id, project_id, started_at, payload) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET project_id = excluded.project_id, started_at = excluded.started_at, payload = excluded.payload`, delivery.ID, delivery.ProjectID, delivery.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), string(payload))
	if err != nil {
		return fmt.Errorf("upsert git delivery: %w", err)
	}
	return nil
}

func decodeGitDelivery(payload string) (gitdelivery.Delivery, error) {
	var delivery gitdelivery.Delivery
	if err := json.Unmarshal([]byte(payload), &delivery); err != nil {
		return delivery, fmt.Errorf("decode git delivery: %w", err)
	}
	return delivery, nil
}
