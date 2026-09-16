package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

// SQLiteEventRepository is the sqlite adapter for observability.Repository.
type SQLiteEventRepository struct {
	db   *sql.DB
	path string
}

func NewSQLiteEventRepository(db *sql.DB, path string) (*SQLiteEventRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		timestamp TEXT NOT NULL,
		project_id TEXT,
		correlation_id TEXT,
		level TEXT,
		category TEXT,
		name TEXT,
		payload TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create events table: %w", err)
	}
	return &SQLiteEventRepository{db: db, path: path}, nil
}

func (r *SQLiteEventRepository) Path() string { return r.path }

func (r *SQLiteEventRepository) Append(event observability.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	_, err = r.db.Exec(`INSERT INTO events (id, timestamp, project_id, correlation_id, level, category, name, payload) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Timestamp.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), event.ProjectID, event.CorrelationID, string(event.Level), event.Category, event.Name, payload)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

// ListAll returns every recorded event with no limit. It backs datasource
// migration, which must copy the full history rather than the capped page
// List returns for UI consumption.
func (r *SQLiteEventRepository) ListAll() ([]observability.Event, error) {
	rows, err := r.db.Query(`SELECT payload FROM events ORDER BY timestamp ASC`)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	events := make([]observability.Event, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		var event observability.Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}

func (r *SQLiteEventRepository) List(query observability.Query) ([]observability.Event, error) {
	limit := query.Limit
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	var conditions []string
	var args []any
	if query.ProjectID != "" {
		conditions = append(conditions, "project_id = ?")
		args = append(args, query.ProjectID)
	}
	if query.CorrelationID != "" {
		conditions = append(conditions, "correlation_id = ?")
		args = append(args, query.CorrelationID)
	}
	if query.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, string(query.Level))
	}
	if query.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, query.Category)
	}
	if query.Name != "" {
		conditions = append(conditions, "name = ?")
		args = append(args, query.Name)
	}
	if !query.Since.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, query.Since.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"))
	}
	sqlQuery := "SELECT payload FROM events"
	if len(conditions) > 0 {
		sqlQuery += " WHERE " + strings.Join(conditions, " AND ")
	}
	sqlQuery += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	events := make([]observability.Event, 0, limit)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		var event observability.Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}
