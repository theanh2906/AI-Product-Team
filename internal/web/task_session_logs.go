package web

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/agentstream"
)

const maxTaskSessionEntries = 400

type taskSessionEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Detail    string    `json:"detail,omitempty"`
}

type taskSessionLog struct {
	ProjectID string             `json:"projectId"`
	TaskID    string             `json:"taskId,omitempty"`
	PlanID    string             `json:"planId,omitempty"`
	Provider  string             `json:"provider,omitempty"`
	Agent     string             `json:"agent,omitempty"`
	Status    string             `json:"status"`
	StartedAt *time.Time         `json:"startedAt,omitempty"`
	UpdatedAt time.Time          `json:"updatedAt"`
	Entries   []taskSessionEntry `json:"entries"`
}

type taskSessionLogManager struct {
	mu          sync.Mutex
	sessions    map[string]taskSessionLog
	subscribers map[string]map[chan taskSessionLog]struct{}
	nextID      atomic.Uint64
}

type taskSessionReporter struct {
	manager   *taskSessionLogManager
	projectID string
	taskID    string
}

func newTaskSessionLogManager() *taskSessionLogManager {
	return &taskSessionLogManager{
		sessions:    make(map[string]taskSessionLog),
		subscribers: make(map[string]map[chan taskSessionLog]struct{}),
	}
}

func taskSessionKey(projectID, taskID string) string { return projectID + "\x00" + taskID }

func (m *taskSessionLogManager) start(projectID, taskID, provider, agent string) agentstream.Reporter {
	return m.startEntity(projectID, taskID, "task", provider, agent)
}

func (m *taskSessionLogManager) startPlan(projectID, planID, provider string) agentstream.Reporter {
	return m.startEntity(projectID, planID, "plan", provider, "team-lead")
}

func (m *taskSessionLogManager) startEntity(projectID, entityID, entityType, provider, agent string) agentstream.Reporter {
	now := time.Now().UTC()
	key := taskSessionKey(projectID, entityID)
	session := taskSessionLog{
		ProjectID: projectID, Provider: provider, Agent: agent,
		Status: "running", StartedAt: &now, UpdatedAt: now, Entries: []taskSessionEntry{},
	}
	if entityType == "plan" {
		session.PlanID = entityID
	} else {
		session.TaskID = entityID
	}
	m.mu.Lock()
	m.sessions[key] = session
	m.appendLocked(key, agentstream.Event{Kind: "lifecycle", Message: "AI session started", Detail: provider})
	m.mu.Unlock()
	return &taskSessionReporter{manager: m, projectID: projectID, taskID: entityID}
}

func (r *taskSessionReporter) Report(event agentstream.Event) {
	r.manager.append(r.projectID, r.taskID, event)
}

func (m *taskSessionLogManager) finish(projectID, taskID string, runErr error) {
	key := taskSessionKey(projectID, taskID)
	m.mu.Lock()
	session, exists := m.sessions[key]
	if !exists {
		m.mu.Unlock()
		return
	}
	if runErr != nil {
		session.Status = "failed"
	} else {
		session.Status = "completed"
	}
	session.UpdatedAt = time.Now().UTC()
	m.sessions[key] = session
	message := "AI session completed"
	level := "info"
	detail := ""
	if runErr != nil {
		message = "AI session stopped"
		level = "error"
		detail = truncateSessionDetail(runErr.Error(), 800)
	}
	m.appendLocked(key, agentstream.Event{Level: level, Kind: "lifecycle", Message: message, Detail: detail})
	m.mu.Unlock()
}

func (m *taskSessionLogManager) append(projectID, taskID string, event agentstream.Event) {
	m.mu.Lock()
	m.appendLocked(taskSessionKey(projectID, taskID), event)
	m.mu.Unlock()
}

func (m *taskSessionLogManager) appendLocked(key string, event agentstream.Event) {
	session, exists := m.sessions[key]
	if !exists || strings.TrimSpace(event.Message) == "" {
		return
	}
	level := strings.TrimSpace(event.Level)
	if level == "" {
		level = "info"
	}
	now := time.Now().UTC()
	session.Entries = append(session.Entries, taskSessionEntry{
		ID: fmt.Sprintf("log-%d", m.nextID.Add(1)), Timestamp: now, Level: level,
		Kind: strings.TrimSpace(event.Kind), Message: strings.TrimSpace(event.Message),
		Detail: truncateSessionDetail(strings.TrimSpace(event.Detail), 4000),
	})
	if len(session.Entries) > maxTaskSessionEntries {
		session.Entries = append([]taskSessionEntry(nil), session.Entries[len(session.Entries)-maxTaskSessionEntries:]...)
	}
	session.UpdatedAt = now
	m.sessions[key] = session
	m.publishLocked(key, session)
}

func (m *taskSessionLogManager) snapshot(projectID, taskID string) taskSessionLog {
	return m.snapshotEntity(projectID, taskID, "task")
}

func (m *taskSessionLogManager) snapshotPlan(projectID, planID string) taskSessionLog {
	return m.snapshotEntity(projectID, planID, "plan")
}

func (m *taskSessionLogManager) snapshotEntity(projectID, entityID, entityType string) taskSessionLog {
	key := taskSessionKey(projectID, entityID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, exists := m.sessions[key]; exists {
		return cloneTaskSession(session)
	}
	session := taskSessionLog{ProjectID: projectID, Status: "idle", UpdatedAt: time.Now().UTC(), Entries: []taskSessionEntry{}}
	if entityType == "plan" {
		session.PlanID = entityID
	} else {
		session.TaskID = entityID
	}
	return session
}

func (m *taskSessionLogManager) subscribe(projectID, taskID string) (<-chan taskSessionLog, func()) {
	key := taskSessionKey(projectID, taskID)
	m.mu.Lock()
	defer m.mu.Unlock()
	updates := make(chan taskSessionLog, 4)
	if m.subscribers[key] == nil {
		m.subscribers[key] = make(map[chan taskSessionLog]struct{})
	}
	m.subscribers[key][updates] = struct{}{}
	return updates, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.subscribers[key], updates)
		if len(m.subscribers[key]) == 0 {
			delete(m.subscribers, key)
		}
	}
}

func (m *taskSessionLogManager) publishLocked(key string, session taskSessionLog) {
	for updates := range m.subscribers[key] {
		select {
		case updates <- cloneTaskSession(session):
		default:
		}
	}
}

func cloneTaskSession(session taskSessionLog) taskSessionLog {
	session.Entries = append([]taskSessionEntry(nil), session.Entries...)
	return session
}

func truncateSessionDetail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func (s *server) streamTaskSession(w http.ResponseWriter, r *http.Request) {
	s.streamAgentSession(w, r, r.PathValue("taskID"), false)
}

func (s *server) streamPlanSession(w http.ResponseWriter, r *http.Request) {
	s.streamAgentSession(w, r, r.PathValue("planID"), true)
}

func (s *server) streamAgentSession(w http.ResponseWriter, r *http.Request, entityID string, isPlan bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming is not supported by this HTTP server."})
		return
	}
	projectID := r.PathValue("projectID")
	updates, unsubscribe := s.taskSessions.subscribe(projectID, entityID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	snapshot := s.taskSessions.snapshot(projectID, entityID)
	if isPlan {
		snapshot = s.taskSessions.snapshotPlan(projectID, entityID)
	}
	writeSSE(w, "session", snapshot)
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case session := <-updates:
			writeSSE(w, "session", session)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
