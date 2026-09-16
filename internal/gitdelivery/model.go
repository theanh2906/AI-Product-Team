package gitdelivery

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("git delivery not found")

type Status string

const (
	StatusQueued     Status = "queued"
	StatusRunning    Status = "running"
	StatusRecovering Status = "recovering"
	StatusPaused     Status = "paused"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type Step string

const (
	StepPreflight Step = "preflight"
	StepCommit    Step = "commit"
	StepPush      Step = "push"
	StepComplete  Step = "complete"
)

type Ticket struct {
	TaskID string   `json:"taskId"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Files  []string `json:"files"`
}

type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Step      Step      `json:"step"`
	Message   string    `json:"message"`
	Detail    string    `json:"detail,omitempty"`
}

type Recovery struct {
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	Attempt  int    `json:"attempt"`
	Summary  string `json:"summary,omitempty"`
}

type Delivery struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"projectId"`
	ProjectName   string     `json:"projectName"`
	Branch        string     `json:"branch"`
	Remote        string     `json:"remote"`
	Status        Status     `json:"status"`
	Step          Step       `json:"step"`
	HeadBefore    string     `json:"headBefore"`
	CommitSHA     string     `json:"commitSha,omitempty"`
	ShortSHA      string     `json:"shortSha,omitempty"`
	CommitMessage string     `json:"commitMessage"`
	Fingerprint   string     `json:"fingerprint"`
	Tickets       []Ticket   `json:"tickets"`
	Files         []string   `json:"files"`
	Events        []Event    `json:"events"`
	Recovery      *Recovery  `json:"recovery,omitempty"`
	StopRequested bool       `json:"stopRequested,omitempty"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type Repository interface {
	Get(context.Context, string) (Delivery, error)
	List(context.Context, string, int) ([]Delivery, error)
	Upsert(context.Context, Delivery) error
}
