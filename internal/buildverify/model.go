package buildverify

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("build verification profile not found")

type Action struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Executable  string   `json:"executable"`
	Arguments   []string `json:"arguments"`
	WorkingDir  string   `json:"workingDir"`
	Source      string   `json:"source"`
	Recommended bool     `json:"recommended"`
	Confidence  int      `json:"confidence"`
}

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

type Run struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	TaskID      string     `json:"taskId,omitempty"`
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

type Repository interface {
	Get(context.Context, string) (Profile, error)
	Upsert(context.Context, Profile) error
}

type Advisor interface {
	Recommend(context.Context, string, []Action) (string, string, error)
}

type LineReporter func(level, message string)
