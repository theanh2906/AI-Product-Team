package storage

import (
	"context"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

func TestJSONNotificationRepositoryPersistsReadLifecycle(t *testing.T) {
	directory := t.TempDir()
	repository, err := NewJSONNotificationRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	created := notifications.Notification{ID: "notification-1", Level: notifications.LevelSuccess, Title: "QA passed", Message: "All checks passed.", CreatedAt: time.Now()}
	if err := repository.Create(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewJSONNotificationRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reopened.List(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].Read {
		t.Fatalf("notification was not persisted: %+v, %v", items, err)
	}
	if _, err := reopened.MarkRead(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	items, err = reopened.List(context.Background(), 10)
	if err != nil || !items[0].Read {
		t.Fatalf("read state was not persisted: %+v, %v", items, err)
	}
	if err := reopened.DeleteRead(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err = reopened.List(context.Background(), 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("read notification was not cleared: %+v, %v", items, err)
	}
}
