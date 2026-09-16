package deploy

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("deploy profile not found")

// Profile status values. A profile only reaches StatusReady once the user has
// explicitly saved a SelectedActionID; detection alone never advances a
// profile past StatusNotConfigured.
const (
	StatusNotDetected   = "not_detected"
	StatusNotConfigured = "not_configured"
	StatusReady         = "ready"
)

// Run status values.
const (
	RunStatusQueued  = "queued"
	RunStatusRunning = "running"
	RunStatusPassed  = "passed"
	RunStatusFailed  = "failed"
	RunStatusTimeout = "timeout"
)

// Action is a candidate deploy command detected from a project schema file
// (or, once saved, the command the user explicitly selected).
type Action struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Executable  string   `json:"executable"`
	Arguments   []string `json:"arguments"`
	WorkingDir  string   `json:"workingDir"`
	Source      string   `json:"source"`
	Confidence  int      `json:"confidence"`
	Recommended bool     `json:"recommended"`
}

// Profile is the per-project deploy configuration: detected candidates plus,
// once the user opts in, the explicitly selected deploy action.
type Profile struct {
	ProjectID        string    `json:"projectId"`
	ProjectName      string    `json:"projectName"`
	ProjectPath      string    `json:"projectPath"`
	Status           string    `json:"status"`
	Fingerprint      string    `json:"fingerprint"`
	Detector         string    `json:"detector"`
	Summary          string    `json:"summary"`
	Warning          string    `json:"warning,omitempty"`
	ConfigFiles      []string  `json:"configFiles"`
	Actions          []Action  `json:"actions"`
	SelectedActionID string    `json:"selectedActionId,omitempty"`
	DetectedAt       time.Time `json:"detectedAt"`
	LastRun          *Run      `json:"lastRun,omitempty"`
}

// Run is one execution of a saved deploy action.
type Run struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	ActionID    string     `json:"actionId"`
	ActionLabel string     `json:"actionLabel"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	ExitCode    int        `json:"exitCode"`
	Reason      string     `json:"reason,omitempty"`
	LogPath     string     `json:"logPath"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	DurationMS  int64      `json:"durationMs"`
}

// Repository persists deploy profiles, kept entirely separate from
// buildverify.Repository so deploy configuration can never be confused with
// build verification state.
type Repository interface {
	Get(context.Context, string) (Profile, error)
	Upsert(context.Context, Profile) error
	ListAll(context.Context) ([]Profile, error)
}

// LineReporter streams runner output lines to a live subscriber.
type LineReporter func(level, message string)
