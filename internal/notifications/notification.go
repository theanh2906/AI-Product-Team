package notifications

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Level string

const (
	LevelInfo    Level = "info"
	LevelSuccess Level = "success"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

func (l Level) Valid() bool {
	return l == LevelInfo || l == LevelSuccess || l == LevelWarning || l == LevelError
}

type Notification struct {
	ID          string    `json:"id"`
	Level       Level     `json:"level"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	ProjectID   string    `json:"projectId,omitempty"`
	ProjectName string    `json:"projectName,omitempty"`
	EntityID    string    `json:"entityId,omitempty"`
	Route       string    `json:"route,omitempty"`
	Read        bool      `json:"read"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Draft struct {
	Level       Level
	Kind        string
	Title       string
	Message     string
	ProjectID   string
	ProjectName string
	EntityID    string
	Route       string
}

type Repository interface {
	Create(context.Context, Notification) error
	List(context.Context, int) ([]Notification, error)
	MarkRead(context.Context, string) (Notification, error)
	MarkAllRead(context.Context) error
	DeleteRead(context.Context) error
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Create(ctx context.Context, draft Draft) (Notification, error) {
	if !draft.Level.Valid() {
		draft.Level = LevelInfo
	}
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Message = strings.TrimSpace(draft.Message)
	if draft.Title == "" || draft.Message == "" {
		return Notification{}, fmt.Errorf("notification title and message are required")
	}
	value := Notification{
		ID: newID(), Level: draft.Level, Kind: strings.TrimSpace(draft.Kind),
		Title: draft.Title, Message: draft.Message, ProjectID: strings.TrimSpace(draft.ProjectID),
		ProjectName: strings.TrimSpace(draft.ProjectName),
		EntityID:    strings.TrimSpace(draft.EntityID), Route: strings.TrimSpace(draft.Route), CreatedAt: s.now(),
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return Notification{}, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.repository.List(ctx, limit)
}

func (s *Service) MarkRead(ctx context.Context, id string) (Notification, error) {
	return s.repository.MarkRead(ctx, id)
}

func (s *Service) MarkAllRead(ctx context.Context) error { return s.repository.MarkAllRead(ctx) }
func (s *Service) DeleteRead(ctx context.Context) error  { return s.repository.DeleteRead(ctx) }

func newID() string {
	random := make([]byte, 6)
	_, _ = rand.Read(random)
	return fmt.Sprintf("notification-%d-%s", time.Now().UnixNano(), hex.EncodeToString(random))
}
