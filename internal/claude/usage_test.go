package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/theanh2906/AI-Product-Team/internal/agentusage"
)

func TestReadClaudeStreamRecordsProviderUsage(t *testing.T) {
	ctx, collector := agentusage.WithCollector(context.Background())
	_, err := readClaudeStream(ctx, strings.NewReader(`{"type":"result","session_id":"session-1","result":"done","usage":{"input_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":30},"total_cost_usd":0.12}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	usage := collector.Snapshot()
	if usage.TotalTokens != 155 || usage.CachedInputTokens != 20 || usage.CostUSD != 0.12 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
