package insights

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("project insight not found")

type Kind string

const (
	KindFeatureRadar Kind = "feature-radar"
	KindBugScan      Kind = "bug-scan"
)

type Record struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Kind      Kind            `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Repository is the stable persistence port for AI-generated project insight.
// JSON is the first adapter; a database adapter can replace it without changing
// the scanners, HTTP handlers, or Angular application.
type Repository interface {
	Upsert(context.Context, Record) error
	Get(context.Context, string, Kind) (Record, error)
	ListProject(context.Context, string) ([]Record, error)
	Delete(context.Context, string, Kind) error
	DeleteProject(context.Context, string) (int, error)
}
