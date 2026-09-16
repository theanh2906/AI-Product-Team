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

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

const notificationFileVersion = 1
const maxNotifications = 500

type notificationFile struct {
	Version int                          `json:"version"`
	Items   []notifications.Notification `json:"items"`
}

type JSONNotificationRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONNotificationRepository(directory string) (*JSONNotificationRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("notification data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create notification data directory: %w", err)
	}
	return &JSONNotificationRepository{path: filepath.Join(directory, "notifications.json"), directory: directory}, nil
}

func (r *JSONNotificationRepository) Path() string { return r.path }

func (r *JSONNotificationRepository) Create(ctx context.Context, value notifications.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	data.Items = append(data.Items, value)
	if len(data.Items) > maxNotifications {
		data.Items = data.Items[len(data.Items)-maxNotifications:]
	}
	return r.writeLocked(data)
}

func (r *JSONNotificationRepository) List(ctx context.Context, limit int) ([]notifications.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	items := make([]notifications.Notification, len(data.Items))
	copy(items, data.Items)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *JSONNotificationRepository) MarkRead(ctx context.Context, id string) (notifications.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return notifications.Notification{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return notifications.Notification{}, err
	}
	for index := range data.Items {
		if data.Items[index].ID == id {
			data.Items[index].Read = true
			if err := r.writeLocked(data); err != nil {
				return notifications.Notification{}, err
			}
			return data.Items[index], nil
		}
	}
	return notifications.Notification{}, fmt.Errorf("notification not found")
}

func (r *JSONNotificationRepository) MarkAllRead(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	changed := false
	for index := range data.Items {
		if !data.Items[index].Read {
			data.Items[index].Read = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return r.writeLocked(data)
}

func (r *JSONNotificationRepository) DeleteRead(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	remaining := data.Items[:0]
	for _, item := range data.Items {
		if !item.Read {
			remaining = append(remaining, item)
		}
	}
	data.Items = remaining
	return r.writeLocked(data)
}

func (r *JSONNotificationRepository) readLocked() (notificationFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return notificationFile{Version: notificationFileVersion, Items: []notifications.Notification{}}, nil
	}
	if err != nil {
		return notificationFile{}, fmt.Errorf("read notifications.json: %w", err)
	}
	var value notificationFile
	if err := json.Unmarshal(data, &value); err != nil {
		return notificationFile{}, fmt.Errorf("decode notifications.json: %w", err)
	}
	if value.Version != notificationFileVersion {
		return notificationFile{}, fmt.Errorf("unsupported notifications.json version %d", value.Version)
	}
	if value.Items == nil {
		value.Items = []notifications.Notification{}
	}
	return value, nil
}

func (r *JSONNotificationRepository) writeLocked(value notificationFile) error {
	value.Version = notificationFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode notifications.json: %w", err)
	}
	temporary, err := os.CreateTemp(r.directory, "notifications-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary notification file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
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
	if err := replaceFile(temporaryPath, r.path); err != nil {
		return fmt.Errorf("replace notifications.json: %w", err)
	}
	removeTemporary = false
	return nil
}
