package observability

import "testing"

func TestCorrelationIDsAreUniqueAndTraceable(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := NewCorrelationID()
		if len(id) < 8 || id[:4] != "req-" {
			t.Fatalf("unexpected correlation id %q", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate correlation id %q", id)
		}
		seen[id] = struct{}{}
	}
}
