package copilot

import (
	"context"
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

func TestReadStreamRecordsAssistantUsage(t *testing.T) {
	ctx, collector := agentusage.WithCollector(context.Background())
	_, err := readStream(ctx, strings.NewReader(`{"type":"assistant.usage","data":{"inputTokens":90,"outputTokens":10,"cacheReadTokens":12,"reasoningTokens":4,"cost":0.03}}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	usage := collector.Snapshot()
	if usage.TotalTokens != 100 || usage.CachedInputTokens != 12 || usage.ReasoningTokens != 4 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
