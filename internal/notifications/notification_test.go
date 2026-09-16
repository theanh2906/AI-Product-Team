package notifications

import (
	"context"
	"testing"
)

type memoryRepository struct {
	value Notification
}

func (r *memoryRepository) Create(_ context.Context, value Notification) error {
	r.value = value
	return nil
}

func (r *memoryRepository) List(context.Context, int) ([]Notification, error) {
	return []Notification{r.value}, nil
}

func (r *memoryRepository) MarkRead(context.Context, string) (Notification, error) {
	return r.value, nil
}

func (r *memoryRepository) MarkAllRead(context.Context) error { return nil }
func (r *memoryRepository) DeleteRead(context.Context) error  { return nil }

func TestCreatePersistsProjectName(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository)

	created, err := service.Create(context.Background(), Draft{
		Level:       LevelSuccess,
		Kind:        "developer_completed",
		Title:       "DEV-001 implementation completed",
		Message:     "Developer completed this task.",
		ProjectID:   "project-1",
		ProjectName: "vltk-auto",
	})
	if err != nil {
		t.Fatalf("create notification: %v", err)
	}
	if created.ProjectName != "vltk-auto" || repository.value.ProjectName != "vltk-auto" {
		t.Fatalf("project name was not persisted: created=%q stored=%q", created.ProjectName, repository.value.ProjectName)
	}
}
