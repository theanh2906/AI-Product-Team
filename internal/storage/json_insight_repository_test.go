package storage

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/insights"
)

func TestJSONInsightRepositoryPersistsProjectKinds(t *testing.T) {
	directory := t.TempDir()
	repository, err := NewJSONInsightRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	record := insights.Record{ID: "insight-1", ProjectID: "project-1", Kind: insights.KindFeatureRadar, Payload: json.RawMessage(`{"summary":"saved"}`)}
	if err := repository.Upsert(ctx, record); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewJSONInsightRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(ctx, record.ProjectID, record.Kind)
	if err != nil || !jsonEqual(loaded.Payload, record.Payload) {
		t.Fatalf("unexpected stored insight: %+v %v", loaded, err)
	}
	if _, err := reopened.Get(ctx, record.ProjectID, insights.KindBugScan); err != insights.ErrNotFound {
		t.Fatalf("expected independent insight kinds, got %v", err)
	}
}

func TestJSONInsightRepositoryDeleteProjectRemovesOnlyMatchingRecords(t *testing.T) {
	directory := t.TempDir()
	repository, err := NewJSONInsightRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	records := []insights.Record{
		{ID: "feature-1", ProjectID: "project-1", Kind: insights.KindFeatureRadar, Payload: json.RawMessage(`{"kind":"feature"}`)},
		{ID: "scan-1", ProjectID: "project-1", Kind: insights.KindBugScan, Payload: json.RawMessage(`{"kind":"scan"}`)},
		{ID: "feature-2", ProjectID: "project-2", Kind: insights.KindFeatureRadar, Payload: json.RawMessage(`{"kind":"other"}`)},
	}
	for _, record := range records {
		if err := repository.Upsert(ctx, record); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := repository.DeleteProject(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected two removed insights, got %d", removed)
	}
	if remaining, err := repository.ListProject(ctx, "project-1"); err != nil || len(remaining) != 0 {
		t.Fatalf("expected project-1 insights to be removed, got %+v %v", remaining, err)
	}
	if loaded, err := repository.Get(ctx, "project-2", insights.KindFeatureRadar); err != nil || !jsonEqual(loaded.Payload, records[2].Payload) {
		t.Fatalf("unrelated insight was not preserved: %+v %v", loaded, err)
	}
	if removed, err := repository.DeleteProject(ctx, "missing"); err != nil || removed != 0 {
		t.Fatalf("missing project delete should be a no-op, got %d %v", removed, err)
	}
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}
