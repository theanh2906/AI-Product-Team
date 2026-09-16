package web

import (
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/notifications"
)

type notificationEventHub struct {
	mu          sync.Mutex
	subscribers map[chan notifications.Notification]struct{}
}

func newNotificationEventHub() *notificationEventHub {
	return &notificationEventHub{subscribers: make(map[chan notifications.Notification]struct{})}
}

func (h *notificationEventHub) subscribe() (<-chan notifications.Notification, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	updates := make(chan notifications.Notification, 8)
	h.subscribers[updates] = struct{}{}
	return updates, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subscribers, updates)
	}
}

func (h *notificationEventHub) publish(value notifications.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for updates := range h.subscribers {
		select {
		case updates <- value:
		default:
		}
	}
}
