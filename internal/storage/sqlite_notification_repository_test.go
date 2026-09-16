package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

func TestSQLiteNotificationRepositoryImplementsDurableCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteNotificationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := notifications.Notification{ID: "n-1", Level: notifications.LevelInfo, Title: "First", CreatedAt: time.Now().UTC().Add(-time.Minute)}
	second := notifications.Notification{ID: "n-2", Level: notifications.LevelSuccess, Title: "Second", CreatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(ctx, 0)
	if err != nil || len(items) != 2 || items[0].ID != "n-2" {
		t.Fatalf("unexpected list order: %+v %v", items, err)
	}
	updated, err := repository.MarkRead(ctx, "n-1")
	if err != nil || !updated.Read {
		t.Fatalf("expected n-1 marked read: %+v %v", updated, err)
	}
	if err := repository.MarkAllRead(ctx); err != nil {
		t.Fatal(err)
	}
	items, err = repository.List(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if !item.Read {
			t.Fatalf("expected all notifications read: %+v", items)
		}
	}
	if err := repository.DeleteRead(ctx); err != nil {
		t.Fatal(err)
	}
	items, err = repository.List(ctx, 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("expected no notifications after DeleteRead: %+v %v", items, err)
	}
}

func TestSQLiteNotificationRepositoryTrimsToMax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "productcrew.sqlite")
	db, err := openSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewSQLiteNotificationRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < maxNotifications+5; index++ {
		note := notifications.Notification{ID: fmt.Sprintf("n-%d", index), CreatedAt: base.Add(time.Duration(index) * time.Second)}
		if err := repository.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repository.List(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != maxNotifications {
		t.Fatalf("expected trimmed count %d, got %d", maxNotifications, len(items))
	}
}
