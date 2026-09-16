package web

import (
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

type boardEventHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan kanban.Board]struct{}
}

func newBoardEventHub() *boardEventHub {
	return &boardEventHub{subscribers: make(map[string]map[chan kanban.Board]struct{})}
}

func (h *boardEventHub) subscribe(projectID string) (<-chan kanban.Board, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	updates := make(chan kanban.Board, 4)
	if h.subscribers[projectID] == nil {
		h.subscribers[projectID] = make(map[chan kanban.Board]struct{})
	}
	h.subscribers[projectID][updates] = struct{}{}
	return updates, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subscribers[projectID], updates)
		if len(h.subscribers[projectID]) == 0 {
			delete(h.subscribers, projectID)
		}
	}
}

func (h *boardEventHub) publish(board kanban.Board) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for updates := range h.subscribers[board.ProjectID] {
		select {
		case updates <- board:
		default:
		}
	}
}
