package insights

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Save(ctx context.Context, projectID string, kind Kind, value any) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("project is required")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode project insight: %w", err)
	}
	record, err := s.repository.Get(ctx, projectID, kind)
	if err != nil && err != ErrNotFound {
		return err
	}
	if record.ID == "" {
		record.ID = insightID()
	}
	record.ProjectID = projectID
	record.Kind = kind
	record.Payload = payload
	record.UpdatedAt = s.now()
	return s.repository.Upsert(ctx, record)
}

func (s *Service) Load(ctx context.Context, projectID string, kind Kind, destination any) error {
	record, err := s.repository.Get(ctx, strings.TrimSpace(projectID), kind)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(record.Payload, destination); err != nil {
		return fmt.Errorf("decode project insight: %w", err)
	}
	return nil
}

func (s *Service) ListProject(ctx context.Context, projectID string) ([]Record, error) {
	return s.repository.ListProject(ctx, strings.TrimSpace(projectID))
}

func (s *Service) DeleteProject(ctx context.Context, projectID string) (int, error) {
	return s.repository.DeleteProject(ctx, strings.TrimSpace(projectID))
}

func insightID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return "insight-" + hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("insight-%x", time.Now().UnixNano())
}
