package observability

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
}

func NewLogger(directory string) (*slog.Logger, string, error) {
	logDirectory := filepath.Join(directory, "logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return nil, "", fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(logDirectory, "application.jsonl")
	writer := &RotatingWriter{path: path, maxBytes: 10 * 1024 * 1024, backups: 5}
	handler := slog.NewJSONHandler(io.MultiWriter(os.Stderr, writer), &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler).With("service", "productcrew"), path, nil
}

func (w *RotatingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := os.Stat(w.path); err == nil && info.Size()+int64(len(data)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return file.Write(data)
}

func (w *RotatingWriter) rotate() error {
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
	for index := w.backups - 1; index >= 1; index-- {
		older := fmt.Sprintf("%s.%d", w.path, index)
		newer := fmt.Sprintf("%s.%d", w.path, index+1)
		if _, err := os.Stat(older); err == nil {
			_ = os.Rename(older, newer)
		}
	}
	if _, err := os.Stat(w.path); err == nil {
		return os.Rename(w.path, w.path+".1")
	}
	return nil
}
