package observability

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Service struct {
	repository  Repository
	logger      *slog.Logger
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewService(repository Repository, logger *slog.Logger) *Service {
	return &Service{repository: repository, logger: logger, subscribers: make(map[chan Event]struct{})}
}

func (s *Service) Record(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.ID == "" {
		event.ID = eventID()
	}
	if event.Level == "" {
		event.Level = LevelInfo
	}
	if err := s.repository.Append(event); err != nil {
		s.logger.Error("persist observability event", "error", err, "event", event.Name, "correlation_id", event.CorrelationID)
	}
	attributes := []any{"event", event.Name, "category", event.Category, "correlation_id", event.CorrelationID, "project_id", event.ProjectID, "entity_type", event.EntityType, "entity_id", event.EntityID, "agent", event.Agent, "stage", event.Stage, "outcome", event.Outcome, "duration_ms", event.DurationMS, "attributes", event.Attributes}
	switch event.Level {
	case LevelError:
		s.logger.Error(event.Message, attributes...)
	case LevelWarn:
		s.logger.Warn(event.Message, attributes...)
	case LevelDebug:
		s.logger.Debug(event.Message, attributes...)
	default:
		s.logger.Info(event.Message, attributes...)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (s *Service) List(query Query) ([]Event, error) { return s.repository.List(query) }
func (s *Service) EventPath() string                 { return s.repository.Path() }

func (s *Service) Subscribe() (<-chan Event, func()) {
	updates := make(chan Event, 16)
	s.mu.Lock()
	s.subscribers[updates] = struct{}{}
	s.mu.Unlock()
	return updates, func() {
		s.mu.Lock()
		delete(s.subscribers, updates)
		close(updates)
		s.mu.Unlock()
	}
}

func eventID() string {
	return randomID("evt")
}

func NewCorrelationID() string { return randomID("req") }

func randomID(prefix string) string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return prefix + "-" + hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("%s-%x", prefix, time.Now().UnixNano())
}
