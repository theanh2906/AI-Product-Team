package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

// SQLiteNotificationRepository is the sqlite adapter for
// notifications.Repository.
type SQLiteNotificationRepository struct {
	db *sql.DB
}

func NewSQLiteNotificationRepository(db *sql.DB) (*SQLiteNotificationRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS notifications (
		id TEXT PRIMARY KEY,
		created_at TEXT NOT NULL,
		read INTEGER NOT NULL,
		payload TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create notifications table: %w", err)
	}
	return &SQLiteNotificationRepository{db: db}, nil
}

func (r *SQLiteNotificationRepository) Create(ctx context.Context, value notifications.Notification) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO notifications (id, created_at, read, payload) VALUES (?, ?, ?, ?)`,
		value.ID, value.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), boolToInt(value.Read), payload); err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return r.trimLocked(ctx)
}

func (r *SQLiteNotificationRepository) trimLocked(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notifications WHERE id NOT IN (
		SELECT id FROM notifications ORDER BY created_at DESC LIMIT ?
	)`, maxNotifications)
	if err != nil {
		return fmt.Errorf("trim notifications: %w", err)
	}
	return nil
}

func (r *SQLiteNotificationRepository) List(ctx context.Context, limit int) ([]notifications.Notification, error) {
	query := `SELECT payload FROM notifications ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	items := make([]notifications.Notification, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		var item notifications.Notification
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, fmt.Errorf("decode notification: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return items, nil
}

func (r *SQLiteNotificationRepository) MarkRead(ctx context.Context, id string) (notifications.Notification, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM notifications WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return notifications.Notification{}, fmt.Errorf("notification not found")
	}
	if err != nil {
		return notifications.Notification{}, fmt.Errorf("read notification: %w", err)
	}
	var item notifications.Notification
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return notifications.Notification{}, fmt.Errorf("decode notification: %w", err)
	}
	item.Read = true
	updated, err := json.Marshal(item)
	if err != nil {
		return notifications.Notification{}, fmt.Errorf("encode notification: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE notifications SET read = 1, payload = ? WHERE id = ?`, updated, id); err != nil {
		return notifications.Notification{}, fmt.Errorf("update notification: %w", err)
	}
	return item, nil
}

func (r *SQLiteNotificationRepository) MarkAllRead(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, `SELECT id, payload FROM notifications WHERE read = 0`)
	if err != nil {
		return fmt.Errorf("list unread notifications: %w", err)
	}
	type pending struct {
		id      string
		payload string
	}
	var updates []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.payload); err != nil {
			rows.Close()
			return fmt.Errorf("scan notification: %w", err)
		}
		updates = append(updates, item)
	}
	closeErr := rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list unread notifications: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("list unread notifications: %w", closeErr)
	}
	for _, item := range updates {
		var value notifications.Notification
		if err := json.Unmarshal([]byte(item.payload), &value); err != nil {
			return fmt.Errorf("decode notification: %w", err)
		}
		value.Read = true
		updated, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode notification: %w", err)
		}
		if _, err := r.db.ExecContext(ctx, `UPDATE notifications SET read = 1, payload = ? WHERE id = ?`, updated, item.id); err != nil {
			return fmt.Errorf("update notification: %w", err)
		}
	}
	return nil
}

func (r *SQLiteNotificationRepository) DeleteRead(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM notifications WHERE read = 1`); err != nil {
		return fmt.Errorf("delete read notifications: %w", err)
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
