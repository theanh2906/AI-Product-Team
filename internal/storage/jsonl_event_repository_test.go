package storage

import (
	"testing"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/observability"
)

func TestJSONLEventRepositoryPersistsAndFiltersTraceEvents(t *testing.T) {
	repository, err := NewJSONLEventRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, event := range []observability.Event{
		{ID: "one", Timestamp: now.Add(-time.Minute), Level: observability.LevelInfo, Category: "ai", ProjectID: "p1", CorrelationID: "trace-1"},
		{ID: "two", Timestamp: now, Level: observability.LevelError, Category: "http", ProjectID: "p2", CorrelationID: "trace-2"},
	} {
		if err := repository.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := repository.List(observability.Query{ProjectID: "p1", CorrelationID: "trace-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "one" {
		t.Fatalf("unexpected filtered events: %+v", events)
	}
	if repository.Path() == "" {
		t.Fatal("expected a stable event storage path")
	}
}
